package session

import (
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/voxlane/voice-gateway/internal/audio"
	"github.com/voxlane/voice-gateway/internal/provider"
)

// debugOutboundCaptureFrames caps a single call's outbound TTS capture at
// 12 seconds of audio (600 PCMU frames at 20ms each). Enough for the
// static greeting plus a few early turns; if a call runs longer, the
// remaining frames are silently dropped and the partial capture is
// closed. Keep this in sync with the same constant on inbound.
const debugOutboundCaptureFrames = 600

// outboundTTSCapture writes the exact 160-byte PCMU frames that
// sendMulawToTwilio would otherwise hand to the provider's WebSocket.
// Default behavior: a no-op (env flag must be enabled). Files land in
// os.TempDir() with a session-scoped suffix so multiple concurrent
// calls never collide.
type outboundTTSCapture struct {
	mu       sync.Mutex
	enabled  bool
	closed   bool
	pcmuPath string
	pcm8Path string
	wavPath  string
	pcmu     []byte
	pcm16    []byte
	frames   int
}

func newOutboundTTSCapture(callID string) *outboundTTSCapture {
	safeID := regexp.MustCompile(`[^a-zA-Z0-9_.-]+`).ReplaceAllString(callID, "_")
	base := filepath.Join(os.TempDir(), "voxlane-outbound-"+safeID)
	c := &outboundTTSCapture{
		enabled:  true,
		pcmuPath: base + ".pcmu",
		pcm8Path: base + ".pcm16_8k",
		wavPath:  base + ".wav",
	}
	log.Printf("[%s] outbound TTS capture enabled pcmu=%s pcm8=%s wav=%s max_frames=%d",
		callID, c.pcmuPath, c.pcm8Path, c.wavPath, debugOutboundCaptureFrames)
	return c
}

// AddPCMUFrame appends a 160-byte PCMU frame to the capture buffer and
// decodes it to PCM16 8kHz for the WAV companion file. Callers must
// pass exactly one 20ms PCMU frame; the function is a no-op once the
// frame cap is reached.
func (c *outboundTTSCapture) AddPCMUFrame(frame []byte, callID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.enabled || c.closed || c.frames >= debugOutboundCaptureFrames {
		return
	}
	if len(frame) != 160 {
		return
	}
	c.pcmu = append(c.pcmu, frame...)
	pcm16, err := decodeUlawFrameToPCM16(frame)
	if err != nil {
		log.Printf("[%s] outbound TTS capture decode failed: %v", callID, err)
		// Roll back the PCMU append so file boundaries stay consistent.
		c.pcmu = c.pcmu[:len(c.pcmu)-160]
		return
	}
	c.pcm16 = append(c.pcm16, pcm16...)
	c.frames++

	if c.frames == debugOutboundCaptureFrames {
		c.closeLocked(callID)
	}
}

func (c *outboundTTSCapture) Close(callID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeLocked(callID)
}

func (c *outboundTTSCapture) closeLocked(callID string) {
	if c.closed || !c.enabled {
		return
	}
	c.closed = true
	if c.frames == 0 {
		return
	}
	if err := os.WriteFile(c.pcmuPath, c.pcmu, 0600); err != nil {
		log.Printf("[%s] outbound TTS capture pcmu write failed: %v", callID, err)
		return
	}
	if err := os.WriteFile(c.pcm8Path, c.pcm16, 0600); err != nil {
		log.Printf("[%s] outbound TTS capture pcm8 write failed: %v", callID, err)
		return
	}
	if err := writePCM16WAV(c.wavPath, c.pcm16, 8000); err != nil {
		log.Printf("[%s] outbound TTS capture wav write failed: %v", callID, err)
		return
	}
	log.Printf("[%s] outbound TTS capture saved pcmu=%s pcm8=%s wav=%s frames=%d pcmu_bytes=%d pcm16_bytes=%d duration_ms=%d",
		callID, c.pcmuPath, c.pcm8Path, c.wavPath, c.frames, len(c.pcmu), len(c.pcm16), c.frames*20)
}

// decodeUlawFrameToPCM16 is a thin wrapper around the audio package's
// G.711 decoder, scoped to PCMU 8kHz so the capture file is always
// single-codec regardless of what the provider is doing.
func decodeUlawFrameToPCM16(frame []byte) ([]byte, error) {
	return audio.G711ToPCM16("ulaw", frame)
}

// noteOutboundPCMUFrame is the session hook called once per 20ms PCMU
// frame right before the frame is handed to the provider adapter. It is
// a no-op unless the env flag DEBUG_OUTBOUND_TTS_CAPTURE=true is set
// AND the active provider is Telnyx (the only path that sends raw
// 8kHz PCMU frames to the wire). The first call lazily creates the
// capture struct, so the env check stays cheap.
func (s *Session) noteOutboundPCMUFrame(frame []byte) {
	if s.provAdapter == nil {
		return
	}
	if s.provAdapter.Type() != provider.TypeTelnyx {
		return
	}
	if os.Getenv("DEBUG_OUTBOUND_TTS_CAPTURE") != "true" {
		return
	}
	s.mu.Lock()
	if s.outboundTTS == nil {
		s.outboundTTS = newOutboundTTSCapture(s.ID)
	}
	c := s.outboundTTS
	s.mu.Unlock()
	c.AddPCMUFrame(frame, s.ID)
}
