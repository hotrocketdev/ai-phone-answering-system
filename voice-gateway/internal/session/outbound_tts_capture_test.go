package session

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/voxlane/voice-gateway/internal/provider"
)

// TestNoteOutboundPCMUFrame_NoOpWhenEnvUnset confirms the capture is
// never created and never written unless DEBUG_OUTBOUND_TTS_CAPTURE=true.
// This is the default behavior in production.
func TestNoteOutboundPCMUFrame_NoOpWhenEnvUnset(t *testing.T) {
	os.Unsetenv("DEBUG_OUTBOUND_TTS_CAPTURE")
	s := newPacingTestSession()
	for i := 0; i < 10; i++ {
		s.noteOutboundPCMUFrame(make([]byte, 160))
	}
	if s.outboundTTS != nil {
		t.Fatalf("outbound capture created despite env flag being unset")
	}
}

// TestNoteOutboundPCMUFrame_CapturesWhenEnabled confirms the capture is
// created lazily on the first frame once the env flag is set.
func TestNoteOutboundPCMUFrame_CapturesWhenEnabled(t *testing.T) {
	t.Setenv("DEBUG_OUTBOUND_TTS_CAPTURE", "true")
	s := newPacingTestSession()
	s.noteOutboundPCMUFrame(make([]byte, 160))
	if s.outboundTTS == nil {
		t.Fatalf("outbound capture not created despite env flag being set")
	}
	if s.outboundTTS.frames != 1 {
		t.Errorf("frames: got %d, want 1", s.outboundTTS.frames)
	}
}

// TestOutboundTTSCapture_IgnoresWrongSizedFrame guards against a partial
// frame being written into the capture. The producer (sendMulawToTwilio)
// only ever hands us 160-byte frames; anything else is a contract bug.
func TestOutboundTTSCapture_IgnoresWrongSizedFrame(t *testing.T) {
	c := newOutboundTTSCapture("test-bad-frame")
	for _, size := range []int{0, 1, 80, 159, 161, 320} {
		c.AddPCMUFrame(make([]byte, size), "test-bad-frame")
	}
	if c.frames != 0 {
		t.Errorf("frames: got %d, want 0 (only 160-byte frames should be captured)", c.frames)
	}
	if len(c.pcmu) != 0 {
		t.Errorf("pcmu bytes: got %d, want 0", len(c.pcmu))
	}
}

// TestOutboundTTSCapture_WritesExpectedFiles verifies the three output
// files exist with the right sizes: 160 bytes PCMU per frame, 320 bytes
// PCM16 8k per frame, and WAV = 44 header + 320 * frames payload.
func TestOutboundTTSCapture_WritesExpectedFiles(t *testing.T) {
	tmp := t.TempDir()
	callID := "test-files"
	c := newOutboundTTSCapture(callID)
	c.pcmuPath = filepath.Join(tmp, "voxlane-outbound-"+callID+".pcmu")
	c.pcm8Path = filepath.Join(tmp, "voxlane-outbound-"+callID+".pcm16_8k")
	c.wavPath = filepath.Join(tmp, "voxlane-outbound-"+callID+".wav")

	const nFrames = 5
	for i := 0; i < nFrames; i++ {
		c.AddPCMUFrame(make([]byte, 160), callID)
	}
	c.Close(callID)

	pcmu, err := os.ReadFile(c.pcmuPath)
	if err != nil {
		t.Fatalf("pcmu read: %v", err)
	}
	if want := 160 * nFrames; len(pcmu) != want {
		t.Errorf("pcmu size: got %d, want %d", len(pcmu), want)
	}

	pcm, err := os.ReadFile(c.pcm8Path)
	if err != nil {
		t.Fatalf("pcm read: %v", err)
	}
	if want := 320 * nFrames; len(pcm) != want {
		t.Errorf("pcm size: got %d, want %d", len(pcm), want)
	}

	wav, err := os.ReadFile(c.wavPath)
	if err != nil {
		t.Fatalf("wav read: %v", err)
	}
	const headerSize = 44
	if want := headerSize + 320*nFrames; len(wav) != want {
		t.Errorf("wav size: got %d, want %d", len(wav), want)
	}
	if string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		t.Errorf("wav header malformed: riff=%q wave=%q", string(wav[:4]), string(wav[8:12]))
	}
	sampleRate := binary.LittleEndian.Uint32(wav[24:28])
	if sampleRate != 8000 {
		t.Errorf("wav sample rate: got %d, want 8000", sampleRate)
	}
}

// TestOutboundTTSCapture_ClosesAtFrameCap verifies the capture auto-closes
// at debugOutboundCaptureFrames so a long call can't fill /tmp.
func TestOutboundTTSCapture_ClosesAtFrameCap(t *testing.T) {
	tmp := t.TempDir()
	callID := "test-cap"
	c := newOutboundTTSCapture(callID)
	c.pcmuPath = filepath.Join(tmp, "voxlane-outbound-"+callID+".pcmu")
	c.pcm8Path = filepath.Join(tmp, "voxlane-outbound-"+callID+".pcm16_8k")
	c.wavPath = filepath.Join(tmp, "voxlane-outbound-"+callID+".wav")

	for i := 0; i < debugOutboundCaptureFrames+5; i++ {
		c.AddPCMUFrame(make([]byte, 160), callID)
	}
	if c.frames != debugOutboundCaptureFrames {
		t.Errorf("frames: got %d, want %d (cap)", c.frames, debugOutboundCaptureFrames)
	}
	if !c.closed {
		t.Errorf("capture should be auto-closed at cap")
	}
	pcmu, err := os.ReadFile(c.pcmuPath)
	if err != nil {
		t.Fatalf("pcmu read: %v", err)
	}
	if want := 160 * debugOutboundCaptureFrames; len(pcmu) != want {
		t.Errorf("pcmu size: got %d, want %d", len(pcmu), want)
	}

	c.AddPCMUFrame(make([]byte, 160), callID)
	if c.frames != debugOutboundCaptureFrames {
		t.Errorf("frames changed after close: got %d", c.frames)
	}
}

// TestOutboundTTSCapture_EmptyCloseNoop verifies Close on a never-used
// capture does not create empty files (avoids noise in /tmp).
func TestOutboundTTSCapture_EmptyCloseNoop(t *testing.T) {
	tmp := t.TempDir()
	callID := "test-empty"
	c := newOutboundTTSCapture(callID)
	c.pcmuPath = filepath.Join(tmp, "voxlane-outbound-"+callID+".pcmu")
	c.pcm8Path = filepath.Join(tmp, "voxlane-outbound-"+callID+".pcm16_8k")
	c.wavPath = filepath.Join(tmp, "voxlane-outbound-"+callID+".wav")

	c.Close(callID)
	for _, p := range []string{c.pcmuPath, c.pcm8Path, c.wavPath} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("empty capture wrote file: %s", p)
		}
	}
}

// TestSendMulawToTwilio_EmitsCaptureFrames confirms the production hot
// path populates the capture when the env flag is set. This is the
// integration test that ties sendMulawToTwilio to noteOutboundPCMUFrame.
func TestSendMulawToTwilio_EmitsCaptureFrames(t *testing.T) {
	t.Setenv("DEBUG_OUTBOUND_TTS_CAPTURE", "true")
	s := newPacingTestSession()
	const wantFrames = 10
	n := s.sendMulawToTwilio(make([]byte, 160*wantFrames))
	if n != wantFrames {
		t.Fatalf("sendMulawToTwilio frames: got %d, want %d", n, wantFrames)
	}
	if s.outboundTTS == nil {
		t.Fatalf("outbound capture not created")
	}
	if s.outboundTTS.frames != wantFrames {
		t.Errorf("captured frames: got %d, want %d", s.outboundTTS.frames, wantFrames)
	}
	if got := len(s.outboundTTS.pcmu); got != 160*wantFrames {
		t.Errorf("captured pcmu bytes: got %d, want %d", got, 160*wantFrames)
	}
}

// TestNoteOutboundPCMUFrame_NonTelnyxAdapterIgnored guards against the
// capture accidentally firing on a Twilio or non-Telnyx adapter.
func TestNoteOutboundPCMUFrame_NonTelnyxAdapterIgnored(t *testing.T) {
	t.Setenv("DEBUG_OUTBOUND_TTS_CAPTURE", "true")
	s := newPacingTestSession()
	s.provAdapter = &twilioTypeMockAdapter{}
	for i := 0; i < 5; i++ {
		s.noteOutboundPCMUFrame(make([]byte, 160))
	}
	if s.outboundTTS != nil {
		t.Fatalf("outbound capture created for non-Telnyx adapter")
	}
}

// twilioTypeMockAdapter implements provider.Adapter but reports
// provider.TypeTwilio so we can prove the Telnyx-only gate fires.
type twilioTypeMockAdapter struct{}

func (m *twilioTypeMockAdapter) Type() provider.Type { return provider.TypeTwilio }
func (m *twilioTypeMockAdapter) ValidateRequest(ctx context.Context, headers map[string]string, body []byte) (string, error) {
	return "test-call", nil
}
func (m *twilioTypeMockAdapter) GenerateCallControl(callID string, ctrl provider.CallControlResponse) ([]byte, string, error) {
	return nil, "", nil
}
func (m *twilioTypeMockAdapter) ParseMediaEvent(raw []byte) (*provider.AudioFrame, *provider.Event) {
	return nil, nil
}
func (m *twilioTypeMockAdapter) EncodeAudio(frame provider.AudioFrame) ([]byte, error) {
	return []byte{0x00}, nil
}
func (m *twilioTypeMockAdapter) EncodeMark(label string) ([]byte, error) { return nil, nil }
func (m *twilioTypeMockAdapter) CloseMessage() []byte                   { return nil }
func (m *twilioTypeMockAdapter) CallID() string                          { return "test-call" }
func (m *twilioTypeMockAdapter) StreamID() string                        { return "test-stream" }
func (m *twilioTypeMockAdapter) Close() error                            { return nil }
