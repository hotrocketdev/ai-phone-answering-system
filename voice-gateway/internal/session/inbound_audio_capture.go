package session

import (
	"encoding/binary"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/voxlane/voice-gateway/internal/audio"
	"github.com/voxlane/voice-gateway/internal/provider"
)

const debugCaptureFrames = 900 // 18 seconds at 20ms per PCMU frame.

type inboundAudioCapture struct {
	mu       sync.Mutex
	enabled  bool
	closed   bool
	pcmuPath string
	wavPath  string
	pcmu     []byte
	pcm16    []byte
	frames   int
}

func (s *Session) noteInboundFrame(frame provider.AudioFrame) {
	s.mu.Lock()
	s.inboundFrames++
	count := s.inboundFrames
	s.mu.Unlock()

	if count <= 5 || count%50 == 0 {
		log.Printf("[%s] inbound media frame count=%d codec=%s sample_rate=%d payload_len=%d direction=%s ts=%s",
			s.ID, count, frame.Codec, frame.SampleRate, len(frame.Payload), frame.Direction, frame.Timestamp)
	}

	if s.provAdapter.Type() != provider.TypeTelnyx || os.Getenv("DEBUG_TELNYX_CAPTURE_AUDIO") != "true" {
		return
	}
	if s.capture == nil {
		s.capture = newInboundAudioCapture(s.ID)
	}
	s.capture.Add(frame.Payload, s.ID)
}

func newInboundAudioCapture(callID string) *inboundAudioCapture {
	safeID := regexp.MustCompile(`[^a-zA-Z0-9_.-]+`).ReplaceAllString(callID, "_")
	base := filepath.Join(os.TempDir(), "voxlane-inbound-"+safeID)
	c := &inboundAudioCapture{
		enabled:  true,
		pcmuPath: base + ".pcmu",
		wavPath:  base + ".wav",
	}
	log.Printf("[%s] inbound audio capture enabled pcmu=%s wav=%s max_frames=%d", callID, c.pcmuPath, c.wavPath, debugCaptureFrames)
	return c
}

func (c *inboundAudioCapture) Add(pcmu []byte, callID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.enabled || c.closed || c.frames >= debugCaptureFrames {
		return
	}

	remaining := debugCaptureFrames - c.frames
	if remaining <= 0 {
		return
	}
	c.pcmu = append(c.pcmu, pcmu...)
	pcm16 := make([]byte, len(pcmu)*2)
	audio.MulawToPCM16(pcmu, pcm16)
	c.pcm16 = append(c.pcm16, pcm16...)
	c.frames++

	if c.frames == debugCaptureFrames {
		c.closeLocked(callID)
	}
}

func (c *inboundAudioCapture) Close(callID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeLocked(callID)
}

func (c *inboundAudioCapture) closeLocked(callID string) {
	if c.closed || !c.enabled {
		return
	}
	c.closed = true
	if len(c.pcmu) == 0 {
		return
	}
	if err := os.WriteFile(c.pcmuPath, c.pcmu, 0600); err != nil {
		log.Printf("[%s] inbound audio capture pcmu write failed: %v", callID, err)
		return
	}
	if err := writePCM16WAV(c.wavPath, c.pcm16, 8000); err != nil {
		log.Printf("[%s] inbound audio capture wav write failed: %v", callID, err)
		return
	}
	log.Printf("[%s] inbound audio capture saved pcmu=%s wav=%s frames=%d bytes=%d", callID, c.pcmuPath, c.wavPath, c.frames, len(c.pcmu))
}

func writePCM16WAV(path string, pcm16 []byte, sampleRate uint32) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	dataSize := uint32(len(pcm16))
	byteRate := sampleRate * 2
	blockAlign := uint16(2)
	if _, err := f.Write([]byte("RIFF")); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, uint32(36)+dataSize); err != nil {
		return err
	}
	if _, err := f.Write([]byte("WAVEfmt ")); err != nil {
		return err
	}
	for _, v := range []interface{}{
		uint32(16), uint16(1), uint16(1), sampleRate, byteRate, blockAlign, uint16(16),
	} {
		if err := binary.Write(f, binary.LittleEndian, v); err != nil {
			return err
		}
	}
	if _, err := f.Write([]byte("data")); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, dataSize); err != nil {
		return err
	}
	_, err = f.Write(pcm16)
	return err
}
