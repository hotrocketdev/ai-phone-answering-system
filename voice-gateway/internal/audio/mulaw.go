// Package audio provides u-law codec and PCM16 resampling for the realtime voice pipeline.
// All hot-path functions are designed for zero allocations after initialization.
package audio

import (
	"encoding/binary"
	"math"
)

// ─── u-law Lookup Tables ────────────────────────────────────────────────

var (
	mulawToPCM16Table [256]int16
	pcm16ToMulawTable [65536]byte // full int16 range → u-law
)

func init() {
	// Build forward table: u-law → PCM16
	for i := 0; i < 256; i++ {
		mulawToPCM16Table[i] = decodeMulaw(byte(i))
	}

	// Build reverse table: PCM16 → u-law (exhaustive search for closest match)
	for i := int32(math.MinInt16); i <= math.MaxInt16; i++ {
		bestCode := byte(0)
		bestDiff := int32(math.MaxInt32)
		for code := 0; code < 256; code++ {
			decoded := int32(mulawToPCM16Table[code])
			diff := i - decoded
			if diff < 0 {
				diff = -diff
			}
			if diff < bestDiff {
				bestDiff = diff
				bestCode = byte(code)
			}
		}
		pcm16ToMulawTable[uint16(i)] = bestCode
	}

	// Fix silence: ensure PCM 0 maps to standard 0xFF silence code
	pcm16ToMulawTable[uint16(0)] = 0xFF
}

// decodeMulaw converts a single u-law byte to a PCM16 sample.
// Standard G.711 u-law to 16-bit linear PCM.
func decodeMulaw(mulaw byte) int16 {
	// Invert bits per G.711 spec
	mulaw = ^mulaw

	// Extract sign (bit 7), segment (bits 4-6), quantization (bits 0-3)
	sign := int16(mulaw&0x80) >> 7
	segment := int16(mulaw&0x70) >> 4
	quant := int16(mulaw & 0x0f)

	// Decode to 14-bit linear: value14 = ((2Q + 33) << S) - 33
	// Then scale to 16-bit: value16 = value14 << 2 = value14 * 4
	value14 := ((quant << 1) + 33) << segment
	value14 -= 33
	value := value14 << 2 // scale to 16-bit

	if sign != 0 {
		value = -value
	}
	return value
}

// encodeMulaw converts a single PCM16 sample to a u-law byte.
// Uses the precomputed lookup table for speed and correctness.
func encodeMulaw(sample int16) byte {
	return pcm16ToMulawTable[uint16(sample)]
}

// ─── Conversion Functions (Hot Path) ────────────────────────────────────

// MulawToPCM16 converts a u-law byte slice to PCM16 little-endian samples.
// pcm16 must be at least len(mulaw)*2 bytes.
// Zero allocations.
func MulawToPCM16(mulaw []byte, pcm16 []byte) {
	for i := 0; i < len(mulaw); i++ {
		sample := mulawToPCM16Table[mulaw[i]]
		binary.LittleEndian.PutUint16(pcm16[i*2:], uint16(sample))
	}
}

// PCM16ToMulaw converts PCM16 little-endian samples to u-law bytes.
// mulaw must be at least len(pcm16)/2 bytes.
// Zero allocations.
func PCM16ToMulaw(pcm16 []byte, mulaw []byte) {
	for i := 0; i < len(pcm16); i += 2 {
		sample := int16(binary.LittleEndian.Uint16(pcm16[i:]))
		mulaw[i/2] = pcm16ToMulawTable[uint16(sample)]
	}
}

// PCM16ToFloat64 converts PCM16 samples to float64 in range [-1.0, 1.0].
// Zero allocations.
func PCM16ToFloat64(pcm16 []byte, floats []float64) {
	for i := 0; i < len(floats); i++ {
		sample := int16(binary.LittleEndian.Uint16(pcm16[i*2:]))
		floats[i] = float64(sample) / 32768.0
	}
}

// Float64ToPCM16 converts float64 samples in range [-1.0, 1.0] to PCM16 bytes.
// Clips out-of-range values. Zero allocations.
func Float64ToPCM16(floats []float64, pcm16 []byte) {
	for i, f := range floats {
		// Clip
		if f > 1.0 {
			f = 1.0
		} else if f < -1.0 {
			f = -1.0
		}
		sample := int16(math.Round(f * 32767.0))
		binary.LittleEndian.PutUint16(pcm16[i*2:], uint16(sample))
	}
}

// ─── Frame Constants ─────────────────────────────────────────────────────

const (
	// Frame size in bytes at 20ms
	FrameSizeMulaw8k  = 160 // 8000 Hz * 0.020s * 1 byte
	FrameSizePCM16_8k = 320 // 8000 Hz * 0.020s * 2 bytes
	FrameSizePCM16_24k = 960 // 24000 Hz * 0.020s * 2 bytes

	// Sample counts at 20ms
	Samples8k  = 160 // 8000 * 0.020
	Samples24k = 480 // 24000 * 0.020
)
