package audio

import (
	"encoding/base64"
	"fmt"
	"sync"
)

// ─── Audio Pipeline ──────────────────────────────────────────────────────

// Pipeline orchestrates the full audio processing chain.
// Inbound:  G.711 8kHz or G.722 16kHz → PCM16 24kHz → base64 → OpenAI
// Outbound: base64 → PCM16 24kHz → PCM16 8kHz → u-law 8kHz 20ms → Twilio
type Pipeline struct {
	resampler *Resampler
	g722Dec   *G722Decoder
	outBuf    []byte // accumulates outbound PCM16 24kHz chunks until full frame
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
		g722Dec:   NewG722Decoder(),
	}
}

// ProcessInbound converts a raw u-law frame to a base64-encoded PCM16 24kHz string
// suitable for sending to OpenAI Realtime API.
//
// Input: 160 bytes u-law 8kHz (one 20ms frame from Twilio)
// Output: base64-encoded PCM16 24kHz (960 bytes → 1280 base64 chars)
func (p *Pipeline) ProcessInbound(mulaw []byte) string {
	pcm24k := p.ProcessInboundBytes(mulaw)
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
	return p.ResamplePCM16_8kTo24k(pcm8k)
}

// ProcessInboundBytesForCodec converts inbound provider audio to PCM16 24kHz bytes.
func (p *Pipeline) ProcessInboundBytesForCodec(codec string, payload []byte) ([]byte, error) {
	if normalizeInboundCodec(codec) == "g722" {
		pcm16k, err := p.G722ToPCM16(payload)
		if err != nil {
			return nil, err
		}
		return p.ResamplePCM16_16kTo24k(pcm16k), nil
	}
	pcm8k, err := G711ToPCM16(codec, payload)
	if err != nil {
		return nil, err
	}
	return p.ResamplePCM16_8kTo24k(pcm8k), nil
}

// G722ToPCM16 decodes raw G.722 payload bytes to PCM16 16kHz bytes.
func (p *Pipeline) G722ToPCM16(payload []byte) ([]byte, error) {
	if p.g722Dec == nil {
		p.g722Dec = NewG722Decoder()
	}
	return p.g722Dec.Decode(payload)
}

// G711ToPCM16 decodes PCMU/u-law or PCMA/A-law payload bytes to PCM16 8kHz bytes.
func G711ToPCM16(codec string, payload []byte) ([]byte, error) {
	pcm8k := make([]byte, len(payload)*2)
	switch normalizeG711Codec(codec) {
	case "pcma":
		AlawToPCM16(payload, pcm8k)
	case "pcmu":
		MulawToPCM16(payload, pcm8k)
	default:
		return nil, fmt.Errorf("unsupported inbound G.711 codec %q", codec)
	}
	return pcm8k, nil
}

// ResamplePCM16_8kTo24k upsamples PCM16 8kHz bytes to PCM16 24kHz bytes.
func (p *Pipeline) ResamplePCM16_8kTo24k(pcm8k []byte) []byte {
	samples8k := len(pcm8k) / 2
	floats8k := make([]float64, samples8k)
	PCM16ToFloat64(pcm8k, floats8k)

	floats24k := make([]float64, samples8k*3)
	p.resampler.Upsample8to24(floats8k, floats24k)
	for i := range floats24k {
		floats24k[i] *= 3.0
	}

	pcm24k := make([]byte, len(floats24k)*2)
	Float64ToPCM16(floats24k, pcm24k)
	return pcm24k
}

// ResamplePCM16_16kTo24k upsamples PCM16 16kHz bytes to PCM16 24kHz bytes.
func (p *Pipeline) ResamplePCM16_16kTo24k(pcm16k []byte) []byte {
	inSamples := len(pcm16k) / 2
	if inSamples == 0 {
		return nil
	}
	outSamples := inSamples * 3 / 2
	pcm24k := make([]byte, outSamples*2)
	for outIdx := 0; outIdx < outSamples; outIdx++ {
		srcNum := outIdx * 2
		srcIdx := srcNum / 3
		frac := srcNum % 3
		a := int16FromPCM16LE(pcm16k[srcIdx*2:])
		if frac == 0 || srcIdx+1 >= inSamples {
			putPCM16LE(pcm24k[outIdx*2:], a)
			continue
		}
		b := int16FromPCM16LE(pcm16k[(srcIdx+1)*2:])
		interp := (int(a)*(3-frac) + int(b)*frac) / 3
		putPCM16LE(pcm24k[outIdx*2:], int16(interp))
	}
	return pcm24k
}

func normalizeInboundCodec(codec string) string {
	switch codec {
	case "G722", "g722", "G.722", "g.722":
		return "g722"
	default:
		return normalizeG711Codec(codec)
	}
}

func normalizeG711Codec(codec string) string {
	switch codec {
	case "PCMA", "pcma", "alaw", "a-law", "g711a", "G711A":
		return "pcma"
	case "PCMU", "pcmu", "ulaw", "u-law", "mulaw", "mu-law", "g711u", "G711U":
		return "pcmu"
	default:
		return codec
	}
}

func int16FromPCM16LE(b []byte) int16 {
	return int16(b[0]) | int16(b[1])<<8
}

func putPCM16LE(b []byte, v int16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

// ProcessOutboundBytes converts raw PCM16 24kHz bytes from OpenAI
// to u-law 8kHz bytes for Twilio. Buffers chunks and processes full 960-byte frames.
// Returns all completed frames. On audio.done, call FlushOutbound for remainder.
func (p *Pipeline) ProcessOutboundBytes(pcm24k []byte) [][]byte {
	p.outBuf = append(p.outBuf, pcm24k...)

	var frames [][]byte
	for len(p.outBuf) >= FrameSizePCM16_24k {
		frame := p.outBuf[:FrameSizePCM16_24k]
		p.outBuf = p.outBuf[FrameSizePCM16_24k:]

		floats24k := make([]float64, Samples24k)
		PCM16ToFloat64(frame, floats24k)

		floats8k := make([]float64, Samples8k)
		p.resampler.Downsample24to8(floats24k, floats8k)

		pcm8k := make([]byte, FrameSizePCM16_8k)
		Float64ToPCM16(floats8k, pcm8k)

		mulaw := make([]byte, FrameSizeMulaw8k)
		PCM16ToMulaw(pcm8k, mulaw)

		frames = append(frames, mulaw)
	}
	return frames
}

// Reset clears the resampler state for reuse.
func (p *Pipeline) Reset() {
	p.resampler.Reset()
	p.outBuf = nil
}

// FlushOutbound processes any remaining buffered outbound audio.
// Pads the last partial frame with silence to a full 20ms frame.
func (p *Pipeline) FlushOutbound() []byte {
	if len(p.outBuf) == 0 {
		return nil
	}
	// Pad to full frame with silence
	for len(p.outBuf) < FrameSizePCM16_24k {
		p.outBuf = append(p.outBuf, 0)
	}
	frame := p.outBuf[:FrameSizePCM16_24k]
	p.outBuf = nil

	floats24k := make([]float64, Samples24k)
	PCM16ToFloat64(frame, floats24k)
	floats8k := make([]float64, Samples8k)
	p.resampler.Downsample24to8(floats24k, floats8k)
	pcm8k := make([]byte, FrameSizePCM16_8k)
	Float64ToPCM16(floats8k, pcm8k)
	mulaw := make([]byte, FrameSizeMulaw8k)
	PCM16ToMulaw(pcm8k, mulaw)
	return mulaw
}
