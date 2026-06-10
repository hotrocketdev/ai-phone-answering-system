// Worker orchestrates the two-way LiveKit conversation loop.
//
// Two modes (set via LIVEKIT_WORKER_MODE):
//
//   stitched (Stage 1, default): per-utterance VAD + STT (Whisper) +
//   LLM (gpt-4o-mini) + TTS (Cartesia) + ffmpeg Opus publish.
//   Multi-turn.
//
//   realtime (Stage 1.5): LiveKit RTP Opus -> continuous ffmpeg
//   decode -> OpenAI Realtime WebSocket -> continuous ffmpeg
//   encode -> LiveKit RTP Opus. Realtime handles STT/LLM/TTS
//   server-side; sub-1s latency, server VAD, voice mimicry.
//
// Inbound pipeline (stitched mode, per-track goroutine):
//   1. track.ReadRTP()  ->  samplebuilder  ->  raw Opus frames
//   2. VAD on frame cadence: 500ms silence ends an utterance
//   3. Snapshot buffered Opus frames; run ffmpeg per-utterance to
//      decode to mono s16le 16kHz PCM
//   4. Wrap PCM in WAV; POST to OpenAI Whisper
//   5. Append user turn to history; POST to gpt-4o-mini
//   6. POST reply to Cartesia Sonic 3.5 + Julia
//   7. ffmpeg encodes PCM to Opus; push frames into outboundProvider
//   8. Push 200ms silence delimiter so the browser hears a gap
//   9. Loop: next utterance, multi-turn
//
// Inbound pipeline (realtime mode, per-track goroutine):
//   1. track.ReadRTP()  ->  samplebuilder  ->  raw Opus frames
//   2. oggMuxer writes OGG Opus pages to ffmpeg stdin (continuous)
//   3. ffmpeg decode goroutine: OGG Opus 48kHz -> PCM16 24kHz
//   4. Chunk to 100ms, send input_audio_buffer.append to Realtime
//   5. Server VAD detects speech, runs model, streams response
//
// Outbound pipeline (realtime mode):
//   - One long-lived ffmpeg encode subprocess: PCM16 24kHz in,
//     OGG Opus 48kHz out. Lives for the worker's lifetime.
//   - Realtime response.audio.delta -> write to encode stdin
//   - Reader goroutine: OGG demux -> 20ms Opus frames -> outboundProvider
//
// Outbound pipeline:
//   - Single long-lived LocalSampleTrack, published once at startup
//   - Channel-backed outboundProvider; never closed (multi-turn)
//   - Each turn pushes 20ms Opus frames at 64kbps
package main

import (
	"context"
	"io"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/livekit/protocol/auth"
	lksdk "github.com/livekit/server-sdk-go"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

// worker holds the spike's runtime config and shared state.
type worker struct {
	livekitURL, apiKey, apiSecret string
	roomName, identity           string
	mode                         string // "stitched" or "realtime"

	// Stage 1 (stitched mode) config
	cartesiaKey, voiceID, modelID string
	cartesiaRate                  int
	cartesiaEnc                   string
	filterChain                   string
	bitrate                       int
	opusApp                       string
	openaiKey                     string
	silenceTimeout                time.Duration

	// Stage 1.5 (realtime mode) config
	rtModel         string
	rtVoice         string
	rtInstructions  string
	rtVadThreshold  float64
	rtVadSilenceMs  int
	rtVadPrefixMs   int
	rtOpusBitrate   int
	rtInboundFilter string

	// Runtime state shared across both modes
	outbound *lksdk.LocalSampleTrack
	provider *outboundProvider

	// Realtime-mode runtime state
	rt            *realtimeClient
	encodePcm     io.WriteCloser
	encodeCancel  context.CancelFunc
	encodeCmds    []*continuousPcm
	decodeCmds    []*continuousPcm
	inboundTracks map[string]context.CancelFunc
	encErrOnce    sync.Once
	rtErrOnce     sync.Once
}

func (w *worker) run() error {
	// 1. Token
	token, err := mintToken(w.apiKey, w.apiSecret, w.roomName, w.identity)
	if err != nil {
		return err
	}
	log.Printf("token generated (room=%s identity=%s ttl=1h)", w.roomName, w.identity)
	latencyLog("token_done")

	// 2. Connect to room
	room, err := lksdk.ConnectToRoomWithToken(w.livekitURL, token, w.callbacks(), lksdk.WithAutoSubscribe(true))
	if err != nil {
		return err
	}
	defer room.Disconnect()
	log.Printf("connected to room=%s as %s", room.SID(), w.identity)
	latencyLog("room_connected")

	// 3. Pre-create the outbound Opus track and publish it. The track
	// publishes silence until the first turn fires.
	if err := w.publishOutboundTrack(room); err != nil {
		return err
	}
	latencyLog("outbound_track_published")

	// 4. Mode-specific setup.
	w.inboundTracks = make(map[string]context.CancelFunc)
	switch w.mode {
	case "realtime":
		if err := w.runRealtime(room); err != nil {
			return err
		}
	case "realtime-cartesia":
		if err := w.runRealtimeCartesia(room); err != nil {
			return err
		}
	case "stitched":
		// stitched mode setup happens lazily in onInboundTrack
		// (per-utterance VAD + STT/LLM/TTS chain).
	default:
		log.Fatalf("unknown mode %q (already validated in main)", w.mode)
	}

	// 5. Block until SIGINT/SIGTERM.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	log.Printf("worker: signal %v — disconnecting", s)

	// 6. Realtime-mode cleanup.
	if w.rt != nil {
		w.rt.close()
	}
	for _, cancel := range w.inboundTracks {
		cancel()
	}
	if w.encodeCancel != nil {
		w.encodeCancel()
	}
	for _, cp := range w.encodeCmds {
		cp.kill()
	}
	for _, cp := range w.decodeCmds {
		cp.kill()
	}
	return nil
}

func (w *worker) callbacks() *lksdk.RoomCallback {
	cb := lksdk.NewRoomCallback()
	cb.OnParticipantConnected = func(p *lksdk.RemoteParticipant) {
		log.Printf("participant connected: %s (sid=%s)", p.Identity(), p.SID())
	}
	cb.OnParticipantDisconnected = func(p *lksdk.RemoteParticipant) {
		log.Printf("participant disconnected: %s (sid=%s)", p.Identity(), p.SID())
	}
	cb.OnTrackSubscribed = func(track *webrtc.TrackRemote, pub *lksdk.RemoteTrackPublication, rp *lksdk.RemoteParticipant) {
		w.onInboundTrack(track, pub, rp)
	}
	cb.OnTrackUnsubscribed = func(track *webrtc.TrackRemote, pub *lksdk.RemoteTrackPublication, rp *lksdk.RemoteParticipant) {
		log.Printf("track unsubscribed: identity=%s sid=%s", rp.Identity(), pub.SID())
	}
	return cb
}

func (w *worker) publishOutboundTrack(room *lksdk.Room) error {
	track, err := lksdk.NewLocalSampleTrack(webrtc.RTPCodecCapability{
		MimeType:  webrtc.MimeTypeOpus,
		ClockRate: 48000,
		Channels:  1,
	})
	if err != nil {
		return err
	}
	w.outbound = track
	w.provider = newOutboundProvider()

	// PublishTrack must be called BEFORE StartWrite: StartWrite
	// only spawns its writer goroutine when the track is already
	// bound to a peer connection, which only happens once the
	// track has been published. Calling StartWrite first would
	// silently leave the provider channel unconsumed.
	pub, err := room.LocalParticipant.PublishTrack(track, &lksdk.TrackPublicationOptions{})
	if err != nil {
		return err
	}
	log.Printf("outbound track published: id=%s mime=%s", pub.SID(), pub.MimeType())

	// Pre-publish silence so the browser sees a live track from t=0
	// (otherwise some browsers' VAD/AGC suppresses early frames).
	w.provider.pushSilence(500 * time.Millisecond)

	// StartWrite kicks off a goroutine that pulls from the provider's
	// channel and writes each sample to the LiveKit RTP egress.
	// Note: the playback-complete callback will never fire for this
	// spike because the provider channel is never closed (multi-turn).
	if err := track.StartWrite(w.provider, func() {
		log.Println("outbound playback complete (unexpected in spike; channel is never closed)")
	}); err != nil {
		return err
	}
	return nil
}

// onInboundTrack spins up the per-track goroutine that owns the
// samplebuilder + VAD + utterance pipeline.
func (w *worker) onInboundTrack(track *webrtc.TrackRemote, pub *lksdk.RemoteTrackPublication, rp *lksdk.RemoteParticipant) {
	log.Printf("inbound_track_subscribed: identity=%s sid=%s codec=%s clock=%d channels=%d mode=%s",
		rp.Identity(), pub.SID(), track.Codec().MimeType,
		track.Codec().ClockRate, track.Codec().Channels, w.mode)
	latencyLog("inbound_track_subscribed")

	if track.Kind() != webrtc.RTPCodecTypeAudio {
		log.Printf("inbound_track_ignored: non-audio (kind=%d)", track.Kind())
		return
	}
	switch w.mode {
	case "realtime", "realtime-cartesia":
		// Both Realtime variants share the same inbound
		// pipeline (LiveKit -> ffmpeg decode -> Realtime
		// STT/LLM). The outbound path differs.
		go w.runRealtimeInbound(track, rp)
	case "stitched":
		go w.runInboundReader(track, rp.Identity())
	}
}

// sampleMsg carries one assembled Opus frame (or an error) from
// the read loop to the orchestrating select.
type sampleMsg struct {
	data []byte
	err  error
}

// runInboundReader orchestrates: readRTP -> samplebuilder -> VAD ->
// utterance channel -> handleUtterance. Two goroutines: a read
// loop pushing sampleMsg into sampleCh, and the orchestrator that
// runs VAD + utterance handling.
func (w *worker) runInboundReader(track *webrtc.TrackRemote, peerIdentity string) {
	defer func() {
		log.Printf("inbound_reader_exit: identity=%s", peerIdentity)
	}()

	codec := track.Codec()
	clockRate := codec.ClockRate
	if clockRate == 0 {
		clockRate = 48000
	}

	// SampleBuilder with a generous maxLate so we can cope with
	// reordering; Opus frames are small (10-60 bytes) so memory
	// cost is negligible.
	sb := newOpusSampleBuilder(200, clockRate)

	// Per-utterance VAD + history + utterance channel.
	v := newVAD()
	v.silenceTimeout = w.silenceTimeout
	history := make([]chatMessage, 0, 8)
	utteranceCh := make(chan [][]byte, 4)

	// Read loop: pull RTP packets, assemble Opus frames, push to VAD.
	// When VAD says an utterance ended, ship the buffered frames on
	// utteranceCh.
	go func() {
		var totalFrames int
		var firstFrameLogged bool
		for {
			pkt, _, err := track.ReadRTP()
			if err != nil {
				log.Printf("inbound_read_rtp: identity=%s err=%v", peerIdentity, err)
				return
			}
			sample, ok := sb.push(pkt)
			if !ok {
				continue
			}
			totalFrames++
			if !firstFrameLogged {
				firstFrameLogged = true
				latencyLog("first_inbound_frame")
				log.Printf("first_inbound_frame: identity=%s bytes=%d duration=%s",
					peerIdentity, len(sample.Data), sample.Duration)
			}
			if v.push(sample.Data) {
				// Max duration hit
				frames := v.takeUtterance()
				if frames != nil {
					utteranceCh <- frames
				}
			}
			if totalFrames%100 == 0 {
				log.Printf("inbound_metric: identity=%s frames=%d", peerIdentity, totalFrames)
			}
		}
	}()

	// Orchestrator loop: silence-timer ticks (100ms) + utteranceCh
	// messages. VAD's tick() returns true when silenceTimeout has
	// elapsed since the last frame; on true, snapshot the buffered
	// frames and ship them on utteranceCh.
	silenceTick := time.NewTimer(w.silenceTimeout)
	defer silenceTick.Stop()
	for {
		select {
		case frames := <-utteranceCh:
			w.handleUtterance(frames, &history)
		case <-silenceTick.C:
			if v.tick() {
				frames := v.takeUtterance()
				if frames != nil {
					utteranceCh <- frames // re-use the same channel
				}
			}
			silenceTick.Reset(w.silenceTimeout)
		}
	}
}

// handleUtterance runs the full STT -> LLM -> TTS -> publish chain
// for one user utterance. Latency is logged at every stage. Errors
// are logged but do not abort the inbound loop. History is updated
// in place so the next turn has context.
func (w *worker) handleUtterance(frames [][]byte, history *[]chatMessage) {
	t0 := time.Now()
	latencyLog("utterance_ended")
	log.Printf("utterance_frames: count=%d", len(frames))
	if len(frames) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Decode Opus -> PCM via ffmpeg (per-utterance subprocess)
	pcm, err := decodeOpusUtteranceToPCM(ctx, frames)
	if err != nil {
		log.Printf("utterance_decode_failed: %v", err)
		return
	}
	latencyLog("opus_decoded")
	log.Printf("utterance_pcm: bytes=%d (%.2fs @ 16kHz mono s16le)",
		len(pcm), float64(len(pcm))/float64(opusDecodeSampleRate*2))

	// 2. Wrap in WAV
	wav := pcmS16LEToWAV(pcm, opusDecodeSampleRate)
	latencyLog("wav_wrapped")

	// 3. STT
	transcript, err := Transcribe(w.openaiKey, wav)
	if err != nil {
		log.Printf("utterance_stt_failed: %v", err)
		return
	}
	latencyLog("stt_done")
	log.Printf("stt_text: %q", transcript)
	if transcript == "" {
		return
	}

	// 4. LLM
	reply, err := CompleteLLM(ctx, w.openaiKey, *history, transcript)
	if err != nil {
		log.Printf("utterance_llm_failed: %v", err)
		return
	}
	latencyLog("llm_done")
	log.Printf("llm_reply: %q", reply)

	// 5. TTS via Cartesia
	var pcmReply []int16
	if w.cartesiaKey == "" {
		log.Printf("utterance_tts_skip: CARTESIA_API_KEY empty — using 2s tone")
		pcmReply = generateTone(w.cartesiaRate, 1, 440.0, 2.0)
	} else {
		pcmReply, err = Synthesize(w.cartesiaKey, reply, w.voiceID, w.modelID, w.cartesiaEnc, w.cartesiaRate)
		if err != nil {
			log.Printf("utterance_tts_failed: %v — falling back to 2s tone", err)
			pcmReply = generateTone(w.cartesioFallbackRate(), 1, 440.0, 2.0)
		}
	}
	latencyLog("cartesia_done")

	// 6. Encode PCM -> Opus + publish (existing pipeline; does not close channel)
	if err := w.publishPCMAsOpus(ctx, pcmReply, w.cartesiaRate, w.cartesiaEnc); err != nil {
		log.Printf("utterance_publish_failed: %v", err)
		return
	}
	latencyLog("opus_published")

	// 7. Push a short silence delimiter so the browser hears a gap
	// between turns (no audible click on Opus stream continuation).
	w.provider.pushSilence(250 * time.Millisecond)

	// 8. Update history (cap at 8 messages = 4 turns)
	*history = append(*history,
		chatMessage{Role: "user", Content: transcript},
		chatMessage{Role: "assistant", Content: reply},
	)
	if len(*history) > 8 {
		*history = (*history)[len(*history)-8:]
	}

	log.Printf("turn_total_ms: %d", time.Since(t0).Milliseconds())
}

// cartesioFallbackRate is used when Cartesia fails and we fall back
// to the test tone. The tone generator runs at this rate (usually
// 48kHz) and ffmpeg encodes to Opus.
func (w *worker) cartesioFallbackRate() int {
	if w.cartesiaRate == 0 {
		return 48000
	}
	return w.cartesiaRate
}

// publishPCMAsOpus spins up an ffmpeg subprocess to encode the PCM
// buffer as Opus and writes the resulting frames into the outbound
// LiveKit track via the provider channel. The provider channel is
// NOT closed here (multi-turn); the channel is closed only at
// worker shutdown.
func (w *worker) publishPCMAsOpus(ctx context.Context, pcm []int16, inputRate int, encoding string) error {
	// Debug: save the latest TTS PCM as a WAV for offline noise
	// analysis. Toggle by touching /tmp/save-tts (any content) and
	// remove to disable. Files are overwritten each turn.
	if _, err := os.Stat("/tmp/save-tts"); err == nil {
		pcmBytes := int16ToBytes(pcm)
		wav := pcmS16LEToWAV(pcmBytes, inputRate)
		_ = os.WriteFile("/tmp/last-reply.wav", wav, 0644)
	}
	ff, err := startFfmpegOpus(ctx, inputRate, encoding, w.filterChain, w.bitrate, w.opusApp)
	if err != nil {
		return err
	}
	defer ff.kill()
	log.Printf("ffmpeg_started=true pid=%d input_rate=%d input_format=%s filter=%q bitrate=%d app=%s",
		ff.cmd.Process.Pid, inputRate, encoding, w.filterChain, w.bitrate, w.opusApp)
	if err := streamPCM(ff, pcm, encoding); err != nil {
		return err
	}
	demuxer := newOggOpusReaderImpl(ff.stdout)
	headPkt, err := demuxer.NextOpusPacket()
	if err != nil {
		return err
	}
	head, err := ParseOpusHead(headPkt)
	if err != nil {
		return err
	}
	log.Printf("opus_header version=%d channels=%d input_rate=%d",
		head.Version, head.ChannelCount, head.InputSampleRate)
	if _, err := demuxer.NextOpusPacket(); err != nil {
		return err
	}
	log.Printf("ogg_demuxer_ready (OpusHead + OpusTags consumed)")

	count := 0
	for {
		pkt, err := demuxer.NextOpusPacket()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		count++
		w.provider.push(media.Sample{Data: pkt, Duration: 20 * time.Millisecond})
	}
	log.Printf("outbound_frames_published: count=%d", count)
	return nil
}

func mintToken(apiKey, apiSecret, room, identity string) (string, error) {
	at := auth.NewAccessToken(apiKey, apiSecret)
	yes := true
	at.AddGrant(&auth.VideoGrant{
		RoomJoin:       true,
		Room:           room,
		CanPublish:     &yes,
		CanSubscribe:   &yes,
		CanPublishData: &yes,
	})
	at.SetIdentity(identity)
	return at.ToJWT()
}
