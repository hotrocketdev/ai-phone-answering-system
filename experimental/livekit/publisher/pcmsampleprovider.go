// PCMSampleProvider: a LiveKit SampleProvider that reads 16-bit mono
// PCM from a buffer, encodes each 20ms frame to G.711 µ-law (PCMU),
// and yields PCMU frames for the track's write worker.
//
// PCMU is used as a spike-friendly intermediate format because:
//   1. The encoding is 10 lines of math, no CGO or external library.
//   2. LiveKit and the livekit-client browser SDK support PCMU natively.
//   3. It proves the entire one-way audio path (Cartesia -> encoder
//      -> LiveKit room -> browser playback) end-to-end.
//
// Opus (HD) encoding is a follow-up that requires libopus CGO bindings
// to be installed on the build host. See experimental/livekit/results/
// README.md for the detailed follow-up plan.
package main

import (
	"fmt"
	"io"
	"time"

	"github.com/pion/webrtc/v3/pkg/media"
)

// PCMSampleProvider implements lksdk.SampleProvider for PCMU output.
type PCMSampleProvider struct {
	pcm        []int16
	frameSize  int // PCM samples per PCMU frame (160 at 8 kHz / 20 ms)
	sampleRate int // PCM sample rate (must be 8000 for PCMU)
	pos        int // read position in PCM buffer
	audLevel   uint8
	// firstFrameLogged is set on the first NextSample call so we can
	// log a "first_audio_byte" latency milestone.
	firstFrameLogged bool
}

// NewPCMSampleProvider encodes mono PCM at 8 kHz into 20ms PCMU frames.
func NewPCMSampleProvider(pcm []int16, sampleRate int) (*PCMSampleProvider, error) {
	if sampleRate != 8000 {
		return nil, fmt.Errorf("pcmu: sample rate must be 8000, got %d", sampleRate)
	}
	frameSize := sampleRate * 20 / 1000
	return &PCMSampleProvider{
		pcm:        pcm,
		frameSize:  frameSize,
		sampleRate: sampleRate,
		audLevel:   60,
	}, nil
}

func (p *PCMSampleProvider) NextSample() (media.Sample, error) {
	if p.pos >= len(p.pcm) {
		return media.Sample{}, io.EOF
	}
	if !p.firstFrameLogged {
		p.firstFrameLogged = true
		latencyLog("first_audio_byte (pcmu)")
	}
	end := p.pos + p.frameSize
	if end > len(p.pcm) {
		end = len(p.pcm)
	}
	frame := p.pcm[p.pos:end]
	p.pos = end
	if len(frame) < p.frameSize {
		padded := make([]int16, p.frameSize)
		copy(padded, frame)
		frame = padded
	}
	out := make([]byte, p.frameSize)
	for i, s := range frame {
		out[i] = linearToMulaw(s)
	}
	return media.Sample{
		Data:     out,
		Duration: 20 * time.Millisecond,
	}, nil
}

func (p *PCMSampleProvider) OnBind() error  { return nil }
func (p *PCMSampleProvider) OnUnbind() error { return nil }
func (p *PCMSampleProvider) Close() error    { return nil }

// CurrentAudioLevel satisfies lksdk.AudioSampleProvider when the track
// inspects the provider.
func (p *PCMSampleProvider) CurrentAudioLevel() uint8 { return p.audLevel }

// linearToMulaw: standard G.711 µ-law encoder (ITU-T G.711, 1972).
// 16-bit linear PCM -> 8-bit µ-law. Returns the bit-inverted byte,
// which is the on-the-wire PCMU format.
func linearToMulaw(pcm int16) byte {
	const BIAS = 0x84
	const MAX = 32635

	sign := 0
	if pcm < 0 {
		pcm = -pcm
		sign = 0x80
	}
	if pcm > MAX {
		pcm = MAX
	}
	pcm = pcm + BIAS
	exp := 7
	for m := int(pcm) >> 8; m > 0; m >>= 1 {
		exp--
		if exp < 0 {
			break
		}
	}
	mantissa := (int(pcm) >> (exp + 3)) & 0x0F
	return byte(^(sign | (exp << 4) | mantissa))
}
