package cartesia

import "testing"

func TestRequestPayloadDefaultsToMulaw8k(t *testing.T) {
	r := New(Config{ModelID: "sonic-3.5", VoiceID: "voice", Language: "en"})
	req := r.requestPayload("hello", "ctx")
	format := req["output_format"].(map[string]interface{})
	if format["encoding"] != "pcm_mulaw" {
		t.Fatalf("encoding = %v, want pcm_mulaw", format["encoding"])
	}
	if format["sample_rate"] != 8000 {
		t.Fatalf("sample_rate = %v, want 8000", format["sample_rate"])
	}
}

func TestRequestPayloadSupportsPCM16_16k(t *testing.T) {
	r := New(Config{
		ModelID:          "sonic-3.5",
		VoiceID:          "voice",
		Language:         "en",
		OutputEncoding:   "pcm_s16le",
		OutputSampleRate: 16000,
	})
	req := r.requestPayload("hello", "ctx")
	format := req["output_format"].(map[string]interface{})
	if format["encoding"] != "pcm_s16le" {
		t.Fatalf("encoding = %v, want pcm_s16le", format["encoding"])
	}
	if format["sample_rate"] != 16000 {
		t.Fatalf("sample_rate = %v, want 16000", format["sample_rate"])
	}
}
