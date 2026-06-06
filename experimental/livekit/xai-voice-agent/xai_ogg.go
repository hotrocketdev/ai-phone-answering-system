// xai_ogg.go — minimal Ogg Opus muxer and demuxer for the xai-voice-agent
// harness. Ports the proven pattern from conversation-worker/oggmuxer.go
// and ogg.go to avoid the CGo dependency that layeh.com/gopus requires.
//
// Inbound (LiveKit mic -> xAI):
//   - LiveKit samplebuilder gives us raw Opus frames.
//   - We wrap each frame in an OGG page and pipe to ffmpeg.
//   - ffmpeg decodes the OGG Opus to PCM16 24 kHz mono.
//   - We send 100 ms PCM chunks to xAI.
//
// Outbound (xAI -> browser):
//   - xAI returns PCM16 24 kHz mono deltas.
//   - ffmpeg encodes them to OGG Opus 48 kHz with small pages.
//   - We demux the OGG pages back to raw Opus frames.
//   - We push each frame to the LiveKit LocalSampleTrack.
package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// --- muxer (raw Opus frame -> OGG page) ---

type oggMuxer struct {
	w           io.Writer
	serial      uint32
	channels    uint8
	inputRate   uint32
	pageSeq     uint32
	sampleCount uint64
	wroteHead   bool
	wroteTags   bool
}

func newOggMuxer(w io.Writer, serial uint32, channels uint8, inputRate uint32) *oggMuxer {
	return &oggMuxer{
		w:         w,
		serial:    serial,
		channels:  channels,
		inputRate: inputRate,
	}
}

func (m *oggMuxer) writeOpusHead() error {
	if m.wroteHead {
		return errors.New("ogg: OpusHead already written")
	}
	preSkip := uint16(312)
	outGain := int16(0)
	mapping := uint8(0)
	pkt := make([]byte, 19)
	copy(pkt[0:8], "OpusHead")
	pkt[8] = 1
	pkt[9] = m.channels
	binary.LittleEndian.PutUint16(pkt[10:12], preSkip)
	binary.LittleEndian.PutUint32(pkt[12:16], m.inputRate)
	binary.LittleEndian.PutUint16(pkt[16:18], uint16(outGain))
	pkt[18] = mapping
	if err := m.writePage(pkt, 0x02, 0); err != nil {
		return fmt.Errorf("ogg: write OpusHead page: %w", err)
	}
	m.wroteHead = true
	return nil
}

func (m *oggMuxer) writeOpusTags() error {
	if !m.wroteHead {
		return errors.New("ogg: OpusTags before OpusHead")
	}
	if m.wroteTags {
		return errors.New("ogg: OpusTags already written")
	}
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

func (m *oggMuxer) writeOpusFrame(frame []byte, frameSamples uint64) error {
	if !m.wroteTags {
		return errors.New("ogg: Opus frame before OpusTags")
	}
	if len(frame) == 0 {
		return nil
	}
	m.sampleCount += frameSamples
	if err := m.writePage(frame, 0x00, m.sampleCount); err != nil {
		return fmt.Errorf("ogg: write opus frame page: %w", err)
	}
	return nil
}

func (m *oggMuxer) writePage(pkt []byte, headerType byte, granule uint64) error {
	var segTable []byte
	if len(pkt) == 0 {
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
	hdr[4] = 0
	hdr[5] = headerType
	binary.LittleEndian.PutUint64(hdr[6:14], granule)
	binary.LittleEndian.PutUint32(hdr[14:18], m.serial)
	binary.LittleEndian.PutUint32(hdr[18:22], m.pageSeq)
	hdr[26] = byte(len(segTable))
	m.pageSeq++

	page := make([]byte, 0, len(hdr)+len(segTable)+len(pkt))
	page = append(page, hdr...)
	page = append(page, segTable...)
	page = append(page, pkt...)

	crc := oggForwardCRC(0, page)
	binary.LittleEndian.PutUint32(page[22:26], crc)

	if _, err := m.w.Write(page); err != nil {
		return err
	}
	return nil
}

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

func oggForwardCRC(seed uint32, data []byte) uint32 {
	crc := seed
	for _, b := range data {
		crc = (crc << 8) ^ oggForwardTable[((crc>>24)&0xff)^uint32(b)]
	}
	return crc
}

// --- demuxer (OGG page -> raw Opus packet) ---

const oggCapture = "OggS"

type oggOpusReader struct {
	r io.Reader

	pageBuf     []byte
	segTable    []byte
	segIdx      int
	pageDataOff int
	eos         bool
	pageDone    bool
}

func newOggOpusReader(r io.Reader) *oggOpusReader {
	return &oggOpusReader{r: r, pageDone: true}
}

func (o *oggOpusReader) NextOpusPacket() ([]byte, error) {
	if o.pageDone {
		if o.eos {
			return nil, io.EOF
		}
		if err := o.readPage(); err != nil {
			return nil, err
		}
	}
	var pkt []byte
	for o.segIdx < len(o.segTable) {
		segLen := int(o.segTable[o.segIdx])
		if o.pageDataOff+segLen > len(o.pageBuf) {
			return nil, fmt.Errorf("ogg: segment overruns page buffer (pageBuf=%d pageDataOff=%d segLen=%d)",
				len(o.pageBuf), o.pageDataOff, segLen)
		}
		seg := o.pageBuf[o.pageDataOff : o.pageDataOff+segLen]
		pkt = append(pkt, seg...)
		o.pageDataOff += segLen
		o.segIdx++
		if segLen < 255 {
			if o.segIdx >= len(o.segTable) {
				o.pageDone = true
			}
			return pkt, nil
		}
	}
	o.pageDone = true
	return pkt, nil
}

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
	segTable := make([]byte, nSeg)
	if _, err := io.ReadFull(o.r, segTable); err != nil {
		return err
	}
	totalData := 0
	for _, s := range segTable {
		totalData += int(s)
	}
	o.pageBuf = make([]byte, totalData)
	if _, err := io.ReadFull(o.r, o.pageBuf); err != nil {
		return err
	}
	o.segTable = segTable
	o.segIdx = 0
	o.pageDataOff = 0
	o.pageDone = false
	if headerType&0x04 != 0 {
		o.eos = true
	}
	return nil
}
