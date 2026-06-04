// Sine-wave test-tone generator. Used by the spike when REPLY_MODE is
// "tone_on_first_frame" or when CARTESIA_API_KEY is empty.
package main

import (
	"math"
)

// generateTone returns mono PCM at sampleRate Hz, of the given
// duration in seconds, at the given frequency in Hz, scaled to
// amplitude×32767.
func generateTone(sampleRate, channels int, freq, seconds float64) []int16 {
	if channels < 1 {
		channels = 1
	}
	n := int(float64(sampleRate) * seconds)
	out := make([]int16, n*channels)
	omega := 2 * math.Pi * freq / float64(sampleRate)
	amp := 0.3 * 32767.0
	for i := 0; i < n; i++ {
		s := int16(amp * math.Sin(omega*float64(i)))
		for c := 0; c < channels; c++ {
			out[i*channels+c] = s
		}
	}
	return out
}
