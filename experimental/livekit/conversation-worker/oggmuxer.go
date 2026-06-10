// Minimal Ogg Opus muxer. Mirror of the spike's Ogg Opus demuxer
// (ogg.go in this package, oggdemuxer.go in publisher/). Writes a
// valid Ogg Opus stream to an io.Writer so that ffmpeg's `ogg`
// demuxer can decode it back to PCM.
//
// Stream layout written:
//   Page 1 (BOS=0x02): OpusHead packet
//   Page 2:            OpusTags packet
//   Page 3+:           one Opus frame per page (one segment each)
//
// Granule positions: 0 for the BOS page (OpusHead) and the comment
// page (OpusTags); sample count for audio pages. ffmpeg's ogg
// demuxer accepts granule=0 for header pages.
//
// CRC: we compute the OGG CRC32 — the FORWARD variant of CRC-32
// (polynomial 0x04C11DB7, init=0, no final XOR, no reflection).
// This is NOT the same as Go's hash/crc32.IEEE which uses the
// reflected algorithm with init=0xFFFFFFFF; using that would
// produce different CRC values and ffmpeg's ogg demuxer would
// reject every page with "CRC mismatch!". The OGG spec uses the
// forward algorithm to match the libogg reference implementation.
package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// opusStreamSerial is the per-stream serial number written into each
// Ogg page header. Anything non-zero and unique is fine.
type oggMuxer struct {
	w            io.Writer
	serial       uint32
	channels     uint8
	inputRate    uint32
	pageSeq      uint32
	sampleCount  uint64 // opus samples emitted (48kHz reference) for granule pos
	wroteHead    bool
	wroteTags    bool
}

// newOggMuxer returns a fresh muxer targeting w. channels is 1 for
// mono (LiveKit spike). inputRate is the Opus frame's reference
// sample rate (48000 for the spike).
func newOggMuxer(w io.Writer, serial uint32, channels uint8, inputRate uint32) *oggMuxer {
	return &oggMuxer{
		w:         w,
		serial:    serial,
		channels:  channels,
		inputRate: inputRate,
	}
}

// writeOpusHead writes the BOS page containing the OpusHead
// identification header. Must be called exactly once before any
// OpusTags or frame pages.
func (m *oggMuxer) writeOpusHead() error {
	if m.wroteHead {
		return errors.New("ogg: OpusHead already written")
	}
	// OpusHead packet: 19 bytes.
	//   "OpusHead" (8) | version (1) | channels (1) | pre-skip (2 LE)
	//   | input sample rate (4 LE) | output gain (2 LE) | mapping family (1)
	preSkip := uint16(312) // 6.5ms at 48kHz, recommended for Opus
	outGain := int16(0)
	mapping := uint8(0) // 0 = mono/stereo, no surround
	pkt := make([]byte, 19)
	copy(pkt[0:8], "OpusHead")
	pkt[8] = 1 // version
	pkt[9] = m.channels
	binary.LittleEndian.PutUint16(pkt[10:12], preSkip)
	binary.LittleEndian.PutUint32(pkt[12:16], m.inputRate)
	binary.LittleEndian.PutUint16(pkt[16:18], uint16(outGain))
	pkt[18] = mapping
	// granule position is 0 for the BOS page (per OGG spec).
	if err := m.writePage(pkt, 0x02 /*BOS*/, 0); err != nil {
		return fmt.Errorf("ogg: write OpusHead page: %w", err)
	}
	m.wroteHead = true
	return nil
}

// writeOpusTags writes the comment header (OpusTags) on a normal
// (non-BOS) page. Must be called after OpusHead and before any frame
// pages. Vendor string is empty; user comment list is empty.
func (m *oggMuxer) writeOpusTags() error {
	if !m.wroteHead {
		return errors.New("ogg: OpusTags before OpusHead")
	}
	if m.wroteTags {
		return errors.New("ogg: OpusTags already written")
	}
	// OpusTags packet: "OpusTags" (8) | vendor length (4 LE)
	// | vendor string | user comment list length (4 LE) = 0
	vendor := ""
	pkt := make([]byte, 8+4+len(vendor)+4)
	copy(pkt[0:8], "OpusTags")
	binary.LittleEndian.PutUint32(pkt[8:12], uint32(len(vendor)))
	copy(pkt[12:12+len(vendor)], vendor)
	binary.LittleEndian.PutUint32(pkt[12+len(vendor):], 0)
	if err := m.writePage(pkt, 0x00, 0); err != nil {
		return fmt.Errorf("ogg: write OpusTags page: %w", err)
	}
	m.wroteTags = true
	return nil
}

// writeOpusFrame writes one Opus frame on its own OGG page. OGG
// supports multi-segment packets (one or more 0xFF segments
// representing 255 bytes each, followed by a final segment
// representing the remainder), so frames >255 bytes are emitted as
// a single logical packet split across segments.
func (m *oggMuxer) writeOpusFrame(frame []byte, frameSamples uint64) error {
	if !m.wroteTags {
		return errors.New("ogg: Opus frame before OpusTags")
	}
	if len(frame) == 0 {
		return nil
	}
	// Granule position is the sample count up to and including this
	// frame, in the stream's reference rate (48kHz for the spike).
	m.sampleCount += frameSamples
	if err := m.writePage(frame, 0x00, m.sampleCount); err != nil {
		return fmt.Errorf("ogg: write opus frame page: %w", err)
	}
	return nil
}

// writePage writes a single Ogg page containing the given packet
// (split into 255-byte segments in the segment table, with a final
// remainder segment). ffmpeg's ogg demuxer reconstructs packets
// from segments: 0xFF segments are "more to come" and the final
// segment ≤ 0xFF terminates the packet.
//
//	0-3:   "OggS"
//	4:     stream structure version (0)
//	5:     header type flag (BOS=0x02, EOS=0x04)
//	6-13:  granule position (LE uint64)
//	14-17: stream serial number (LE uint32)
//	18-21: page sequence number (LE uint32)
//	22-25: CRC (LE uint32) — computed over the whole page with
//	       this field set to 0
//	26:    number of segments
//	27..:  segment table
//	..:    segment data
func (m *oggMuxer) writePage(pkt []byte, headerType byte, granule uint64) error {
	// Build segment table: 0xFF for each 255-byte chunk, then one
	// final byte representing the remainder (0-255). This is the
	// canonical OGG multi-segment packet layout.
	var segTable []byte
	if len(pkt) == 0 {
		// A zero-length packet is one segment of 0 bytes (terminator).
		segTable = []byte{0}
	} else {
		for off := 0; off < len(pkt); off += 255 {
			n := len(pkt) - off
			if n >= 255 {
				segTable = append(segTable, 0xFF)
			} else {
				segTable = append(segTable, byte(n))
			}
		}
	}

	hdr := make([]byte, 27)
	copy(hdr[0:4], "OggS")
	hdr[4] = 0 // version
	hdr[5] = headerType
	binary.LittleEndian.PutUint64(hdr[6:14], granule)
	binary.LittleEndian.PutUint32(hdr[14:18], m.serial)
	binary.LittleEndian.PutUint32(hdr[18:22], m.pageSeq)
	// hdr[22:26] = CRC placeholder; filled in below.
	hdr[26] = byte(len(segTable))
	m.pageSeq++

	// Build the full page (header + segTable + packet) with CRC=0
	// first, then compute the OGG forward CRC32 over it and write
	// the CRC into hdr[22:26] in little-endian.
	page := make([]byte, 0, len(hdr)+len(segTable)+len(pkt))
	page = append(page, hdr...)
	page = append(page, segTable...)
	page = append(page, pkt...)

	// OGG CRC32: forward algorithm with polynomial 0x04C11DB7.
	// This is NOT the same as Go's hash/crc32.IEEE (which uses the
	// reflected algorithm). The forward algorithm:
	//   crc = (crc << 8) ^ table[((crc >> 24) & 0xff) ^ byte]
	// with init=0 and no final XOR.
	crc := oggForwardCRC(0, page)
	binary.LittleEndian.PutUint32(page[22:26], crc)

	if _, err := m.w.Write(page); err != nil {
		return err
	}
	return nil
}

// oggForwardTable is the 256-entry lookup table for the OGG CRC32
// forward algorithm (polynomial 0x04C11DB7, generated at package
// init time). Pre-computed once for performance.
var oggForwardTable = func() [256]uint32 {
	var t [256]uint32
	for i := 0; i < 256; i++ {
		c := uint32(i) << 24
		for j := 0; j < 8; j++ {
			if c&0x80000000 != 0 {
				c = (c << 1) ^ 0x04C11DB7
			} else {
				c <<= 1
			}
		}
		t[i] = c
	}
	return t
}()

// oggForwardCRC computes the OGG CRC32 over data using the forward
// algorithm. seed=0 for fresh CRC, or the running CRC for chunked
// processing.
func oggForwardCRC(seed uint32, data []byte) uint32 {
	crc := seed
	for _, b := range data {
		crc = (crc << 8) ^ oggForwardTable[((crc>>24)&0xff)^uint32(b)]
	}
	return crc
}
