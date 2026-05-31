package telnyx

import (
	"encoding/binary"
	"testing"

	"github.com/voxlane/voice-gateway/internal/provider"
)

func TestRTPPacketizer(t *testing.T) {
	// Create a minimal adapter for testing
	a := &Adapter{
		callID:   "test-call",
		rtpSeq:   0,
		rtpTS:    48000,
		rtpSSRC:  0xDEADBEEF,
		rtpFirst: true,
	}

	pcmu := make([]byte, 160)
	for i := range pcmu {
		pcmu[i] = byte(i & 0xFF)
	}

	// Packet 1
	pkt, err := a.EncodeAudio(provider.AudioFrame{Payload: pcmu})
	if err != nil {
		t.Fatalf("EncodeAudio: %v", err)
	}
	if len(pkt) != 172 {
		t.Fatalf("packet length: want 172, got %d", len(pkt))
	}
	if pkt[0] != 0x80 {
		t.Fatalf("version: want 0x80, got 0x%02x", pkt[0])
	}
	if pkt[1] != 0x80 {
		t.Fatalf("marker + PT: want 0x80 (marker set, PT=0), got 0x%02x", pkt[1])
	}
	if seq := binary.BigEndian.Uint16(pkt[2:]); seq != 0 {
		t.Fatalf("seq: want 0, got %d", seq)
	}
	if ts := binary.BigEndian.Uint32(pkt[4:]); ts != 48000 {
		t.Fatalf("timestamp: want 48000, got %d", ts)
	}
	if ssrc := binary.BigEndian.Uint32(pkt[8:]); ssrc != 0xDEADBEEF {
		t.Fatalf("ssrc: want 0xDEADBEEF, got 0x%x", ssrc)
	}
	for i, b := range pcmu {
		if pkt[12+i] != b {
			t.Fatalf("payload byte %d: want %d, got %d", i, b, pkt[12+i])
		}
	}

	// Packet 2 — marker should be off
	pkt2, _ := a.EncodeAudio(provider.AudioFrame{Payload: pcmu})
	if pkt2[1] != 0x00 {
		t.Fatalf("marker: want 0 (off), got 0x%02x", pkt2[1])
	}
	if seq := binary.BigEndian.Uint16(pkt2[2:]); seq != 1 {
		t.Fatalf("seq: want 1, got %d", seq)
	}
	if ts := binary.BigEndian.Uint32(pkt2[4:]); ts != 48160 {
		t.Fatalf("timestamp: want 48160, got %d", ts)
	}
}
