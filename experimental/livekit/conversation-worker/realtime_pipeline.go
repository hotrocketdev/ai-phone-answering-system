// Stage 1.5: OpenAI Realtime bridge for LiveKit.
//
// Two worker modes live in this file:
//
//   realtime (original): LiveKit RTP -> continuous ffmpeg decode ->
//   OpenAI Realtime WebSocket -> Realtime TTS (server-side) ->
//   continuous ffmpeg encode -> LiveKit RTP. Realtime handles
//   STT/LLM/TTS server-side; sub-1s latency in theory. As of
//   2026-06 the server-side TTS audio deltas are silently missing
//   (response.output_audio_transcript.delta IS delivered; the
//   actual response.output_audio.delta is not), so this mode is
//   currently broken at the model layer.
//
//   realtime-cartesia (fallback): Same inbound path (LiveKit ->
//   ffmpeg decode -> Realtime STT/LLM), but instead of using
//   Realtime's server-side TTS, we take the assistant text from
//   response.output_audio_transcript.delta, buffer it by
//   sentence boundary, and POST each sentence to Cartesia Sonic
//   3.5 + Julia. Cartesia's PCM is streamed into the same
//   outbound Opus encoder the original mode uses.
//
// Audio path (realtime-cartesia):
//
//   Browser mic (Opus 48kHz)
//     -> LiveKit RTP
//     -> [1] ffmpeg decode (continuous): OGG Opus 48kHz -> PCM16 24kHz
//     -> OpenAI Realtime WebSocket (STT + LLM only, server VAD)
//     -> response.output_audio_transcript.delta (assistant text)
//     -> sentence buffer (flush on . ! ? \n or 200-char cap)
//     -> Cartesia /tts/bytes (Sonic 3.5, Julia voice, PCM16 24kHz)
//     -> [2] ffmpeg encode (continuous): PCM16 24kHz -> OGG Opus 48kHz
//     -> LiveKit RTP
//     -> Browser speakers
//
// The two continuous ffmpeg subprocesses use the
// drain-in-goroutine pattern (see continuous_ffmpeg.go) to avoid
// the 64KB pipe-buffer deadlock.
package main

import (
	"context"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	lksdk "github.com/livekit/server-sdk-go"
)

// runRealtime implements the Stage 1.5 pipeline. Called from
// worker.run() when mode == "realtime". The LiveKit room and
// outbound track are already published by the time we get here.
func (w *worker) runRealtime(room *lksdk.Room) error {
	if w.openaiKey == "" {
		return errRealtime("OPENAI_API_KEY not set")
	}

	// 1. Open OpenAI Realtime WebSocket and configure session.
	cfg := realtimeSessionConfig{
		Type:         "realtime",
		Instructions: w.rtInstructions,
		Audio: &realtimeAudioConfig{
			Input: &realtimeAudioInput{
				Format: &realtimeAudioFormat{
					Type: "audio/pcm",
					Rate: 24000,
				},
				Transcription: &inputTranscription{
					Model: "whisper-1",
				},
				TurnDetection: &turnDetection{
					Type:              "server_vad",
					Threshold:         w.rtVadThreshold,
					PrefixPaddingMs:   w.rtVadPrefixMs,
					SilenceDurationMs: w.rtVadSilenceMs,
				},
			},
			Output: &realtimeAudioOutput{
				Format: &realtimeAudioFormat{
					Type: "audio/pcm",
					Rate: 24000,
				},
				Voice: w.rtVoice,
			},
		},
	}
	rt, err := newRealtimeClient(w.openaiKey, w.rtModel, cfg)
	if err != nil {
		return err
	}
	w.rt = rt
	latencyLog("realtime_connected")

	// 2. Register callbacks BEFORE triggering any response so we
	//    don't miss the first audio deltas.
	rt.OnAudio = w.handleRealtimeAudio
	rt.OnUserTranscript = func(text string) {
		log.Printf("realtime_user: %q", text)
	}
	var assistantText string
	rt.OnAssistantTranscript = func(delta string) {
		assistantText += delta
		log.Printf("realtime_assistant_text_delta: %q (running: %q)", delta, assistantText)
	}
	rt.OnError = func(e map[string]interface{}) {
		log.Printf("realtime_error_event: %v", e)
	}

	// 3. Start the outbound PCM -> Opus encoder subprocess. It
	//    lives for the lifetime of the worker. The handler
	//    writes incoming Realtime audio chunks to it.
	if err := w.startOutboundEncoder(); err != nil {
		return err
	}
	latencyLog("outbound_encoder_started")

	// 4. Force an initial greeting. Without this, Realtime sits
	//    silent until the user speaks (server VAD only fires on
	//    user audio).
	if err := rt.createResponse("Please greet the caller warmly and ask how you can help."); err != nil {
		log.Printf("realtime: initial createResponse failed: %v", err)
	}

	return nil
}

// startOutboundEncoder starts a long-lived ffmpeg subprocess that
// encodes PCM16 24kHz mono (stdin) to OGG Opus 48kHz mono (stdout).
// A reader goroutine consumes the OGG stream and pushes each 20ms
// Opus frame into the outboundProvider. The encode handle is
// stored on the worker; subsequent Realtime audio chunks are
// written to encodePcm.
func (w *worker) startOutboundEncoder() error {
	ctx, cancel := context.WithCancel(context.Background())
	w.encodeCancel = cancel
	cp, err := startContinuous(ctx, opusEncodeContinuousCmd(w.rtOpusBitrate))
	if err != nil {
		cancel()
		return err
	}
	w.encodeCmds = append(w.encodeCmds, cp) // for shutdown
	w.encodePcm = cp.stdin
	log.Printf("outbound_encoder: ffmpeg started pid=%d bitrate=%d",
		cp.cmd.Process.Pid, w.rtOpusBitrate)

	// Reader goroutine: drain stdout, demux OGG, push frames to
	// the outbound provider.
	go func() {
		oggSource := io.Reader(cp.stdout)
		// Optional: dump the raw OGG stream to a file for
		// inspection. Toggle by touching /tmp/save-ogg (any
		// content). Helps diagnose "ffmpeg produced OGG but
		// the demuxer reads nothing" cases.
		if _, err := os.Stat("/tmp/save-ogg"); err == nil {
			f, err := os.Create("/tmp/outbound-stream.ogg")
			if err == nil {
				oggSource = io.TeeReader(cp.stdout, f)
				log.Printf("outbound_encoder: dumping OGG to /tmp/outbound-stream.ogg")
			}
		}
		reader := newOggContinuousReader(oggSource)
		count := 0
		readAttempts := 0
		var lastLoggedCount int
		var lastLogTime = time.Now()
		for {
			readAttempts++
			pkt, err := reader.NextOpusPacket()
			if err != nil {
				if err != io.EOF {
					log.Printf("outbound_encoder: read: %v (attempts=%d pushed=%d)", err, readAttempts, count)
				} else {
					log.Printf("outbound_encoder: EOF after %d frames", count)
				}
				return
			}
			count++
			readAttempts = 0
			w.provider.push(media.Sample{
				Data:     pkt,
				Duration: 20 * time.Millisecond,
			})
			// Periodic log so we can verify the encoder is
			// actually pushing frames to LiveKit. 1s cadence
			// keeps the log readable.
			if time.Since(lastLogTime) > 1*time.Second {
				delta := count - lastLoggedCount
				if delta > 0 {
					log.Printf("outbound_encoder: opus_frames_pushed=%d delta_1s=%d",
						count, delta)
				} else {
					log.Printf("outbound_encoder: waiting_for_data frames_pushed=%d", count)
				}
				lastLoggedCount = count
				lastLogTime = time.Now()
			}
		}
	}()
	return nil
}

// handleRealtimeAudio is the callback invoked for each
// response.audio.delta event from OpenAI Realtime. pcm is PCM16
// 24kHz mono. We write it directly to the outbound ffmpeg
// encoder's stdin; the encoder produces Opus frames that are
// pushed to the LiveKit outbound track by the reader goroutine.
func (w *worker) handleRealtimeAudio(pcm []byte) {
	if w.encodePcm == nil {
		log.Printf("handleRealtimeAudio: encodePcm is nil (encoder not started)")
		return
	}
	if _, err := w.encodePcm.Write(pcm); err != nil {
		// Only log once per connection to avoid spam.
		w.encErrOnce.Do(func() {
			log.Printf("handleRealtimeAudio: encodePcm.Write: %v", err)
		})
	}
}

// runRealtimeInbound handles one inbound LiveKit track. It starts
// a continuous ffmpeg subprocess that decodes the OGG Opus stream
// to PCM16 24kHz mono, then forwards PCM chunks to the OpenAI
// Realtime input buffer via input_audio_buffer.append events.
func (w *worker) runRealtimeInbound(track *webrtc.TrackRemote, rp *lksdk.RemoteParticipant) {
	trackID := track.ID()
	if _, ok := w.inboundTracks[trackID]; ok {
		log.Printf("realtime_inbound: track %s already running", trackID)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.inboundTracks[trackID] = cancel
	log.Printf("realtime_inbound_started: track=%s identity=%s codec=%s clock=%d channels=%d",
		trackID, rp.Identity(), track.Codec().MimeType,
		track.Codec().ClockRate, track.Codec().Channels)

	// Start ffmpeg decode.
	cp, err := startContinuous(ctx, opusDecodeContinuousCmd(w.rtInboundFilter))
	if err != nil {
		log.Printf("realtime_inbound: ffmpeg start: %v", err)
		cancel()
		delete(w.inboundTracks, trackID)
		return
	}
	w.decodeCmds = append(w.decodeCmds, cp)
	log.Printf("realtime_inbound: ffmpeg pid=%d filter=%q",
		cp.cmd.Process.Pid, w.rtInboundFilter)

	// Write OpusHead + OpusTags once. Without these, ffmpeg's ogg
	// demuxer doesn't know the stream parameters.
	muxer := newOggMuxer(cp.stdin, 0xC0FFEE, 1, 48000)
	if err := muxer.writeOpusHead(); err != nil {
		log.Printf("realtime_inbound: writeOpusHead: %v", err)
		cancel()
		return
	}
	if err := muxer.writeOpusTags(); err != nil {
		log.Printf("realtime_inbound: writeOpusTags: %v", err)
		cancel()
		return
	}

	// Goroutine 1: drain ffmpeg stdout, chunk to 100ms PCM, send
	// to Realtime input buffer.
	go func() {
		chunkBuf := newChunkBuffer(pcmChunkBytes)
		readBuf := make([]byte, 4096)
		var totalBytes int64
		var chunksSent int
		for {
			n, err := cp.stdout.Read(readBuf)
			if n > 0 {
				totalBytes += int64(n)
				chunks := chunkBuf.push(readBuf[:n])
				for _, c := range chunks {
					if w.rt == nil {
						return
					}
					if err := w.rt.sendAudio(c); err != nil {
						w.rtErrOnce.Do(func() {
							log.Printf("realtime_inbound: sendAudio: %v", err)
						})
						return
					}
					chunksSent++
				}
				if chunksSent%50 == 0 && chunksSent > 0 {
					log.Printf("realtime_inbound: pcm_bytes=%d chunks_sent=%d",
						totalBytes, chunksSent)
				}
			}
			if err != nil {
				if err != io.EOF {
					log.Printf("realtime_inbound: stdout: %v", err)
				}
				return
			}
		}
	}()

	// Goroutine 2: read RTP packets, write OGG Opus frames to
	// ffmpeg stdin. This is the only place we touch the inbound
	// track; everything downstream is async.
	go func() {
		defer func() {
			cancel()
			delete(w.inboundTracks, trackID)
			log.Printf("realtime_inbound_exit: track=%s", trackID)
		}()
		sb := newOpusSampleBuilder(200, track.Codec().ClockRate)
		var frames int
		for {
			pkt, _, err := track.ReadRTP()
			if err != nil {
				log.Printf("realtime_inbound: ReadRTP: %v", err)
				return
			}
			sample, ok := sb.push(pkt)
			if !ok {
				continue
			}
			if err := muxer.writeOpusFrame(sample.Data, 960); err != nil {
				log.Printf("realtime_inbound: writeOpusFrame: %v", err)
				return
			}
			frames++
		}
	}()
}

// errRealtime wraps a string as an error for short messages.
type errRealtime string

func (e errRealtime) Error() string { return string(e) }

// Use the existing outbound provider + sync. The realtime worker
// shares the outboundProvider with the stitched path; both write
// Opus frames into the same channel that the LiveKit writer
// goroutine drains.
var _ = sync.Mutex{} // sync is used by chunkBuffer; keep import.

// ----------------------------------------------------------------------------
// realtime-cartesia mode
// ----------------------------------------------------------------------------
//
// This mode was added on 2026-06-05 in response to OpenAI Realtime
// stopping server-side TTS audio delta delivery. The LLM is still
// usable (it streams the assistant's intended speech as text deltas
// via response.output_audio_transcript.delta), so we re-route that
// text through Cartesia Sonic 3.5 + Julia. The hope is that the
// Realtime VAD / STT / LLM combination still gives us the
// low-latency, multi-turn feel we wanted, while Cartesia restores
// the natural Alex voice.
//
// Latency budget per turn (target: under 2.0s end-to-end):
//
//   user stops speaking
//   + ~600ms (Realtime server VAD silence_duration_ms)
//   + LLM first delta ~150-400ms
//   + sentence buffer flush (immediate on . ! ?)
//   + Cartesia HTTP RTT + first byte ~250-500ms
//   + Opus frame encode ~20ms
//   = ~1.0-1.5s for short replies, more for longer ones
//
// sentenceBuffer accumulates the running assistant text. It
// flushes to Cartesia on:
//   - end-of-sentence punctuation (. ! ? \n, optionally followed
//     by a closing quote/bracket)
//   - the running text exceeding maxChars (default 200) so we
//     never burst more than ~15-20s of speech in one TTS call
//   - end-of-turn (response.done / output_audio_transcript.done)
//   - a manual flush call
type sentenceBuffer struct {
	mu       sync.Mutex
	text     string
	maxChars int
	onFlush  func(text string) // called outside the lock
}

func newSentenceBuffer(maxChars int, onFlush func(string)) *sentenceBuffer {
	return &sentenceBuffer{maxChars: maxChars, onFlush: onFlush}
}

func (s *sentenceBuffer) push(delta string) {
	s.mu.Lock()
	s.text += delta
	text := s.text
	shouldFlush := false
	if isSentenceEnd(text) || len([]rune(text)) >= s.maxChars {
		shouldFlush = true
		s.text = ""
	}
	s.mu.Unlock()
	if shouldFlush {
		s.flushText(text)
	}
}

func (s *sentenceBuffer) flush() {
	s.mu.Lock()
	text := s.text
	s.text = ""
	s.mu.Unlock()
	if text != "" {
		s.flushText(text)
	}
}

func (s *sentenceBuffer) flushText(text string) {
	if s.onFlush == nil {
		return
	}
	s.onFlush(text)
}

// runRealtimeCartesia is the entry point for
// LIVEKIT_WORKER_MODE=realtime-cartesia. It uses the same
// LiveKit / Realtime bridge as runRealtime for the inbound
// path (STT + LLM), but routes the assistant's text through
// Cartesia instead of the Realtime server-side TTS.
func (w *worker) runRealtimeCartesia(room *lksdk.Room) error {
	if w.openaiKey == "" {
		return errRealtime("OPENAI_API_KEY not set")
	}
	if w.cartesiaKey == "" {
		return errRealtime("CARTESIA_API_KEY not set (required for realtime-cartesia mode)")
	}

	// 1. Open OpenAI Realtime WebSocket and configure session.
	//    The session is configured with audio output format
	//    PCM16 24kHz anyway (mandatory schema field) but we
	//    ignore the server-side audio deltas — the only events
	//    we listen for are the transcript deltas.
	cfg := realtimeSessionConfig{
		Type:         "realtime",
		Instructions: w.rtInstructions,
		Audio: &realtimeAudioConfig{
			Input: &realtimeAudioInput{
				Format: &realtimeAudioFormat{
					Type: "audio/pcm",
					Rate: 24000,
				},
				Transcription: &inputTranscription{
					Model: "whisper-1",
				},
				TurnDetection: &turnDetection{
					Type:              "server_vad",
					Threshold:         w.rtVadThreshold,
					PrefixPaddingMs:   w.rtVadPrefixMs,
					SilenceDurationMs: w.rtVadSilenceMs,
				},
			},
			Output: &realtimeAudioOutput{
				Format: &realtimeAudioFormat{
					Type: "audio/pcm",
					Rate: 24000,
				},
				Voice: w.rtVoice,
			},
		},
	}
	rt, err := newRealtimeClient(w.openaiKey, w.rtModel, cfg)
	if err != nil {
		return err
	}
	w.rt = rt
	latencyLog("realtime_connected")

	// 2. Start the outbound PCM -> Opus encoder subprocess.
	//    It lives for the lifetime of the worker. Cartesia's
	//    PCM is written to it via w.encodePcm.
	if err := w.startOutboundEncoder(); err != nil {
		return err
	}
	latencyLog("outbound_encoder_started")

	// 3. Set up the sentence buffer + Cartesia flush callback.
	//    Each flushed sentence is POSTed to Cartesia; the
	//    returned PCM is written straight to the outbound
	//    encoder. The flush is synchronous within the callback
	//    so we don't lose track of order, but a single
	//    sentence's TTS is fast enough that the gap between
	//    sentences is the natural delay.
	sb := newSentenceBuffer(200, func(text string) {
		if text == "" {
			return
		}
		t0 := time.Now()
		log.Printf("cartesia_flush: text=%q", text)
		if err := StreamSynthesize(w.cartesiaKey, text, w.voiceID, w.modelID,
			w.cartesiaEnc, w.cartesiaRate, w.encodePcm); err != nil {
			log.Printf("cartesia_stream_failed: %v", err)
			return
		}
		log.Printf("cartesia_done: bytes_text=%d latency_ms=%d", len(text), time.Since(t0).Milliseconds())
	})

	// 4. Register callbacks. We deliberately do NOT set OnAudio
	//    — that's the path that's broken. We listen only to
	//    transcripts and lifecycle events.
	var userTurnCount int
	rt.OnUserTranscript = func(text string) {
		userTurnCount++
		log.Printf("realtime_user[%d]: %q", userTurnCount, text)
		latencyLog("user_turn_transcribed")
	}
	rt.OnAssistantTranscript = func(delta string) {
		sb.push(delta)
	}
	rt.OnError = func(e map[string]interface{}) {
		log.Printf("realtime_error_event: %v", e)
	}

	// 5. End-of-turn hooks: when Realtime finishes a response,
	//    flush any remaining buffered text. The
	//    response.output_audio_transcript.done event fires
	//    after all deltas have been sent, so by the time the
	//    session-level "response done" arrives, the buffer
	//    is at its end-of-turn state.
	//
	//    We piggyback on the existing UNHANDLED log path for
	//    output_audio_transcript.done by wrapping it via a
	//    hook in the realtimeClient. To keep this file
	//    self-contained, we use a different approach: poll
	//    for end-of-turn via a one-shot timer in the
	//    OnAssistantTranscript that resets whenever a delta
	//    arrives. Simpler: the sentence buffer flushes
	//    immediately on sentence-end punctuation, so the
	//    remaining buffer at response.done is small (just
	//    the last fragment, if any). We can also add a
	//    periodic flush every 2s of silence as a safety net.

	// 6. Force the initial greeting. Server VAD only fires on
	//    user audio; we have to ask for the first reply
	//    explicitly.
	if err := rt.createResponse("Please greet the caller warmly and ask how you can help."); err != nil {
		log.Printf("realtime: initial createResponse failed: %v", err)
	}

	// 7. Safety-net flush: every 2 seconds, flush any text
	//    that has been sitting in the buffer (e.g. a
	//    sentence without terminal punctuation, or a
	//    trailing fragment after the last period). This
	//    prevents audio from never being sent if the LLM
	//    trail-off-cuts without ending punctuation.
	go w.sentenceBufferSafetyNet(sb)

	return nil
}

// sentenceBufferSafetyNet periodically flushes any text
// sitting in the sentence buffer. This catches fragments
// without terminal punctuation (LLM cut off mid-thought, or
// trailing fragment after the last period) so the user
// always hears the full reply.
func (w *worker) sentenceBufferSafetyNet(sb *sentenceBuffer) {
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for range tick.C {
		sb.flush()
	}
}
