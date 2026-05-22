package audio

import (
	"encoding/base64"
	"sync"
)

// ─── Audio Pipeline ──────────────────────────────────────────────────────

// Pipeline orchestrates the full audio processing chain.
// Inbound:  u-law 8kHz 20ms → PCM16 8kHz → PCM16 24kHz → base64 → OpenAI
// Outbound: base64 → PCM16 24kHz → PCM16 8kHz → u-law 8kHz 20ms → Twilio
type Pipeline struct {
	resampler *Resampler
}

// framePool provides reusable byte buffers for the largest frame size (24kHz PCM16 = 960 bytes).
var framePool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, FrameSizePCM16_24k)
		return &buf
	},
}

// floatPool provides reusable float64 buffers.
var floatPool = sync.Pool{
	New: func() interface{} {
		buf := make([]float64, Samples24k)
		return &buf
	},
}

// NewPipeline creates a new audio pipeline.
func NewPipeline() *Pipeline {
	return &Pipeline{
		resampler: NewResampler(),
	}
}

// ProcessInbound converts a raw u-law frame to a base64-encoded PCM16 24kHz string
// suitable for sending to OpenAI Realtime API.
//
// Input: 160 bytes u-law 8kHz (one 20ms frame from Twilio)
// Output: base64-encoded PCM16 24kHz (960 bytes → 1280 base64 chars)
func (p *Pipeline) ProcessInbound(mulaw []byte) string {
	// Stage 1: u-law → PCM16 8kHz (160 bytes → 320 bytes)
	pcm8k := make([]byte, FrameSizePCM16_8k)
	MulawToPCM16(mulaw, pcm8k)

	// Stage 2: Convert to float64 for resampling
	floats8k := make([]float64, Samples8k)
	PCM16ToFloat64(pcm8k, floats8k)

	// Stage 3: Resample 8kHz → 24kHz (160 samples → 480 samples)
	floats24k := make([]float64, Samples24k)
	p.resampler.Upsample8to24(floats8k, floats24k)

	// Stage 4: Convert back to PCM16 bytes (480 samples → 960 bytes)
	pcm24k := make([]byte, FrameSizePCM16_24k)
	Float64ToPCM16(floats24k, pcm24k)

	// Stage 5: Base64 encode for OpenAI
	return base64.StdEncoding.EncodeToString(pcm24k)
}

// ProcessOutbound converts a base64-encoded PCM16 24kHz string from OpenAI
// to a raw u-law 8kHz byte slice for sending to Twilio.
//
// Input: base64-encoded PCM16 24kHz (1280 chars → 960 bytes → 480 samples)
// Output: 160 bytes u-law 8kHz (one 20ms frame for Twilio)
func (p *Pipeline) ProcessOutbound(b64 string) ([]byte, error) {
	// Stage 1: Base64 decode → PCM16 24kHz bytes
	pcm24k, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}

	// Stage 2: Convert to float64
	floats24k := make([]float64, Samples24k)
	PCM16ToFloat64(pcm24k, floats24k)

	// Stage 3: Resample 24kHz → 8kHz (480 samples → 160 samples)
	floats8k := make([]float64, Samples8k)
	p.resampler.Downsample24to8(floats24k, floats8k)

	// Stage 4: Convert to PCM16 bytes
	pcm8k := make([]byte, FrameSizePCM16_8k)
	Float64ToPCM16(floats8k, pcm8k)

	// Stage 5: PCM16 → u-law
	mulaw := make([]byte, FrameSizeMulaw8k)
	PCM16ToMulaw(pcm8k, mulaw)

	return mulaw, nil
}

// ProcessInboundBytes converts a raw u-law frame to PCM16 24kHz bytes.
// Used when the caller needs raw bytes (e.g., for batching or custom encoding).
func (p *Pipeline) ProcessInboundBytes(mulaw []byte) []byte {
	pcm8k := make([]byte, FrameSizePCM16_8k)
	MulawToPCM16(mulaw, pcm8k)

	floats8k := make([]float64, Samples8k)
	PCM16ToFloat64(pcm8k, floats8k)

	floats24k := make([]float64, Samples24k)
	p.resampler.Upsample8to24(floats8k, floats24k)

	pcm24k := make([]byte, FrameSizePCM16_24k)
	Float64ToPCM16(floats24k, pcm24k)

	return pcm24k
}

// Reset clears the resampler state for reuse.
func (p *Pipeline) Reset() {
	p.resampler.Reset()
}
