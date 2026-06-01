package telnyx

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/voxlane/voice-gateway/internal/provider"
)

func TestEncodeAudioPreservesRawPCMU(t *testing.T) {
	a := &Adapter{callID: "test-call"}
	pcmu := make([]byte, 160)
	for i := range pcmu {
		pcmu[i] = byte(i & 0xFF)
	}

	encoded, err := a.EncodeAudio(provider.AudioFrame{Payload: pcmu})
	if err != nil {
		t.Fatalf("EncodeAudio: %v", err)
	}
	if len(encoded) != len(pcmu) {
		t.Fatalf("encoded length: want %d, got %d", len(pcmu), len(encoded))
	}
	for i, b := range pcmu {
		if encoded[i] != b {
			t.Fatalf("payload byte %d: want %d, got %d", i, b, encoded[i])
		}
	}
}

func TestEncodeOutboundMediaMessage(t *testing.T) {
	pcmu := []byte{0xff, 0xfe, 0xfd, 0xfc}

	msg, err := encodeOutboundMedia(pcmu)
	if err != nil {
		t.Fatalf("encodeOutboundMedia: %v", err)
	}

	var decoded struct {
		Event string `json:"event"`
		Media struct {
			Payload string `json:"payload"`
		} `json:"media"`
	}
	if err := json.Unmarshal(msg, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if decoded.Event != "media" {
		t.Fatalf("event: want media, got %q", decoded.Event)
	}

	payload, err := base64.StdEncoding.DecodeString(decoded.Media.Payload)
	if err != nil {
		t.Fatalf("payload base64: %v", err)
	}
	if string(payload) != string(pcmu) {
		t.Fatalf("payload mismatch: want %v, got %v", pcmu, payload)
	}
}

func TestParseJSONMediaEvent(t *testing.T) {
	a := &Adapter{callID: "call-123"}
	pcmu := []byte{0xff, 0x7f, 0x00, 0x80}
	raw, _ := json.Marshal(map[string]interface{}{
		"event":     "media",
		"stream_id": "stream-123",
		"media": map[string]string{
			"track":     "inbound",
			"timestamp": "20",
			"payload":   base64.StdEncoding.EncodeToString(pcmu),
		},
	})

	frame, event := a.ParseMediaEvent(raw)
	if event != nil {
		t.Fatalf("event: want nil, got %#v", event)
	}
	if frame == nil {
		t.Fatal("frame: want non-nil")
	}
	if frame.StreamID != "stream-123" {
		t.Fatalf("stream id: want stream-123, got %q", frame.StreamID)
	}
	if frame.Direction != "inbound" {
		t.Fatalf("direction: want inbound, got %q", frame.Direction)
	}
	if string(frame.Payload) != string(pcmu) {
		t.Fatalf("payload mismatch: want %v, got %v", pcmu, frame.Payload)
	}
}

func TestParseJSONMediaEventPreservesTrackDirection(t *testing.T) {
	a := &Adapter{callID: "call-123"}
	pcmu := []byte{0xff, 0x7f, 0x00, 0x80}
	raw, _ := json.Marshal(map[string]interface{}{
		"event":     "media",
		"stream_id": "stream-123",
		"media": map[string]string{
			"track":   "outbound",
			"payload": base64.StdEncoding.EncodeToString(pcmu),
		},
	})

	frame, event := a.ParseMediaEvent(raw)
	if event != nil {
		t.Fatalf("event: want nil, got %#v", event)
	}
	if frame == nil {
		t.Fatal("frame: want non-nil")
	}
	if frame.Direction != "outbound" {
		t.Fatalf("direction: want outbound, got %q", frame.Direction)
	}
}
