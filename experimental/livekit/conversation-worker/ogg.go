// Minimal Ogg Opus demuxer. Identical to the spike publisher's
// oggdemuxer.go (kept in sync). Returns one raw Opus packet per
// NextOpusPacket() call; first two packets are OpusHead and OpusTags.
package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const oggCapture = "OggS"

// oggOpusReader reads Ogg Opus pages and yields raw Opus packets.
type oggOpusReader struct {
	r io.Reader

	// current page being processed
	pageBuf    []byte
	segOffsets []int
	segLens    []int
	segIdx     int
	eos        bool
}

// newOggOpusReaderImpl is the constructor; aliased to newOggOpusReader
// via a method so the public symbol is short.
func newOggOpusReaderImpl(r io.Reader) *oggOpusReader {
	return &oggOpusReader{r: r}
}

// NextOpusPacket returns the next raw Opus packet, or io.EOF at end of
// stream. Each packet is one Opus frame (~20ms at default settings).
func (o *oggOpusReader) NextOpusPacket() ([]byte, error) {
	for o.segIdx >= len(o.segLens) {
		// need a new page
		if o.eos {
			return nil, io.EOF
		}
		if err := o.readPage(); err != nil {
			return nil, err
		}
	}
	pkt := o.pageBuf[o.segOffsets[o.segIdx] : o.segOffsets[o.segIdx]+o.segLens[o.segIdx]]
	o.segIdx++
	return pkt, nil
}

// readPage reads one Ogg page from the underlying reader and splits it
// into segments.
func (o *oggOpusReader) readPage() error {
	var hdr [27]byte
	if _, err := io.ReadFull(o.r, hdr[:]); err != nil {
		return err
	}
	if string(hdr[:4]) != oggCapture {
		return fmt.Errorf("ogg: bad capture pattern %q", hdr[:4])
	}
	if hdr[4] != 0 {
		return fmt.Errorf("ogg: bad stream structure version %d", hdr[4])
	}
	headerType := hdr[5]
	nSeg := int(hdr[26])

	// read segment table
	segTable := make([]byte, nSeg)
	if _, err := io.ReadFull(o.r, segTable); err != nil {
		return err
	}

	// group the segment table into logical packets.
	// For Opus (1 packet per segment typically), each segment is its
	// own logical packet. We just record (offset, length) for each
	// segment and treat each as one Opus packet.
	totalData := 0
	for _, s := range segTable {
		totalData += int(s)
	}
	o.pageBuf = make([]byte, totalData)
	if _, err := io.ReadFull(o.r, o.pageBuf); err != nil {
		return err
	}

	offsets := make([]int, 0, nSeg)
	lens := make([]int, 0, nSeg)
	off := 0
	for _, s := range segTable {
		offsets = append(offsets, off)
		lens = append(lens, int(s))
		off += int(s)
	}
	o.segOffsets = offsets
	o.segLens = lens
	o.segIdx = 0

	if headerType&0x04 != 0 {
		o.eos = true
	}
	return nil
}

// opusHead is the parsed Opus identification header.
type opusHead struct {
	Version         uint8
	ChannelCount    uint8
	PreSkip         uint16
	InputSampleRate uint32
	OutputGain      int16
	MappingFamily   uint8
}

// ParseOpusHead validates the first Ogg page is an OpusHead and returns
// its parsed fields.
func ParseOpusHead(packet []byte) (*opusHead, error) {
	if len(packet) < 19 {
		return nil, errors.New("opus: OpusHead too short")
	}
	if string(packet[:8]) != "OpusHead" {
		return nil, errors.New("opus: missing OpusHead magic")
	}
	h := &opusHead{
		Version:         packet[8],
		ChannelCount:    packet[9],
		PreSkip:         binary.LittleEndian.Uint16(packet[10:12]),
		InputSampleRate: binary.LittleEndian.Uint32(packet[12:16]),
		OutputGain:      int16(binary.LittleEndian.Uint16(packet[16:18])),
		MappingFamily:   packet[18],
	}
	if h.Version != 1 {
		return nil, fmt.Errorf("opus: unsupported OpusHead version %d", h.Version)
	}
	if h.ChannelCount < 1 || h.ChannelCount > 8 {
		return nil, fmt.Errorf("opus: invalid channel count %d", h.ChannelCount)
	}
	return h, nil
}
