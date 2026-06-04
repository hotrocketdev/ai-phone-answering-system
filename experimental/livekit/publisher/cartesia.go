// Cartesia HTTP TTS client for the LiveKit spike.
//
// Uses the /tts/bytes endpoint with raw PCM output. For the spike
// we typically request 48 kHz mono to match Opus's native clock rate,
// avoiding any resampling step. The encoding and rate are configurable
// for the sonic-3.5 optimisation investigation.
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const cartesiaEndpoint = "https://api.cartesia.ai/tts/bytes"

type cartesiaRequest struct {
	ModelID    string                 `json:"model_id"`
	Transcript string                 `json:"transcript"`
	Voice      map[string]interface{} `json:"voice"`
	OutputFormat map[string]interface{} `json:"output_format"`
}

// Cartesia-supported raw sample rates (from Cartesia /tts/bytes schema).
var cartesiaRawRates = map[int]bool{
	8000:  true,
	16000: true,
	22050: true,
	24000: true,
	44100: true,
	48000: true,
}

// Cartesia-supported raw encodings.
var cartesiaRawEncodings = map[string]bool{
	"pcm_f32le":  true,
	"pcm_s16le":  true,
	"pcm_mulaw":  true,
	"pcm_alaw":   true,
}

// Synthesize calls Cartesia HTTP TTS and returns raw mono PCM samples
// at the requested sample rate. The encoding controls byte layout:
//   - "pcm_s16le" → int16 little-endian (one int16 per sample)
//   - "pcm_f32le" → float32 little-endian (one float32 per sample,
//     returned as int16 scaled to [-32768, 32767])
func Synthesize(apiKey, text, voiceID, model, encoding string, sampleRate int) ([]int16, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("cartesia: empty API key")
	}
	if !cartesiaRawRates[sampleRate] {
		return nil, fmt.Errorf("cartesia: unsupported sample rate %d (allowed: 8000, 16000, 22050, 24000, 44100, 48000)", sampleRate)
	}
	if !cartesiaRawEncodings[encoding] {
		return nil, fmt.Errorf("cartesia: unsupported encoding %q (allowed: pcm_f32le, pcm_s16le, pcm_mulaw, pcm_alaw)", encoding)
	}
	body, _ := json.Marshal(cartesiaRequest{
		ModelID:    model,
		Transcript: text,
		Voice: map[string]interface{}{
			"mode": "id",
			"id":   voiceID,
		},
		OutputFormat: map[string]interface{}{
			"container":   "raw",
			"encoding":    encoding,
			"sample_rate": sampleRate,
		},
	})

	req, err := http.NewRequest(http.MethodPost, cartesiaEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Cartesia-Version", "2024-06-01")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cartesia: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cartesia: status %d: %s", resp.StatusCode, string(b))
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cartesia: read: %w", err)
	}

	switch encoding {
	case "pcm_s16le":
		if len(raw)%2 != 0 {
			return nil, fmt.Errorf("cartesia: odd s16le byte length %d", len(raw))
		}
		samples := make([]int16, len(raw)/2)
		if err := binary.Read(bytes.NewReader(raw), binary.LittleEndian, &samples); err != nil {
			return nil, fmt.Errorf("cartesia: decode s16le: %w", err)
		}
		return samples, nil
	case "pcm_f32le":
		if len(raw)%4 != 0 {
			return nil, fmt.Errorf("cartesia: odd f32le byte length %d", len(raw))
		}
		floats := make([]float32, len(raw)/4)
		if err := binary.Read(bytes.NewReader(raw), binary.LittleEndian, &floats); err != nil {
			return nil, fmt.Errorf("cartesia: decode f32le: %w", err)
		}
		// Convert float32 [-1.0, 1.0] to int16 [-32768, 32767]. Cartesia's
		// f32le output is in [-1, 1] and we rescale to int16 full scale.
		samples := make([]int16, len(floats))
		for i, f := range floats {
			if f > 1.0 {
				f = 1.0
			} else if f < -1.0 {
				f = -1.0
			}
			samples[i] = int16(f * 32767.0)
		}
		return samples, nil
	case "pcm_mulaw":
		// µ-law: 8-bit, decode to int16.
		samples := make([]int16, len(raw))
		for i, b := range raw {
			samples[i] = mulawToInt16(b)
		}
		return samples, nil
	case "pcm_alaw":
		// A-law: 8-bit, decode to int16.
		samples := make([]int16, len(raw))
		for i, b := range raw {
			samples[i] = alawToInt16(b)
		}
		return samples, nil
	}
	return nil, fmt.Errorf("cartesia: unhandled encoding %q", encoding)
}

// mulawToInt16 decodes a single G.711 µ-law byte to int16.
func mulawToInt16(u uint8) int16 {
	u = ^u
	sign := u & 0x80
	expn := (u >> 4) & 0x07
	mant := u & 0x0F
	mag := int16(((int16(mant) << 3) + 0x84) << expn)
	mag -= 0x84
	if sign != 0 {
		return -mag
	}
	return mag
}

// alawToInt16 decodes a single G.711 A-law byte to int16.
func alawToInt16(a uint8) int16 {
	a ^= 0x55
	sign := a & 0x80
	expn := (a >> 4) & 0x07
	mant := a & 0x0F
	mag := int16(int16(mant) << 4)
	if expn != 0 {
		mag += 0x100
		mag <<= expn - 1
	}
	if sign != 0 {
		return mag
	}
	return -mag
}
