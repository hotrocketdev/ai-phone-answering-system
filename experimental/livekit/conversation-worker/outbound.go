// Outbound provider feeds Opus frames into the LiveKit track's
// StartWrite consumer. It is a tiny channel-backed SampleProvider:
// push() puts samples on the channel, NextSample() reads them. The
// provider is shared between the inbound-reader goroutine (which
// triggers the per-turn reply) and the LiveKit writer goroutine.
//
// Multi-turn mode: the channel is NEVER closed during normal
// operation. pushSilence() inserts 20ms zero-PCM Opus frames as a
// turn delimiter. The channel is only ever closed at worker
// shutdown (never in this spike; the worker exits on signal and
// the process dies).
package main

import (
	"errors"
	"sync"
	"time"

	lksdk "github.com/livekit/server-sdk-go"
	"github.com/pion/webrtc/v3/pkg/media"
)

const (
	outboundProviderSampleRate = 48000
	outboundProviderChannels   = 1
	// silenceOpusFrameBytes is one 20ms silence Opus frame at
	// 48kHz mono encoded at 64kbps. The bytes are arbitrary
	// (LiveKit encodes silence on the wire anyway), so a small
	// zero-filled buffer is fine for a turn delimiter.
	silenceOpusFrameBytes = 6
)

type outboundProvider struct {
	mu sync.Mutex
	ch chan media.Sample
}

func newOutboundProvider() *outboundProvider {
	return &outboundProvider{
		// Buffer up to 100 Opus frames (~2 s) so the LiveKit writer
		// never starves while the ffmpeg->demuxer pipeline is
		// filling the channel.
		ch: make(chan media.Sample, 100),
	}
}

// push enqueues a sample. Non-blocking: drops on overflow.
func (p *outboundProvider) push(s media.Sample) {
	select {
	case p.ch <- s:
	default:
		// Drop on overflow rather than block the inbound reader.
	}
}

// pushSilence inserts a run of 20ms zero-PCM Opus frames into the
// outbound stream. Used as a turn delimiter so the browser hears a
// gap between replies. duration rounds up to a multiple of 20ms.
func (p *outboundProvider) pushSilence(d time.Duration) {
	if d <= 0 {
		return
	}
	n := int((d + 19*time.Millisecond) / 20 * time.Millisecond / time.Millisecond)
	if n < 1 {
		n = 1
	}
	frame := make([]byte, silenceOpusFrameBytes)
	for i := 0; i < n; i++ {
		select {
		case p.ch <- media.Sample{Data: frame, Duration: 20 * time.Millisecond}:
		default:
			return // drop tail of silence on overflow
		}
	}
}

func (p *outboundProvider) NextSample() (media.Sample, error) {
	s, ok := <-p.ch
	if !ok {
		return media.Sample{}, errors.New("EOF")
	}
	return s, nil
}

func (p *outboundProvider) OnBind() error  { return nil }
func (p *outboundProvider) OnUnbind() error { return nil }
func (p *outboundProvider) Close() error    { return nil }

// CurrentAudioLevel satisfies lksdk.AudioSampleProvider when the
// track inspects the provider. Returns a fixed 60 (out of 100) so
// the browser's VU meter shows activity when audio is streaming.
func (p *outboundProvider) CurrentAudioLevel() uint8 { return 60 }

// Compile-time interface assertions.
var (
	_ lksdk.SampleProvider      = (*outboundProvider)(nil)
	_ lksdk.AudioSampleProvider = (*outboundProvider)(nil)
)
