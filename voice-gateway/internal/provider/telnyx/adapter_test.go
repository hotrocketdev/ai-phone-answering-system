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
	if frame.Codec != "pcmu" {
		t.Fatalf("codec: want pcmu default, got %q", frame.Codec)
	}
	if string(frame.Payload) != string(pcmu) {
		t.Fatalf("payload mismatch: want %v, got %v", pcmu, frame.Payload)
	}
}

func TestParseStartPCMASelectsAlawCodec(t *testing.T) {
	a := &Adapter{callID: "call-123"}
	start, _ := json.Marshal(map[string]interface{}{
		"event":     "start",
		"stream_id": "stream-123",
		"start": map[string]interface{}{
			"media_format": map[string]interface{}{
				"encoding":    "PCMA",
				"sample_rate": 8000,
				"channels":    1,
			},
		},
	})
	_, event := a.ParseMediaEvent(start)
	if event == nil {
		t.Fatal("event: want start event")
	}

	payload := []byte{0xd5, 0xd5, 0xd5, 0xd5}
	media, _ := json.Marshal(map[string]interface{}{
		"event":     "media",
		"stream_id": "stream-123",
		"media": map[string]string{
			"track":   "inbound",
			"payload": base64.StdEncoding.EncodeToString(payload),
		},
	})
	frame, event := a.ParseMediaEvent(media)
	if event != nil {
		t.Fatalf("event: want nil, got %#v", event)
	}
	if frame == nil {
		t.Fatal("frame: want non-nil")
	}
	if frame.Codec != "pcma" {
		t.Fatalf("codec: want pcma, got %q", frame.Codec)
	}
}

func TestParseStartPCMUSelectsMulawCodec(t *testing.T) {
	a := &Adapter{callID: "call-123"}
	start, _ := json.Marshal(map[string]interface{}{
		"event": "start",
		"start": map[string]interface{}{
			"media_format": map[string]interface{}{
				"encoding":    "PCMU",
				"sample_rate": 8000,
				"channels":    1,
			},
		},
	})
	a.ParseMediaEvent(start)

	payload := []byte{0xff, 0xff, 0xff, 0xff}
	media, _ := json.Marshal(map[string]interface{}{
		"event": "media",
		"media": map[string]string{
			"payload": base64.StdEncoding.EncodeToString(payload),
		},
	})
	frame, _ := a.ParseMediaEvent(media)
	if frame == nil {
		t.Fatal("frame: want non-nil")
	}
	if frame.Codec != "pcmu" {
		t.Fatalf("codec: want pcmu, got %q", frame.Codec)
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
