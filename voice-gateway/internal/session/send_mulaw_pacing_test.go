package session

import (
	"context"
	"testing"
	"time"

	"github.com/voxlane/voice-gateway/internal/config"
	"github.com/voxlane/voice-gateway/internal/provider"
)

// minimalMockAdapter implements provider.Adapter with just enough behavior
// to exercise sendMulawToTwilio without real provider I/O. The type
// deliberately does NOT match *providertwilio.Adapter or *providertelnyx.Adapter
// so sendProviderMessage's type-switch falls through (no real WS write,
// no noteCartesiaOutboundFrame call) while EncodeAudio still returns a
// non-nil message so the frame counter is incremented.
type minimalMockAdapter struct {
	encoded []byte
	err     error
}

func (m *minimalMockAdapter) Type() provider.Type { return provider.TypeTelnyx }
func (m *minimalMockAdapter) ValidateRequest(ctx context.Context, headers map[string]string, body []byte) (string, error) {
	return "test-call", nil
}
func (m *minimalMockAdapter) GenerateCallControl(callID string, ctrl provider.CallControlResponse) ([]byte, string, error) {
	return nil, "", nil
}
func (m *minimalMockAdapter) ParseMediaEvent(raw []byte) (*provider.AudioFrame, *provider.Event) {
	return nil, nil
}
func (m *minimalMockAdapter) EncodeAudio(frame provider.AudioFrame) ([]byte, error) {
	return m.encoded, m.err
}
func (m *minimalMockAdapter) EncodeMark(label string) ([]byte, error) { return nil, nil }
func (m *minimalMockAdapter) CloseMessage() []byte                   { return nil }
func (m *minimalMockAdapter) CallID() string                          { return "test-call" }
func (m *minimalMockAdapter) StreamID() string                        { return "test-stream" }
func (m *minimalMockAdapter) Close() error                            { return nil }

func newPacingTestSession() *Session {
	return &Session{
		ID:          "test-pacing",
		Config:      &config.Config{},
		provAdapter: &minimalMockAdapter{encoded: []byte{0x00}, err: nil},
	}
}

// TestSendMulawToTwilio_FrameCountAndBuffer verifies the 160-byte u-law
// frame size (20ms at 8kHz) and the cartesiaRemain carry-over.
func TestSendMulawToTwilio_FrameCountAndBuffer(t *testing.T) {
	cases := []struct {
		name           string
		input          int
		wantFrames     int
		wantRemainder  int
	}{
		{"exact 1 frame", 160, 1, 0},
		{"exact 2 frames", 320, 2, 0},
		{"1 frame + 80 remainder", 240, 1, 80},
		{"1 frame + 159 remainder", 319, 1, 159},
		{"0 frames (under 160)", 80, 0, 80},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newPacingTestSession()
			s.cartesiaRemain = nil
			got := s.sendMulawToTwilio(make([]byte, tc.input))
			if got != tc.wantFrames {
				t.Errorf("frames: got %d, want %d", got, tc.wantFrames)
			}
			if len(s.cartesiaRemain) != tc.wantRemainder {
				t.Errorf("remainder: got %d, want %d", len(s.cartesiaRemain), tc.wantRemainder)
			}
		})
	}
}

// TestSendMulawToTwilio_PacingAt50fps verifies that each 160-byte PCMU
// frame is paced at 20ms (50fps). Without the time.Sleep in the loop,
// 10 frames would complete in microseconds; with pacing, the call must
// take at least 180ms (allowing 10% scheduler slop per frame).
func TestSendMulawToTwilio_PacingAt50fps(t *testing.T) {
	s := newPacingTestSession()
	tenFrames := make([]byte, 1600)

	start := time.Now()
	n := s.sendMulawToTwilio(tenFrames)
	elapsed := time.Since(start)

	if n != 10 {
		t.Fatalf("frame count: got %d, want 10", n)
	}
	const minTotal = 10 * 18 * time.Millisecond
	if elapsed < minTotal {
		t.Errorf("pacing too fast: elapsed=%v, want >= %v (10 frames at 18ms minimum, 20ms target)", elapsed, minTotal)
	}
	if len(s.cartesiaRemain) != 0 {
		t.Errorf("remainder: got %d, want 0", len(s.cartesiaRemain))
	}
}
