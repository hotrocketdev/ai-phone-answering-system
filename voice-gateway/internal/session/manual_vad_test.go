package session

import (
	"math"
	"testing"

	"github.com/voxlane/voice-gateway/internal/audio"
)

func TestAverageAbsMulawSeparatesLineNoiseFromSpeech(t *testing.T) {
	lineNoise := make([]byte, 160)
	for i := range lineNoise {
		lineNoise[i] = 0xD5
	}

	speech := make([]byte, 160)
	for i := range speech {
		sample := int16(math.Sin(2.0*math.Pi*440.0*float64(i)/8000.0) * 12000)
		speech[i] = audio.EncodePCM16ToMulaw(sample)
	}

	if got := averageAbsMulaw(lineNoise); got >= manualVADSpeechThreshold {
		t.Fatalf("line noise average=%d, expected below speech threshold", got)
	}
	if got := averageAbsMulaw(speech); got <= manualVADSpeechThreshold {
		t.Fatalf("speech average=%d, expected above speech threshold", got)
	}
}
