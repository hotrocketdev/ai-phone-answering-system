// Inbound Opus frame assembler.
//
// Uses pion's samplebuilder to coalesce RTP packets into complete
// Opus frames. This is the same pattern as the LiveKit server-sdk-go
// filesaver example. The returned sample's Data is a raw Opus frame
// (10-60 bytes typically).
package main

import (
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/pion/webrtc/v3/pkg/media/samplebuilder"
)

type opusSampleBuilder struct {
	sb *samplebuilder.SampleBuilder
}

// newOpusSampleBuilder returns a builder that waits up to maxLate
// packets (in sequence-number space) before forcing a flush. Clock
// rate is 48000 for Opus.
func newOpusSampleBuilder(maxLate uint16, clockRate uint32) *opusSampleBuilder {
	return &opusSampleBuilder{
		sb: samplebuilder.New(maxLate, &codecs.OpusPacket{}, clockRate),
	}
}

// push consumes one RTP packet and returns (sample, true) when a
// complete Opus frame has been assembled, or (_, false) when more
// packets are needed.
func (b *opusSampleBuilder) push(pkt *rtp.Packet) (media.Sample, bool) {
	b.sb.Push(pkt)
	s := b.sb.Pop()
	if s == nil {
		return media.Sample{}, false
	}
	return *s, true
}
