// oggdemuxer_test.go — unit tests for the minimal Ogg Opus demuxer.
//
// These tests build Ogg Opus streams in memory (matching ffmpeg's
// output format) and verify the demuxer extracts packets correctly.
package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

// makeOggPage builds a single Ogg page with the given segment data.
// lacingTable describes the packet sizes; each entry becomes one
// segment. Granule position is set to 0.
func makeOggPage(serial uint32, pageSeq uint32, headerType byte, packets [][]byte) []byte {
	// Build segment table
	var lacing []byte
	for _, p := range packets {
		// split each packet into 255-byte segments
		for rem := len(p); rem >= 255; rem -= 255 {
			lacing = append(lacing, 255)
		}
		lacing = append(lacing, byte(len(p)%255))
	}

	body := []byte{}
	for _, p := range packets {
		body = append(body, p...)
	}

	hdr := make([]byte, 27)
	copy(hdr[0:4], []byte("OggS"))
	hdr[4] = 0 // version
	hdr[5] = headerType
	binary.LittleEndian.PutUint64(hdr[6:14], 0)   // granule
	binary.LittleEndian.PutUint32(hdr[14:18], serial)
	binary.LittleEndian.PutUint32(hdr[18:22], pageSeq)
	binary.LittleEndian.PutUint32(hdr[22:26], 0)  // CRC (we don't compute it)
	hdr[26] = byte(len(lacing))

	return append(append(hdr, lacing...), body...)
}

func TestOggOpusReaderParsesHeadAndTags(t *testing.T) {
	head := []byte("OpusHead" + "\x01\x01\x00\x00\x00\xbb\x80\x00\x00\x00\x00")
	tags := []byte("OpusTags" + "vendor=ffmpeg\x00\x00\x00\x00")
	// one segment of "real" opus data
	data := []byte{0xfc, 0xde, 0xad, 0xbe, 0xef}

	page1 := makeOggPage(0x11223344, 0, 0x02, [][]byte{head})
	page2 := makeOggPage(0x11223344, 1, 0x00, [][]byte{tags})
	page3 := makeOggPage(0x11223344, 2, 0x00, [][]byte{data})

	stream := append(append(page1, page2...), page3...)
	r := NewOggOpusReader(bytes.NewReader(stream))

	// First packet should be OpusHead
	pkt1, err := r.NextOpusPacket()
	if err != nil {
		t.Fatalf("read head: %v", err)
	}
	if !bytes.Equal(pkt1, head) {
		t.Errorf("head mismatch: got %q want %q", pkt1, head)
	}
	// Parse and validate
	h, err := ParseOpusHead(pkt1)
	if err != nil {
		t.Fatalf("parse head: %v", err)
	}
	if h.Version != 1 || h.ChannelCount != 1 {
		t.Errorf("head fields wrong: %+v", h)
	}

	// Second packet should be OpusTags
	pkt2, err := r.NextOpusPacket()
	if err != nil {
		t.Fatalf("read tags: %v", err)
	}
	if !bytes.Equal(pkt2, tags) {
		t.Errorf("tags mismatch: got %q want %q", pkt2, tags)
	}

	// Third packet should be the real Opus data
	pkt3, err := r.NextOpusPacket()
	if err != nil {
		t.Fatalf("read data: %v", err)
	}
	if !bytes.Equal(pkt3, data) {
		t.Errorf("data mismatch: got %v want %v", pkt3, data)
	}

	// Fourth should be EOF
	_, err = r.NextOpusPacket()
	if err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestOggOpusReaderRejectsBadCapture(t *testing.T) {
	bad := []byte("BAD!")
	bad = append(bad, make([]byte, 23)...)
	r := NewOggOpusReader(bytes.NewReader(bad))
	_, err := r.NextOpusPacket()
	if err == nil {
		t.Errorf("expected error on bad capture pattern, got nil")
	}
}

func TestOggOpusReaderRejectsBadOpusHead(t *testing.T) {
	// Wrong magic
	badHead := []byte("NotHead\x00\x01\x01\x00\x00\x00\xbb\x80\x00\x00\x00\x00")
	page := makeOggPage(0x1, 0, 0x02, [][]byte{badHead})
	r := NewOggOpusReader(bytes.NewReader(page))
	pkt, err := r.NextOpusPacket()
	if err != nil {
		t.Fatalf("read bad head: %v", err)
	}
	if _, err := ParseOpusHead(pkt); err == nil {
		t.Errorf("expected parse error on bad OpusHead, got nil")
	}
}
