// Minimal Ogg page demuxer for Opus. Adapted from the spike publisher's
// oggdemuxer.go (kept in sync). Returns raw Opus packets (OpusHead,
// OpusTags, then audio frames) suitable for direct handing to a
// LiveKit OpusPayloader.
package main

import (
	"encoding/binary"
	"errors"
	"io"
)

const (
	oggCapturePattern = "OggS"
	oggPageHeaderSize = 27
)

// oggOpusReader pulls Opus packets out of an Ogg Opus stream.
type oggOpusReader struct {
	r        io.Reader
	pageBuf  []byte
	pageOff  int
	packet   []byte
	packetOff int
}

// newOggOpusReaderImpl is the constructor; aliased to newOggOpusReader
// via a method so the public symbol is short.
func newOggOpusReaderImpl(r io.Reader) *oggOpusReader {
	return &oggOpusReader{r: r}
}

// NextOpusPacket returns the next Opus packet from the stream.
// Returns io.EOF on end of stream.
func (d *oggOpusReader) NextOpusPacket() ([]byte, error) {
	for {
		if d.packet != nil && d.packetOff < len(d.packet) {
			out := d.packet[d.packetOff:]
			d.packetOff = len(d.packet)
			d.packet = nil
			return out, nil
		}
		if d.pageBuf == nil {
			hdr, err := d.readPageHeader()
			if err != nil {
				return nil, err
			}
			segCount := int(hdr[26])
			segs := make([]byte, segCount)
			if _, err := io.ReadFull(d.r, segs); err != nil {
				return nil, err
			}
			var pageLen int
			for _, s := range segs {
				pageLen += int(s)
			}
			body := make([]byte, pageLen)
			if _, err := io.ReadFull(d.r, body); err != nil {
				return nil, err
			}
			// Walk the lacing table to split into packets.
			d.pageBuf = body
			d.pageOff = 0
			d.packet = nil
			d.packetOff = 0
			for _, s := range segs {
				size := int(s)
				if d.packet == nil {
					d.packet = d.pageBuf[d.pageOff : d.pageOff+size]
				} else {
					d.packet = append(d.packet, d.pageBuf[d.pageOff:d.pageOff+size]...)
				}
				d.pageOff += size
				if s < 255 {
					// End of packet.
					d.packetOff = 0
					// Loop: NextOpusPacket will read from d.packet.
					break
				}
			}
			if d.packet == nil {
				// Continuation packet spans entire page; keep building.
				continue
			}
		}
	}
}

func (d *oggOpusReader) readPageHeader() ([]byte, error) {
	hdr := make([]byte, oggPageHeaderSize)
	if _, err := io.ReadFull(d.r, hdr); err != nil {
		return nil, err
	}
	if string(hdr[:4]) != oggCapturePattern {
		return nil, errors.New("ogg: missing OggS capture pattern")
	}
	return hdr, nil
}

// OpusHead is the parsed header from the first Ogg packet.
type opusHead struct {
	Version         uint8
	ChannelCount    uint8
	InputSampleRate uint32
	OutputGain      uint16
	MappingFamily   uint8
}

// ParseOpusHead decodes the 19-byte OpusHead packet. The first 8
// bytes are the magic string "OpusHead"; the rest are little-endian
// fields.
func ParseOpusHead(b []byte) (*opusHead, error) {
	if len(b) < 19 {
		return nil, errors.New("opus head: short packet")
	}
	if string(b[:8]) != "OpusHead" {
		return nil, errors.New("opus head: bad magic")
	}
	preSkip := binary.LittleEndian.Uint16(b[10:12])
	_ = preSkip // preSkip is informational; not exposed in the struct.
	return &opusHead{
		Version:         b[8],
		ChannelCount:    b[9],
		InputSampleRate: binary.LittleEndian.Uint32(b[12:16]),
		OutputGain:      binary.LittleEndian.Uint16(b[16:18]),
		MappingFamily:   b[18],
	}, nil
}
