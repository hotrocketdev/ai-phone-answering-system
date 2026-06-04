// Worker orchestrates the two-way loop. It is a long-running goroutine
// driven by a LiveKit Room callback; outbound publish happens once at
// startup, inbound subscription is event-driven.
package main

import (
	"context"
	"io"
	"log"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
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
	cartesiaKey, voiceID, modelID string
	cartesiaRate                  int
	cartesiaEnc                   string
	filterChain                   string
	bitrate                       int
	opusApp                       string
	replyMode                     string
	replyText                     string

	// outbound is the Opus track published to the room. Samples are
	// written by the sampleProvider goroutine driven by LiveKit.
	outbound *lksdk.LocalSampleTrack
	provider *outboundProvider

	// replyOnce ensures the outbound reply fires exactly once per
	// worker lifetime (this is a spike — no conversation loop).
	replied   atomic.Bool
	replyOnce sync.Once
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
	// publishes silence until the reply fires; that is fine for the
	// spike (the browser sees `track subscribed` and waits).
	if err := w.publishOutboundTrack(room); err != nil {
		return err
	}
	latencyLog("outbound_track_published")

	// 4. Block until SIGINT/SIGTERM. The reply is fired asynchronously
	// by the OnTrackSubscribed callback.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	log.Printf("worker: signal %v — disconnecting", s)
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

	// StartWrite kicks off a goroutine that pulls from the provider's
	// channel and writes each sample to the LiveKit RTP egress.
	done := make(chan struct{})
	if err := track.StartWrite(w.provider, func() {
		log.Println("outbound playback complete")
		close(done)
	}); err != nil {
		return err
	}
	return nil
}

// onInboundTrack is the core event of the spike. It spins up a
// per-track goroutine that assembles Opus frames from RTP packets
// and triggers the outbound reply on the first frame.
func (w *worker) onInboundTrack(track *webrtc.TrackRemote, pub *lksdk.RemoteTrackPublication, rp *lksdk.RemoteParticipant) {
	log.Printf("inbound_track_subscribed: identity=%s sid=%s codec=%s clock=%d channels=%d",
		rp.Identity(), pub.SID(), track.Codec().MimeType, track.Codec().ClockRate, track.Codec().Channels)
	latencyLog("inbound_track_subscribed")

	// Only audio is interesting for the spike.
	if track.Kind() != webrtc.RTPCodecTypeAudio {
		log.Printf("inbound_track_ignored: non-audio (kind=%d)", track.Kind())
		return
	}

	go w.runInboundReader(track, rp.Identity())
}

// runInboundReader assembles Opus frames from RTP packets. The first
// complete frame triggers the outbound reply (Stage 2).
func (w *worker) runInboundReader(track *webrtc.TrackRemote, peerIdentity string) {
	defer func() {
		log.Printf("inbound_reader_exit: identity=%s", peerIdentity)
	}()

	codec := track.Codec()
	if codec.MimeType != webrtc.MimeTypeOpus {
		log.Printf("inbound_reader: non-Opus codec %q — assembling raw RTP payload (no depacketizer)", codec.MimeType)
	}

	clockRate := codec.ClockRate
	if clockRate == 0 {
		clockRate = 48000
	}

	// SampleBuilder with a generous maxLate so we can cope with
	// reordering; Opus frames are small (10-60 bytes) so memory cost
	// is negligible.
	sb := newOpusSampleBuilder(200, clockRate)

	var totalFrames int
	var totalBytes int
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
		totalBytes += len(sample.Data)
		if !firstFrameLogged {
			firstFrameLogged = true
			latencyLog("first_inbound_frame")
			log.Printf("first_inbound_frame: identity=%s bytes=%d duration=%s",
				peerIdentity, len(sample.Data), sample.Duration)
			// Stage 2 trigger: first frame → outbound reply.
			w.maybeFireReply()
		}
		if totalFrames%50 == 0 {
			log.Printf("inbound_metric: identity=%s frames=%d bytes=%d",
				peerIdentity, totalFrames, totalBytes)
		}
	}
}

// maybeFireReply runs the reply exactly once. The work happens on a
// goroutine so the inbound reader is not blocked.
func (w *worker) maybeFireReply() {
	w.replyOnce.Do(func() {
		go w.fireReply()
	})
}

func (w *worker) fireReply() {
	log.Printf("outbound_reply_start: mode=%s", w.replyMode)
	latencyLog("outbound_reply_start")
	defer latencyLog("outbound_reply_done")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	switch w.replyMode {
	case "none":
		log.Printf("outbound_reply_skip: REPLY_MODE=none")
		return
	case "tone_on_first_frame":
		w.publishTone(ctx)
	case "fixed_on_first_frame":
		w.publishCartesia(ctx, w.replyText)
	default:
		log.Printf("outbound_reply_unknown_mode: %q — falling back to tone", w.replyMode)
		w.publishTone(ctx)
	}
}

func (w *worker) publishTone(ctx context.Context) {
	log.Printf("outbound_tone: freq=440Hz duration=3s")
	// 3s of 440 Hz mono PCM at 48 kHz, amplitude 30%.
	pcm := generateTone(48000, 1, 440.0, 3.0)
	if err := w.publishPCMAsOpus(ctx, pcm, 48000, "s16le"); err != nil {
		log.Printf("outbound_tone: publish failed: %v", err)
		return
	}
	log.Printf("outbound_tone_done")
}

func (w *worker) publishCartesia(ctx context.Context, text string) {
	if w.cartesiaKey == "" {
		log.Printf("outbound_cartesia_skip: CARTESIA_API_KEY empty — falling back to tone")
		w.publishTone(ctx)
		return
	}
	log.Printf("outbound_cartesia: voice=%s model=%s rate=%d encoding=%s text_len=%d",
		w.voiceID, w.modelID, w.cartesiaRate, w.cartesiaEnc, len(text))
	pcm, err := Synthesize(w.cartesiaKey, text, w.voiceID, w.modelID, w.cartesiaEnc, w.cartesiaRate)
	if err != nil {
		log.Printf("outbound_cartesia: synthesize failed: %v — falling back to tone", err)
		w.publishTone(ctx)
		return
	}
	latencyLog("cartesia_done")
	if err := w.publishPCMAsOpus(ctx, pcm, w.cartesiaRate, w.cartesiaEnc); err != nil {
		log.Printf("outbound_cartesia: publish failed: %v", err)
		return
	}
	log.Printf("outbound_cartesia_done")
}

// publishPCMAsOpus spins up an ffmpeg subprocess to encode the PCM
// buffer as Opus and writes the resulting frames into the outbound
// LiveKit track via the provider channel.
func (w *worker) publishPCMAsOpus(ctx context.Context, pcm []int16, inputRate int, encoding string) error {
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
	// OpusHead + OpusTags consumed inline; subsequent packets are
	// raw Opus frames for the LiveKit track.
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
	// Signal end-of-stream so the LiveKit track can finalize.
	w.provider.close()
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
