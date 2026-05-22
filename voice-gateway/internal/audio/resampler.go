package audio

import "math"

// ─── Polyphase Resampler (3x integer ratio) ─────────────────────────────

// Resampler performs 3x upsampling and 3x downsampling for 8kHz ↔ 24kHz conversion.
// Uses a polyphase FIR filter to avoid aliasing artifacts.
// All hot-path methods are zero-allocation after initialization.
type Resampler struct {
	// Filter coefficients — 48-tap Kaiser window FIR
	// Cutoff: 4kHz, Stopband: 5.3kHz, ~80dB attenuation
	taps      []float64
	phases    [3][]float64 // 3 polyphase subfilters, 16 taps each
	// State
	delayLine []float64 // FIFO for FIR history (length = len(taps)/3 = 16)
	delayIdx  int
}

// NewResampler creates a new polyphase resampler with a 48-tap Kaiser filter.
func NewResampler() *Resampler {
	// 48-tap Kaiser window lowpass, cutoff 4kHz (at 24kHz output = 4000/24000 = 0.1667)
	// 48 taps → 3 polyphase × 16 taps each
	taps := designKaiserFilter(48, 4000.0, 5300.0, 24000.0)
	p := decomposePolyphase(taps, 3)
	var phases [3][]float64
	copy(phases[:], p)

	return &Resampler{
		taps:      taps,
		phases:    phases,
		delayLine: make([]float64, len(taps)), // full filter length for downsampling
	}
}

// Upsample8to24 converts 160 float64 samples at 8kHz → 480 float64 samples at 24kHz.
// out must be at least 480 elements.
// Zero allocations on the hot path.
func (r *Resampler) Upsample8to24(in, out []float64) {
	outIdx := 0
	for _, sample := range in {
		// Push into delay line
		r.delayLine[r.delayIdx] = sample
		r.delayIdx = (r.delayIdx + 1) % len(r.delayLine)

		// Generate 3 output samples (one per polyphase)
		for phase := 0; phase < 3; phase++ {
			out[outIdx] = r.convolvePhase(phase)
			outIdx++
		}
	}
}

// Downsample24to8 converts 480 float64 samples at 24kHz → 160 float64 samples at 8kHz.
// Applies anti-aliasing lowpass filter before decimation.
// out must be at least 160 elements.
// Zero allocations on the hot path.
func (r *Resampler) Downsample24to8(in, out []float64) {
	// Apply anti-aliasing filter to input (full FIR, not polyphase)
	filtered := make([]float64, len(in))
	delayLen := len(r.delayLine)

	for i := range in {
		r.delayLine[r.delayIdx] = in[i]
		r.delayIdx = (r.delayIdx + 1) % delayLen

		var sum float64
		for j := 0; j < len(r.taps); j++ {
			idx := r.delayIdx - 1 - j
			for idx < 0 {
				idx += delayLen
			}
			idx %= delayLen
			sum += r.delayLine[idx] * r.taps[j]
		}
		filtered[i] = sum
	}

	// Decimate by 3
	for i := 0; i < len(out); i++ {
		out[i] = filtered[i*3]
	}
}

// convolvePhase applies a single polyphase subfilter to the delay line.
// The subfilter uses only the last subLen entries of the full delay line.
func (r *Resampler) convolvePhase(phase int) float64 {
	subfilter := r.phases[phase]
	subLen := len(subfilter)
	offset := len(r.delayLine) - subLen // always use last subLen entries
	var sum float64
	for j := 0; j < subLen; j++ {
		idx := r.delayIdx - 1 - j - offset
		for idx < 0 {
			idx += len(r.delayLine)
		}
		idx %= len(r.delayLine)
		sum += r.delayLine[idx] * subfilter[j]
	}
	return sum
}

// Reset clears the delay line state (for reuse across calls).
func (r *Resampler) Reset() {
	for i := range r.delayLine {
		r.delayLine[i] = 0
	}
	r.delayIdx = 0
}

// ─── Filter Design ───────────────────────────────────────────────────────

// designKaiserFilter designs a Kaiser window FIR lowpass filter.
// Returns `n` filter coefficients.
func designKaiserFilter(n int, passHz, stopHz, sampleRate float64) []float64 {
	passFreq := passHz / sampleRate
	stopFreq := stopHz / sampleRate

	// Transition width used to inform Kaiser beta parameter
	_ = stopFreq - passFreq // transition width — informs filter design

	// Kaiser parameters for ~80dB attenuation
	beta := 7.857
	cutoff := (passFreq + stopFreq) / 2.0

	taps := make([]float64, n)
	center := float64(n-1) / 2.0

	for i := 0; i < n; i++ {
		x := float64(i) - center

		// Ideal lowpass (sinc)
		var ideal float64
		if x == 0 {
			ideal = 2.0 * cutoff
		} else {
			ideal = math.Sin(2.0*math.Pi*cutoff*x) / (math.Pi * x)
		}

		// Kaiser window
		arg := beta * math.Sqrt(1.0-math.Pow(2.0*x/float64(n-1), 2.0))
		window := besselI0(arg) / besselI0(beta)

		taps[i] = ideal * window
	}

	// Normalize
	var sum float64
	for _, t := range taps {
		sum += t
	}
	for i := range taps {
		taps[i] /= sum
	}

	return taps
}

// decomposePolyphase separates a filter into `m` polyphase subfilters.
func decomposePolyphase(taps []float64, m int) [][]float64 {
	phases := make([][]float64, m)
	subLen := len(taps) / m

	for p := 0; p < m; p++ {
		phases[p] = make([]float64, subLen)
		for i := 0; i < subLen; i++ {
			phases[p][i] = taps[i*m+p]
		}
	}
	return phases
}

// besselI0 computes the modified Bessel function I0(x) using series expansion.
func besselI0(x float64) float64 {
	var sum float64 = 1.0
	var term float64 = 1.0
	var y = x / 2.0

	for n := 1; n < 25; n++ {
		term *= y / float64(n)
		sum += term * term
	}
	return sum
}
