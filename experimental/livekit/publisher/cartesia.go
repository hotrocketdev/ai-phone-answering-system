// Cartesia HTTP TTS client for the LiveKit spike.
//
// Uses the /tts/bytes endpoint with raw PCM s16le output. For the spike
// we request 48 kHz mono to match Opus's native clock rate, avoiding
// any resampling step.
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
	ModelID      string                 `json:"model_id"`
	Transcript   string                 `json:"transcript"`
	Voice        map[string]interface{} `json:"voice"`
	OutputFormat map[string]interface{} `json:"output_format"`
}

// Synthesize calls Cartesia HTTP TTS and returns PCM s16le mono samples
// at the requested sample rate.
func Synthesize(apiKey, text, voiceID, model string, sampleRate int) ([]int16, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("cartesia: empty API key")
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
			"encoding":    "pcm_s16le",
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
	if len(raw)%2 != 0 {
		return nil, fmt.Errorf("cartesia: odd PCM byte length %d", len(raw))
	}

	samples := make([]int16, len(raw)/2)
	if err := binary.Read(bytes.NewReader(raw), binary.LittleEndian, &samples); err != nil {
		return nil, fmt.Errorf("cartesia: decode pcm: %w", err)
	}
	return samples, nil
}
