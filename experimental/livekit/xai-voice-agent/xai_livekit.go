// xai_livekit.go — LiveKit <-> xAI Voice Agent audio bridge.
//
// Audio format chain (Plan D, L16 outbound test r8):
//
//   browser -> LiveKit inbound RTP Opus 48kHz stereo
//     -> samplebuilder (Opus depacketizer) -> raw Opus frames
//     -> oggMuxer (one Opus frame per OGG page) -> ffmpeg decode
//     -> PCM16 24kHz mono -> xai.AppendAudio (100ms chunks)
//     -> xAI server VAD / Grok / Eve
//     -> xai audio delta (PCM16 24kHz mono)
//     -> pcmRingBuffer (no encoder, no OGG layer)
//     -> pcmProvider.NextSample returns 20ms PCM chunks
//     -> LocalSampleTrack codec=L16 24kHz mono
//     -> LiveKit RTP -> browser
//
// The L16 outbound path is the r8 test (per manager's gate: if r7
// still has cut-offs, run one L16 test before declaring NO-GO).
// Hypothesis: the ffmpeg Opus encoder + OGG muxer was introducing
// page-buffer bursts that starved LiveKit's 20ms pacer. Bypassing
// the entire encoder/OGG layer and sending raw PCM directly via
// L16 should eliminate the cut-offs if that's the cause.
//
// Inbound path is unchanged: browser -> OGG mux -> ffmpeg decode ->
// xAI. We still need the inbound OGG/ffmpeg because LiveKit cloud
// delivers Opus, and xAI wants PCM.
package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/joho/godotenv"
	"github.com/livekit/protocol/auth"
	lksdk "github.com/livekit/server-sdk-go"
	"github.com/livekit/server-sdk-go/pkg/samplebuilder"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

const (
	// ffmpeg: inbound decode, OGG Opus on stdin -> PCM16 24kHz mono
	inboundFFmpegArgs = "-hide_banner -loglevel error -f ogg -i pipe:0 -af aresample=24000 -ac 1 -ar 24000 -f s16le pipe:1"

	// L16 outbound constants
	//
	// L16 24kHz mono: 24000 samples/sec, 2 bytes/sample, 1 channel
	// = 48000 bytes/sec. Each 20ms frame is 480 samples = 960 bytes.
	// pcmFrameBytes is the size of a single 20ms PCM chunk we hand
	// to LocalSampleTrack.WriteSample.
	pcmSampleRate = 24000
	pcmChannels   = 1
	pcmFrameDur   = 20 * time.Millisecond
	pcmFrameBytes = pcmSampleRate * pcmChannels * 2 * int(pcmFrameDur/time.Second) / 1000 // 24000*1*2*20/1000 = 960
	// l16MimeType is the WebRTC MIME for raw 16-bit linear PCM. Pion's
	// webrtc package does not export a constant for L16, so we use
	// the string. The SDP will advertise "a=rtpmap:<pt> L16/24000/1".
	l16MimeType = "audio/L16"
)

// pcmRingBuffer is a thread-safe byte queue used to bridge the xAI
// read-loop goroutine (writer) and the LocalSampleTrack write worker
// goroutine (reader). xAI audio deltas are PCM16 24kHz mono byte
// streams; the reader pulls exactly pcmFrameBytes (960) at a time
// to satisfy LiveKit's 20ms outbound cadence.
//
// We use a chan []byte (capacity-buffered) plus an internal carry
// buffer to handle the case where xAI's audio delta doesn't align
// with our 960-byte frame boundary.
type pcmRingBuffer struct {
	ch   chan []byte
	have []byte // leftover bytes from a previous read
}

func newPcmRingBuffer(capacity int) *pcmRingBuffer {
	return &pcmRingBuffer{ch: make(chan []byte, capacity)}
}

// Write is called by the xai.OnAudioDelta callback (write side).
// It does a non-blocking send; if the channel is full, the chunk is
// dropped (logged). For L16 audio that's a brief glitch, not silence.
func (r *pcmRingBuffer) Write(pcm []byte) {
	if len(pcm) == 0 {
		return
	}
	// Copy because the xAI caller's slice may be reused.
	b := make([]byte, len(pcm))
	copy(b, pcm)
	select {
	case r.ch <- b:
	default:
		// Channel full — drop and log. The reader is paced at
		// 20ms; if we overflow, the xAI delta is arriving faster
		// than we can play it back.
		log.Printf("pcm ring buffer: drop %d bytes (channel full)", len(pcm))
	}
}

// ReadFrame returns exactly pcmFrameBytes (960) of PCM, blocking
// until enough data is available, or returns io.EOF if the channel
// is closed.
func (r *pcmRingBuffer) ReadFrame() ([]byte, error) {
	out := make([]byte, pcmFrameBytes)
	written := 0
	// Drain carry first.
	if len(r.have) > 0 {
		n := copy(out, r.have)
		written += n
		r.have = r.have[n:]
	}
	for written < pcmFrameBytes {
		b, ok := <-r.ch
		if !ok {
			if written == 0 {
				return nil, io.EOF
			}
			// Channel closed mid-frame: return a short frame.
			return out[:written], nil
		}
		need := pcmFrameBytes - written
		if len(b) <= need {
			copy(out[written:], b)
			written += len(b)
		} else {
			copy(out[written:], b[:need])
			written += need
			r.have = append(r.have, b[need:]...)
		}
	}
	return out, nil
}

// pcmProvider is the LiveKit SampleProvider that returns 20ms PCM
// chunks from the ring buffer. It implements AudioSampleProvider so
// the LocalSampleTrack can compute audio level for VAD/UI.
type pcmProvider struct {
	buf   *pcmRingBuffer
	close chan struct{}
}

func newPcmProvider(buf *pcmRingBuffer) *pcmProvider {
	return &pcmProvider{buf: buf, close: make(chan struct{})}
}

func (p *pcmProvider) NextSample() (media.Sample, error) {
	select {
	case <-p.close:
		return media.Sample{}, io.EOF
	default:
	}
	data, err := p.buf.ReadFrame()
	if err != nil {
		return media.Sample{}, err
	}
	return media.Sample{Data: data, Duration: pcmFrameDur}, nil
}
func (p *pcmProvider) OnBind() error           { return nil }
func (p *pcmProvider) OnUnbind() error         { return nil }
func (p *pcmProvider) Close() error            { close(p.close); return nil }
func (p *pcmProvider) CurrentAudioLevel() uint8 { return 60 }

func runLiveKitBridge(ctx context.Context, cfg *config) error {
	token, err := buildLiveKitToken(cfg)
	if err != nil {
		return fmt.Errorf("build token: %w", err)
	}
	log.Printf("joining LiveKit room %q as %q", cfg.roomName, cfg.identity)
	room, err := lksdk.ConnectToRoomWithToken(cfg.livekitURL, token, &lksdk.RoomCallback{
		ParticipantCallback: lksdk.ParticipantCallback{
			OnTrackSubscribed: func(track *webrtc.TrackRemote, pub *lksdk.RemoteTrackPublication, rp *lksdk.RemoteParticipant) {
				if track.Kind() == webrtc.RTPCodecTypeAudio {
					go handleInboundTrack(ctx, cfg, track, rp)
				}
			},
		},
	}, lksdk.WithAutoSubscribe(true))
	if err != nil {
		return fmt.Errorf("connect to room: %w", err)
	}
	defer room.Disconnect()

	// L16 24kHz mono outbound track. Replaces the previous Opus 48kHz
	// track. PCM samples are written directly via the SampleProvider;
	// no encoder, no OGG layer, no ffmpeg.
	outbound, err := lksdk.NewLocalSampleTrack(webrtc.RTPCodecCapability{
		MimeType:  l16MimeType,
		ClockRate: uint32(pcmSampleRate),
		Channels:  uint16(pcmChannels),
	})
	if err != nil {
		return fmt.Errorf("create outbound L16 track: %w", err)
	}
	pcmBuf := newPcmRingBuffer(64)
	provider := newPcmProvider(pcmBuf)
	pub, err := room.LocalParticipant.PublishTrack(outbound, &lksdk.TrackPublicationOptions{Name: "xai-voice"})
	if err != nil {
		return fmt.Errorf("publish track: %w", err)
	}
	log.Printf("published outbound L16 track 'xai-voice' (id=%s mime=%s clockRate=%d channels=%d)",
		pub.SID(), pub.MimeType(), pcmSampleRate, pcmChannels)
	if err := outbound.StartWrite(provider, func() {
		log.Println("outbound playback complete (unexpected; provider is never closed)")
	}); err != nil {
		return fmt.Errorf("start write: %w", err)
	}

	xai, err := newXaiClient(cfg)
	if err != nil {
		return fmt.Errorf("xai connect: %w", err)
	}
	defer xai.Close()

	inboundFFmpeg, err := startInboundFFmpeg(ctx)
	if err != nil {
		return fmt.Errorf("start inbound ffmpeg: %w", err)
	}
	defer inboundFFmpeg.Close()

	var bridgeWg sync.WaitGroup

	// Pipe xAI audio deltas (PCM16 24kHz mono int16) into the PCM
	// ring buffer. We LittleEndian-encode each int16 sample to a
	// 2-byte little-endian word, matching xAI's documented PCM byte
	// order.
	xai.OnAudioDelta = func(pcm []int16) {
		b := make([]byte, len(pcm)*2)
		for i, s := range pcm {
			binary.LittleEndian.PutUint16(b[i*2:], uint16(s))
		}
		pcmBuf.Write(b)
	}
	xai.OnTranscript = func(role, text string) {
		log.Printf("xai transcript [%s]: %s", role, text)
	}
	xai.OnError = func(err error) {
		log.Printf("xai error: %v", err)
	}

	// Function-call bridge: receive the model's tool call, dispatch to
	// the stub, and send the function_call_output back with the real
	// call_id (set by xai_client.go from function_call_arguments.done).
	// Production worker will replace dispatchToolCall with a real
	// booking/availability dispatcher.
	xai.OnFunctionCall = func(name string, args map[string]any) {
		dispatchStart := time.Now()
		callID := ""
		if v := xai.latestCallID.Load(); v != nil {
			if s, ok := v.(string); ok {
				callID = s
			}
		}
		argsSummary := summarizeArgs(args)
		log.Printf("METRIC function_call_received name=%s call_id=%s args=%s turn_id=%d",
			name, callID, argsSummary, atomic.LoadInt64(&xai.turnCounter))

		result := dispatchToolCall(name, args)
		log.Printf("METRIC function_call_dispatched name=%s call_id=%s result=%s dispatch_ms=%d",
			name, callID, summarizeResult(result), time.Since(dispatchStart).Milliseconds())

		expectResumedFor = name
		expectResumedAt = time.Now()

		if err := xai.SendFunctionResult(name, args, result); err != nil {
			log.Printf("METRIC function_call_output_failed name=%s call_id=%s err=%v", name, callID, err)
			return
		}
		log.Printf("METRIC function_call_output_sent name=%s call_id=%s", name, callID)
	}
	xai.OnResponseDone = func() {
		if expectResumedFor != "" {
			resumeLatency := time.Since(expectResumedAt).Milliseconds()
			log.Printf("METRIC assistant_resumed_after_tool name=%s resume_ms=%d", expectResumedFor, resumeLatency)
			expectResumedFor = ""
			expectResumedAt = time.Time{}
		}
	}

	// Pipe inbound ffmpeg PCM output into xAI as 100ms chunks
	bridgeWg.Add(1)
	go func() {
		defer bridgeWg.Done()
		buf := make([]byte, xaiChunkBytes)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			n, err := io.ReadFull(inboundFFmpeg.stdout, buf)
			if err != nil {
				if err == io.EOF || err == io.ErrUnexpectedEOF {
					return
				}
				log.Printf("inbound ffmpeg read error: %v", err)
				return
			}
			if err := xai.AppendAudio(buf[:n]); err != nil {
				log.Printf("xai AppendAudio error: %v", err)
				return
			}
		}
	}()

	bridgeWg.Add(1)
	go func() {
		defer bridgeWg.Done()
		if err := xai.ReadEvents(ctx); err != nil {
			log.Printf("xai read loop ended: %v", err)
		}
	}()

	log.Printf("xai-voice-agent LIVE (L16 outbound). Press Ctrl+C to stop.")
	<-ctx.Done()
	log.Printf("xai-voice-agent shutting down")
	bridgeWg.Wait()
	return nil
}

// handleInboundTrack is called for each subscribed audio track from a
// remote participant. We assemble Opus frames via samplebuilder, wrap
// each in an OGG page, and forward to the inbound ffmpeg decoder.
func handleInboundTrack(ctx context.Context, cfg *config, track *webrtc.TrackRemote, rp *lksdk.RemoteParticipant) {
	log.Printf("subscribed to track %s from %s (codec=%s)", track.ID(), rp.Identity(), track.Codec().MimeType)
	// Wait for the inbound ffmpeg to be initialized; the OnTrackSubscribed
	// callback can fire before startInboundFFmpeg completes.
	select {
	case <-inboundFFmpegReady:
	case <-ctx.Done():
		return
	case <-time.After(5 * time.Second):
		log.Printf("inbound ffmpeg not ready after 5s; dropping track %s", track.ID())
		return
	}
	inbound := getInboundFFmpegStdin()
	if inbound == nil {
		log.Printf("no inbound ffmpeg available; dropping track")
		return
	}
	ogg := newOggMuxer(inbound, 0xDEADBEEF, 2 /* LiveKit emits stereo Opus */, 48000)
	if err := ogg.writeOpusHead(); err != nil {
		log.Printf("ogg writeOpusHead: %v", err)
		return
	}
	if err := ogg.writeOpusTags(); err != nil {
		log.Printf("ogg writeOpusTags: %v", err)
		return
	}
	sb := samplebuilder.New(20, &codecs.OpusPacket{}, 48000)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		rtpPacket, _, err := track.ReadRTP()
		if err != nil {
			log.Printf("ReadRTP error on track %s: %v", track.ID(), err)
			return
		}
		sb.Push(rtpPacket)
		for {
			sample := sb.Pop()
			if sample == nil {
				break
			}
			// A 20ms Opus frame at 48kHz has 960 samples (mono);
			// for stereo it's still 960 samples per channel
			// (20ms of 48kHz audio). The granule is per-channel.
			// 960 samples per channel is the OGG granule we use.
			if err := ogg.writeOpusFrame(sample.Data, 960); err != nil {
				log.Printf("ogg writeOpusFrame: %v", err)
				return
			}
		}
	}
}

// --- ffmpeg plumbing (inbound only; outbound uses L16) ---

type inboundFFmpegHandle struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
}

var (
	inboundFFmpegOnce  sync.Once
	inboundFFmpegInst  *inboundFFmpegHandle
	inboundFFmpegReady = make(chan struct{})
)

func startInboundFFmpeg(ctx context.Context) (*inboundFFmpegHandle, error) {
	var err error
	inboundFFmpegOnce.Do(func() {
		cmd := exec.CommandContext(ctx, "ffmpeg", splitArgs(inboundFFmpegArgs)...)
		stdin, e1 := cmd.StdinPipe()
		stdout, e2 := cmd.StdoutPipe()
		if e1 != nil || e2 != nil {
			err = fmt.Errorf("stdin/stdout pipes: %v %v", e1, e2)
			return
		}
		cmd.Stderr = os.Stderr
		if e := cmd.Start(); e != nil {
			err = fmt.Errorf("start ffmpeg: %w", e)
			return
		}
		inboundFFmpegInst = &inboundFFmpegHandle{cmd: cmd, stdin: stdin, stdout: stdout}
		log.Printf("inbound ffmpeg started (pid=%d)", cmd.Process.Pid)
		close(inboundFFmpegReady)
	})
	return inboundFFmpegInst, err
}

func (h *inboundFFmpegHandle) Close() error {
	if h == nil {
		return nil
	}
	_ = h.stdin.Close()
	_ = h.stdout.Close()
	if h.cmd.Process != nil {
		return h.cmd.Process.Kill()
	}
	return nil
}

func getInboundFFmpegStdin() io.WriteCloser {
	if inboundFFmpegInst == nil {
		return nil
	}
	return inboundFFmpegInst.stdin
}

func splitArgs(s string) []string {
	var args []string
	cur := ""
	for _, r := range s {
		if r == ' ' {
			if cur != "" {
				args = append(args, cur)
				cur = ""
			}
		} else {
			cur += string(r)
		}
	}
	if cur != "" {
		args = append(args, cur)
	}
	return args
}

func buildLiveKitToken(cfg *config) (string, error) {
	_ = godotenv.Load()
	at := auth.NewAccessToken(cfg.apiKey, cfg.apiSecret)
	yes := true
	at.AddGrant(&auth.VideoGrant{
		RoomJoin:       true,
		Room:           cfg.roomName,
		CanPublish:     &yes,
		CanSubscribe:   &yes,
		CanPublishData: &yes,
	})
	at.SetIdentity(cfg.identity)
	at.SetValidFor(2 * time.Hour)
	return at.ToJWT()
}

// --- function-call bridge (Plan D harness, stub dispatcher) ---

// expectResumedFor / expectResumedAt are package-level because the
// OnFunctionCall and OnResponseDone callbacks are set in
// runLiveKitBridge but fire later from the xai ReadEvents goroutine.
// We use these to attribute the next assistant response back to the
// tool call that prompted it.
var (
	expectResumedFor string
	expectResumedAt  time.Time
)

// dispatchToolCall is the deterministic stub dispatcher for the
// harness. It does NOT touch the network, the booking provider, or
// the manager escalation system — those are wired in the production
// worker. The point here is to prove the round trip:
//
//   1. xAI emits function_call
//   2. harness receives it
//   3. harness logs it
//   4. harness sends function_call_output with the real call_id
//   5. xAI continues speaking naturally
func dispatchToolCall(name string, args map[string]any) map[string]any {
	switch name {
	case "availability.check":
		// Always available, single fixed slot. Production will hit
		// Rezz diary here.
		return map[string]any{
			"available":  true,
			"next_slot":  "19:00",
			"message":    "A table is available.",
		}
	case "booking.create":
		// Confirm the booking with a synthetic confirmation id. Real
		// worker will write to the booking system + send SMS.
		return map[string]any{
			"status":           "created",
			"confirmation_id":  "TEST-1234",
		}
	case "manager.escalate":
		// Record a synthetic message; manager callback queue in
		// production. For the harness, this just acknowledges.
		return map[string]any{
			"status":            "message_taken",
			"callback_required": true,
		}
	default:
		// Unknown tool — tell the model we don't know what this is.
		// xAI will likely try to recover, or simply acknowledge and
		// move on. The harness test logs the unknown name loudly.
		return map[string]any{
			"error":  "unknown_tool",
			"detail": fmt.Sprintf("dispatcher has no handler for %q", name),
		}
	}
}

// summarizeArgs returns a compact, log-safe representation of the
// function-call arguments. Phone numbers and PII are kept in the
// log for traceability in this harness run (no real PII — synthetic).
func summarizeArgs(args map[string]any) string {
	if len(args) == 0 {
		return "{}"
	}
	parts := make([]string, 0, len(args))
	for k, v := range args {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	return "{" + joinStrings(parts, ", ") + "}"
}

// summarizeResult returns a compact one-line summary of a tool result
// for log output. Truncates long values.
func summarizeResult(r map[string]any) string {
	if len(r) == 0 {
		return "{}"
	}
	parts := make([]string, 0, len(r))
	for k, v := range r {
		s := fmt.Sprintf("%v", v)
		if len(s) > 60 {
			s = s[:60] + "..."
		}
		parts = append(parts, fmt.Sprintf("%s=%s", k, s))
	}
	return "{" + joinStrings(parts, ", ") + "}"
}

// joinStrings is a tiny helper because we don't want to import strings
// just for this. Avoids the loop pattern below.
func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += sep + p
	}
	return out
}
