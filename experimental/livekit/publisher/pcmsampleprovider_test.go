package main

import "testing"

// TestLinearToMulawDeterministic verifies the encoder is deterministic.
// µ-law is a well-defined algorithm so given a sample, the output is
// always the same byte.
func TestLinearToMulawDeterministic(t *testing.T) {
	cases := []int16{0, 100, -100, 1000, -1000, 8000, -8000, 32767, -32768, 16384, -16384}
	for _, s := range cases {
		a := linearToMulaw(s)
		b := linearToMulaw(s)
		if a != b {
			t.Errorf("linearToMulaw(%d) not deterministic: %#x != %#x", s, a, b)
		}
	}
}

// TestLinearToMulawSymmetry checks that positive and negative samples
// of equal magnitude differ only in the sign bit. This is a property
// of symmetric G.711 µ-law encoding.
func TestLinearToMulawSymmetry(t *testing.T) {
	for _, mag := range []int16{100, 1000, 8000, 16000, 32767} {
		pos := linearToMulaw(mag)
		neg := linearToMulaw(-mag)
		// Sign bit is the high bit. Positive and negative samples of
		// equal magnitude should differ only in the sign bit.
		diff := pos ^ neg
		if diff != 0x80 {
			t.Errorf("pos=%#x neg=%#x (mag=%d): differ by %#x, want 0x80", pos, neg, mag, diff)
		}
	}
}

func TestPCMSampleProviderFrameSize(t *testing.T) {
	// 8000 Hz, 20ms = 160 samples per PCMU frame.
	pcm := make([]int16, 8000) // 1 second of silence
	p, err := NewPCMSampleProvider(pcm, 8000)
	if err != nil {
		t.Fatalf("NewPCMSampleProvider: %v", err)
	}
	if p.frameSize != 160 {
		t.Errorf("frameSize = %d, want 160", p.frameSize)
	}
}

func TestPCMSampleProviderRejectsNon8k(t *testing.T) {
	_, err := NewPCMSampleProvider([]int16{0}, 48000)
	if err == nil {
		t.Error("expected error for 48000 Hz, got nil")
	}
}

func TestPCMSampleProviderEmitsEOF(t *testing.T) {
	// 1 second of silence
	pcm := make([]int16, 8000)
	p, _ := NewPCMSampleProvider(pcm, 8000)
	frames := 0
	for {
		_, err := p.NextSample()
		if err != nil {
			break
		}
		frames++
	}
	// 8000 / 160 = 50 frames per second
	if frames != 50 {
		t.Errorf("frames = %d, want 50", frames)
	}
}
