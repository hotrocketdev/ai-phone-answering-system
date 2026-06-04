// Outbound provider feeds Opus frames into the LiveKit track's
// StartWrite consumer. It is a tiny channel-backed SampleProvider:
// push() puts samples on the channel, NextSample() reads them. The
// provider is shared between the inbound-reader goroutine (which
// triggers the reply) and the LiveKit writer goroutine.
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
)

type outboundProvider struct {
	mu     sync.Mutex
	ch     chan media.Sample
	closed bool
}

func newOutboundProvider() *outboundProvider {
	return &outboundProvider{
		// Buffer up to 100 Opus frames (~2 s) so the LiveKit writer
		// never starves while the ffmpeg→demuxer pipeline is filling
		// the channel.
		ch: make(chan media.Sample, 100),
	}
}

// push enqueues a sample. If the provider is closed, push is a no-op.
func (p *outboundProvider) push(s media.Sample) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	select {
	case p.ch <- s:
	default:
		// Drop on overflow rather than block the inbound reader.
		// For the spike, the ffmpeg→demuxer is faster than the
		// LiveKit writer, so this should not fire.
	}
}

// close signals end-of-stream. LiveKit will fire the playback-complete
// callback once the channel is drained.
func (p *outboundProvider) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	close(p.ch)
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
// track inspects the provider. Returns a fixed 60 (out of 100) so the
// browser's VU meter shows activity.
func (p *outboundProvider) CurrentAudioLevel() uint8 { return 60 }

// Compile-time interface assertions.
var (
	_ lksdk.SampleProvider      = (*outboundProvider)(nil)
	_ lksdk.AudioSampleProvider = (*outboundProvider)(nil)
)

// silenceSilenceProvider is a no-op SampleProvider that emits silence
// at 48 kHz mono 20 ms cadence. Currently unused; left here as a
// template if a future spike wants a "publish a silent track" mode.
type silenceSilenceProvider struct{}

func (silenceSilenceProvider) NextSample() (media.Sample, error) {
	return media.Sample{
		Data:     make([]byte, 1920), // 960 samples × 2 bytes (s16le) at 48 kHz = 20 ms
		Duration: 20 * time.Millisecond,
	}, nil
}
func (silenceSilenceProvider) OnBind() error             { return nil }
func (silenceSilenceProvider) OnUnbind() error           { return nil }
func (silenceSilenceProvider) Close() error              { return nil }
func (silenceSilenceProvider) CurrentAudioLevel() uint8  { return 0 }

var (
	_ lksdk.SampleProvider      = silenceSilenceProvider{}
	_ lksdk.AudioSampleProvider = silenceSilenceProvider{}
)
