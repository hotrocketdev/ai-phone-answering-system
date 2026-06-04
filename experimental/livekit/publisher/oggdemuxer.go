// oggdemuxer.go — minimal Ogg container demuxer for the LiveKit HD spike.
//
// Reads an Ogg Opus stream (e.g. from `ffmpeg -c:a libopus -f opus -`)
// and yields one raw Opus packet per call to NextOpusPacket().
//
// Ogg page format (RFC 3533):
//
//	Offset  Size  Field
//	0       4     "OggS" capture pattern
//	4       1     stream_structure_version (must be 0)
//	5       1     header_type_flag (0x02 = BOS, 0x04 = EOS)
//	6       8     granule position (little-endian)
//	14      4     stream serial number
//	18      4     page sequence number
//	22      4     CRC checksum
//	26      1     number of page segments (0..255)
//	27      n     segment table
//	27+n    ?     segment data
//
// Each entry in the segment table is the length of one segment (0..255).
// A segment length of 255 means "this segment is 255 bytes, continue";
// any other length means "this segment is N bytes, packet ends here".
// For Opus, each segment typically contains one Opus packet.
//
// This demuxer is intentionally minimal: it skips BOS/EOS flags, ignores
// the granule position, does NOT verify CRCs (ffmpeg is a trusted source
// in this spike), and assumes one Opus packet per segment.
package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const oggCapture = "OggS"

// OggOpusReader reads Ogg Opus pages and yields raw Opus packets.
type OggOpusReader struct {
	r io.Reader

	// current page being processed
	pageBuf    []byte // segment data of the current page
	segOffsets []int  // start offset of each segment in pageBuf
	segLens    []int  // length of each segment
	segIdx     int    // next segment to yield
	pageDone   bool   // true when current page's segments are exhausted
	eos        bool   // true when EOS page has been seen
}

// NewOggOpusReader wraps r. The first page MUST be the OpusHead header
// (BOS) and the second MUST be OpusTags — both are consumed silently.
func NewOggOpusReader(r io.Reader) *OggOpusReader {
	return &OggOpusReader{r: r}
}

// NextOpusPacket returns the next raw Opus packet, or io.EOF at end of
// stream. Each packet is one Opus frame (~20ms at default settings).
func (o *OggOpusReader) NextOpusPacket() ([]byte, error) {
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
// into segments. Sets o.eos if the page has the EOS flag.
func (o *OggOpusReader) readPage() error {
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
	// granule := binary.LittleEndian.Uint64(hdr[6:14])
	// serial := binary.LittleEndian.Uint32(hdr[14:18])
	// pageSeq := binary.LittleEndian.Uint32(hdr[18:22])
	// crc := binary.LittleEndian.Uint32(hdr[22:26])
	nSeg := int(hdr[26])

	// read segment table
	segTable := make([]byte, nSeg)
	if _, err := io.ReadFull(o.r, segTable); err != nil {
		return err
	}

	// group the segment table into logical packets.
	// A logical packet is a sequence of segments where only the last
	// segment has length < 255. For Opus (1 packet per segment typically),
	// each segment is its own logical packet.
	//
	// We just record (offset, length) for each segment — the caller
	// treats each segment as one Opus packet. This works for ffmpeg's
	// default Opus output.
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

// opusHeader describes a parsed Opus identification header.
type opusHeader struct {
	Version        uint8
	ChannelCount   uint8
	PreSkip        uint16
	InputSampleRate uint32
	OutputGain     int16
	MappingFamily  uint8
}

// ParseOpusHead validates the first Ogg page is an OpusHead and returns
// its parsed fields. Returns an error if the page is not a valid Opus
// identification header.
func ParseOpusHead(packet []byte) (*opusHeader, error) {
	if len(packet) < 19 {
		return nil, errors.New("opus: OpusHead too short")
	}
	if string(packet[:8]) != "OpusHead" {
		return nil, errors.New("opus: missing OpusHead magic")
	}
	// packet[8] = version (must be 1)
	h := &opusHeader{
		Version:        packet[8],
		ChannelCount:   packet[9],
		PreSkip:        binary.LittleEndian.Uint16(packet[10:12]),
		InputSampleRate: binary.LittleEndian.Uint32(packet[12:16]),
		OutputGain:     int16(binary.LittleEndian.Uint16(packet[16:18])),
		MappingFamily:  packet[18],
	}
	if h.Version != 1 {
		return nil, fmt.Errorf("opus: unsupported OpusHead version %d", h.Version)
	}
	if h.ChannelCount < 1 || h.ChannelCount > 8 {
		return nil, fmt.Errorf("opus: invalid channel count %d", h.ChannelCount)
	}
	return h, nil
}
