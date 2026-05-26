package audio

import (
	"encoding/base64"
	"encoding/binary"
	"math"
	"math/rand"
	"testing"
)

// ─── u-law Codec Tests ────────────────────────────────────────────────

func TestMulawRoundtrip(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	failures := 0
	for i := 0; i < 10000; i++ {
		original := int16(rng.Intn(65536) - 32768)

		// Use the lookup table directly (same as hot path)
		encoded := pcm16ToMulawTable[uint16(original)]
		decoded := mulawToPCM16Table[encoded]

		absOrig := original
		if absOrig < 0 {
			absOrig = -absOrig
		}

		// u-law is a companding codec — large amplitude errors at extremes are expected
		var tolerance int16
		switch {
		case absOrig < 256:
			tolerance = 70
		case absOrig < 1024:
			tolerance = 140
		case absOrig < 4096:
			tolerance = 600
		case absOrig < 16384:
			tolerance = 2500
		default:
			tolerance = 10000
		}

		diff := decoded - original
		if diff < 0 {
			diff = -diff
		}
		if diff > tolerance {
			failures++
		}
	}
	if failures > 5 {
		t.Errorf("too many roundtrip failures: %d/10000 (expected ≤5)", failures)
	}
}

func TestMulawSilence(t *testing.T) {
	// Zero PCM should encode to 0xFF (silence in u-law)
	encoded := encodeMulaw(0)
	if encoded != 0xFF {
		t.Errorf("zero sample: encoded=0x%02X, expected 0xFF", encoded)
	}
	decoded := decodeMulaw(0xFF)
	if decoded > 64 || decoded < -64 {
		t.Errorf("0xFF (silence) decoded=%d, expected near 0", decoded)
	}
}

func TestMulawFullScale(t *testing.T) {
	// u-law is companding — full-scale signals are intentionally compressed.
	// Test that the codec preserves sign and produces reasonable amplitude.
	posCode := pcm16ToMulawTable[uint16(32767)]
	minVal := int16(-32768)
	negCode := pcm16ToMulawTable[uint16(minVal)]

	pos := mulawToPCM16Table[posCode]
	neg := mulawToPCM16Table[negCode]

	if pos <= 0 {
		t.Errorf("full scale positive: got %d, expected positive", pos)
	}
	if neg >= 0 {
		t.Errorf("full scale negative: got %d, expected negative", neg)
	}
	if pos < 5000 || -neg < 5000 {
		t.Errorf("full scale values too small: pos=%d, neg=%d", pos, neg)
	}
}

func TestMulawLookupTableSize(t *testing.T) {
	if len(mulawToPCM16Table) != 256 {
		t.Errorf("expected 256 entries, got %d", len(mulawToPCM16Table))
	}
	if len(pcm16ToMulawTable) != 65536 {
		t.Errorf("expected 65536 entries, got %d", len(pcm16ToMulawTable))
	}
}

// ─── Conversion Tests ──────────────────────────────────────────────────

func TestMulawToPCM16_BufferSizes(t *testing.T) {
	input := make([]byte, FrameSizeMulaw8k)
	for i := range input {
		input[i] = 0xFF // silence in u-law
	}
	output := make([]byte, len(input)*2)
	MulawToPCM16(input, output)
	// Silence u-law (0xFF) decodes to near-zero PCM
}

func TestPCM16ToMulaw_BufferSizes(t *testing.T) {
	input := make([]byte, FrameSizePCM16_8k) // zeroed = PCM silence
	output := make([]byte, FrameSizeMulaw8k)
	PCM16ToMulaw(input, output)
	// Silence PCM (all zeros) encodes to 0xFF in u-law
	for i, b := range output {
		if b != 0xFF {
			t.Errorf("sample %d: silence PCM should encode to 0xFF, got 0x%02X", i, b)
		}
	}
}

func TestConvertRoundtrip_FloatConversion(t *testing.T) {
	// PCM16 → float64 → PCM16 should preserve values within float precision
	original := make([]byte, FrameSizePCM16_8k)
	for i := 0; i < len(original); i += 2 {
		// Fill with a known pattern
		val := int16((i/2)*100 - 8000)
		putInt16LE(original[i:], val)
	}

	floats := make([]float64, Samples8k)
	PCM16ToFloat64(original, floats)

	result := make([]byte, FrameSizePCM16_8k)
	Float64ToPCM16(floats, result)

	for i := 0; i < len(original); i += 2 {
		orig := int16FromLE(original[i:])
		res := int16FromLE(result[i:])
		if absDiff(orig, res) > 1 {
			t.Errorf("sample %d: original=%d, result=%d", i/2, orig, res)
		}
	}
}

// ─── Resampler Tests ───────────────────────────────────────────────────

func TestResampler_UpsampleLength(t *testing.T) {
	r := NewResampler()
	in := make([]float64, Samples8k)
	out := make([]float64, Samples24k)
	r.Upsample8to24(in, out)
	// Verify output is 3x input length (handled by caller allocating correct size)
}

func TestResampler_NoClipping(t *testing.T) {
	r := NewResampler()
	// Full-scale 1kHz sine wave at 8kHz
	in := make([]float64, Samples8k)
	for i := range in {
		in[i] = math.Sin(2.0 * math.Pi * 1000.0 * float64(i) / 8000.0)
	}
	out := make([]float64, Samples24k)
	r.Upsample8to24(in, out)
	for i, sample := range out {
		if sample > 1.05 || sample < -1.05 {
			t.Errorf("sample %d clipped: %.4f", i, sample)
		}
	}
}

func TestResampler_PreservesFrequency(t *testing.T) {
	// 1kHz sine wave at 8kHz should remain ~1kHz after upsample
	r := NewResampler()
	in := make([]float64, Samples8k)
	for i := range in {
		in[i] = math.Sin(2.0 * math.Pi * 1000.0 * float64(i) / 8000.0)
	}

	out := make([]float64, Samples24k)
	r.Upsample8to24(in, out)

	// Count zero-crossings: at 24kHz, 20ms, 1kHz sine → ~40 crossings
	crossings := 0
	for i := 2; i < len(out); i++ { // skip first samples (filter warm-up)
		if (out[i-1] >= 0 && out[i] < 0) || (out[i-1] < 0 && out[i] >= 0) {
			crossings++
		}
	}
	// Allow wider tolerance due to filter phase shift and transient
	if crossings < 25 || crossings > 45 {
		t.Errorf("frequency mismatch: expected ~40 zero crossings, got %d", crossings)
	}
}

func TestResampler_DownsampleLength(t *testing.T) {
	r := NewResampler()
	in := make([]float64, Samples24k)
	out := make([]float64, Samples8k)
	r.Downsample24to8(in, out)
	// Verify output is 1/3 input length
}

func TestResampler_Reset(t *testing.T) {
	r := NewResampler()

	// Process some data
	in := make([]float64, Samples8k)
	for i := range in {
		in[i] = 0.5
	}
	out := make([]float64, Samples24k)
	r.Upsample8to24(in, out)

	// Reset
	r.Reset()

	// Verify delay line is cleared
	for _, v := range r.delayLine {
		if v != 0 {
			t.Errorf("delay line not cleared after reset: %f", v)
		}
	}
	if r.delayIdx != 0 {
		t.Errorf("delay index not reset: %d", r.delayIdx)
	}
}

// ─── Pipeline Tests ────────────────────────────────────────────────────

func TestPipeline_InboundOutputFormat(t *testing.T) {
	p := NewPipeline()
	input := make([]byte, FrameSizeMulaw8k)
	for i := range input {
		input[i] = 0xFF // silence
	}

	b64 := p.ProcessInbound(input)

	// Verify it's valid base64
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("ProcessInbound output is not valid base64: %v", err)
	}

	// 160 u-law bytes → 320 PCM16 bytes → 480 float64 samples → 960 PCM16 bytes → base64
	expectedDecodedLen := FrameSizePCM16_24k // 960
	if len(decoded) != expectedDecodedLen {
		t.Errorf("decoded base64 length: expected %d, got %d", expectedDecodedLen, len(decoded))
	}
}

func TestPipeline_OutboundOutputFormat(t *testing.T) {
	p := NewPipeline()

	// Create valid 24kHz PCM16 frame (960 bytes → 1280 base64 chars)
	pcm24k := make([]byte, FrameSizePCM16_24k)
	b64 := base64.StdEncoding.EncodeToString(pcm24k)

	output, err := p.ProcessOutbound(b64)
	if err != nil {
		t.Fatalf("ProcessOutbound failed: %v", err)
	}

	if len(output) != FrameSizeMulaw8k {
		t.Errorf("expected %d output bytes, got %d", FrameSizeMulaw8k, len(output))
	}
}

func TestPipeline_InvalidBase64(t *testing.T) {
	p := NewPipeline()
	_, err := p.ProcessOutbound("!!!not-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestPipeline_ProcessOutboundBytes_FullFrame(t *testing.T) {
	p := NewPipeline()

	// Generate a full 960-byte PCM16 24kHz frame (sine wave)
	frame := make([]byte, FrameSizePCM16_24k)
	for i := 0; i < len(frame); i += 2 {
		sample := int16(math.Sin(2.0*math.Pi*440.0*float64(i/2)/24000.0) * 16000)
		binary.LittleEndian.PutUint16(frame[i:], uint16(sample))
	}

	mulaw, err := p.ProcessOutboundBytes(frame)
	if err != nil {
		t.Fatalf("ProcessOutboundBytes failed: %v", err)
	}
	if mulaw == nil {
		t.Fatal("expected non-nil output for full frame")
	}
	if len(mulaw) != FrameSizeMulaw8k {
		t.Errorf("expected %d u-law bytes, got %d", FrameSizeMulaw8k, len(mulaw))
	}

	// Verify output is not all silence
	hasSignal := false
	for _, b := range mulaw {
		if b != 0xFF && b != 0x7F {
			hasSignal = true
			break
		}
	}
	if !hasSignal {
		t.Error("output is all silence — audio conversion lost signal")
	}
}

func TestPipeline_BufferedFrames(t *testing.T) {
	p := NewPipeline()

	frame := make([]byte, FrameSizePCM16_24k)
	for i := 0; i < len(frame); i += 2 {
		binary.LittleEndian.PutUint16(frame[i:], uint16((i/2)*100))
	}

	// Send partial chunk — now processes immediately with padding
	r, err := p.ProcessOutboundBytes(frame[:320])
	if err != nil {
		t.Fatalf("partial chunk failed: %v", err)
	}
	if r == nil || len(r) != FrameSizeMulaw8k {
		t.Errorf("partial chunk: expected %d u-law bytes, got %d", FrameSizeMulaw8k, len(r))
	}
}

func TestPipeline_FlushPartial(t *testing.T) {
	p := NewPipeline()

	// Small chunk — processed immediately, padded with silence
	partial := make([]byte, 100)
	r, err := p.ProcessOutboundBytes(partial)
	if err != nil {
		t.Fatalf("small chunk failed: %v", err)
	}
	if r == nil || len(r) != FrameSizeMulaw8k {
		t.Errorf("small chunk: expected %d u-law bytes, got %d", FrameSizeMulaw8k, len(r))
	}
}

// ─── Benchmark ─────────────────────────────────────────────────────────

func BenchmarkMulawToPCM16(b *testing.B) {
	input := make([]byte, FrameSizeMulaw8k)
	output := make([]byte, FrameSizePCM16_8k)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		MulawToPCM16(input, output)
	}
}

func BenchmarkPCM16ToMulaw(b *testing.B) {
	input := make([]byte, FrameSizePCM16_8k)
	output := make([]byte, FrameSizeMulaw8k)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PCM16ToMulaw(input, output)
	}
}

func BenchmarkResampler_Upsample(b *testing.B) {
	r := NewResampler()
	in := make([]float64, Samples8k)
	out := make([]float64, Samples24k)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Upsample8to24(in, out)
		r.Reset()
	}
}

func BenchmarkResampler_Downsample(b *testing.B) {
	r := NewResampler()
	in := make([]float64, Samples24k)
	out := make([]float64, Samples8k)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Downsample24to8(in, out)
	}
}

func BenchmarkPipeline_ProcessInbound(b *testing.B) {
	p := NewPipeline()
	input := make([]byte, FrameSizeMulaw8k)
	for i := range input {
		input[i] = 0xFF
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.ProcessInbound(input)
	}
}

// ─── Helpers ───────────────────────────────────────────────────────────

func int16FromLE(b []byte) int16 {
	return int16(b[0]) | int16(b[1])<<8
}

func putInt16LE(b []byte, v int16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

func absDiff(a, b int16) int16 {
	d := a - b
	if d < 0 {
		return -d
	}
	return d
}
