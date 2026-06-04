// Cartesia HTTP TTS client. Same shape as the spike publisher's
// cartesia.go — decodes pcm_s16le, pcm_f32le, pcm_mulaw, pcm_alaw.
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
)

// Synthesize calls Cartesia /tts/bytes and returns decoded mono PCM
// (int16) at the requested sample rate. The encoding argument is the
// Cartesia-style "pcm_s16le" / "pcm_f32le" / "pcm_mulaw" / "pcm_alaw".
func Synthesize(apiKey, text, voiceID, modelID, encoding string, sampleRate int) ([]int16, error) {
	body := map[string]interface{}{
		"model_id": modelID,
		"transcript": text,
		"voice": map[string]interface{}{
			"mode": "id",
			"id":   voiceID,
		},
		"output_format": map[string]interface{}{
			"container":   "raw",
			"encoding":    encoding,
			"sample_rate": sampleRate,
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("cartesia: marshal body: %w", err)
	}
	req, err := http.NewRequest("POST", "https://api.cartesia.ai/tts/bytes", bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("cartesia: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("Cartesia-Version", "2024-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cartesia: http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cartesia: http %d: %s", resp.StatusCode, string(bodyBytes))
	}
	audio, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cartesia: read body: %w", err)
	}
	return decodeCartesiaPCM(audio, encoding)
}

func decodeCartesiaPCM(b []byte, encoding string) ([]int16, error) {
	switch encoding {
	case "pcm_s16le", "s16le":
		out := make([]int16, len(b)/2)
		for i := range out {
			out[i] = int16(binary.LittleEndian.Uint16(b[2*i : 2*i+2]))
		}
		return out, nil
	case "pcm_f32le", "f32le":
		out := make([]int16, len(b)/4)
		for i := range out {
			f := math.Float32frombits(binary.LittleEndian.Uint32(b[4*i : 4*i+4]))
			out[i] = int16(f * 32768.0)
		}
		return out, nil
	case "pcm_mulaw", "mulaw":
		out := make([]int16, len(b))
		for i, m := range b {
			out[i] = mulawToLinear(m)
		}
		return out, nil
	case "pcm_alaw", "alaw":
		out := make([]int16, len(b))
		for i, a := range b {
			out[i] = alawToLinear(a)
		}
		return out, nil
	}
	return nil, fmt.Errorf("cartesia: unknown encoding %q", encoding)
}

// mulawToLinear and alawToLinear: standard G.711 decoders. Kept
// short — these are well-known bit-twiddling tables.
func mulawToLinear(mu byte) int16 {
	mu = ^mu
	sign := mu & 0x80
	exponent := (mu >> 4) & 0x07
	mantissa := mu & 0x0F
	sample := int16(((mantissa << 3) + 0x84) << exponent)
	sample -= 0x84
	if sign != 0 {
		return -sample
	}
	return sample
}

func alawToLinear(a byte) int16 {
	a ^= 0x55
	sign := a & 0x80
	exponent := (a >> 4) & 0x07
	mantissa := a & 0x0F
	sample := int16(mantissa << 4 | 0x008)
	sample += 0x100
	sample <<= exponent
	sample -= 0x100
	if sign != 0 {
		return -sample
	}
	return sample
}
