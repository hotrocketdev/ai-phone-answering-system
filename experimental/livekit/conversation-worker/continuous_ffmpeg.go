// Continuous ffmpeg helpers for the Realtime pipeline.
//
// Unlike the per-utterance ffmpeg calls in opus_decode.go, Realtime
// is a continuous streaming protocol: audio flows in both directions
// without a "turn boundary" marker. We run two long-lived ffmpeg
// subprocesses:
//
//   1. opusDecodeContinuous: OGG Opus 48kHz mono on stdin,
//      PCM16 24kHz mono on stdout. Used for the inbound (browser
//      mic) -> Realtime path.
//
//   2. opusEncodeContinuous: PCM16 24kHz mono on stdin,
//      OGG Opus 48kHz mono on stdout. Used for the Realtime ->
//      browser speakers path.
//
// Both use the same drain-in-goroutine pattern as
// decodeOpusUtteranceToPCM to avoid the 64KB pipe-buffer deadlock.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
	"sync"
)

// pcmChunkBytes is the chunk size of PCM16 24kHz mono audio we
// forward to the Realtime input buffer per append event. 100ms =
// 2400 samples = 4800 bytes. Realtime's docs recommend 100ms
// chunks; smaller adds overhead, larger increases latency.
const pcmChunkBytes = 4800

// pcmFrameBytes20ms is the size of a 20ms PCM16 24kHz frame
// (24000 * 0.020 * 2 = 960 bytes). We use this for the
// encodeContinuous output: 20ms Opus frames pushed into the
// outbound track.
const pcmFrameBytes20ms = 960

// opusDecodeContinuousCmd returns the ffmpeg argv that decodes an
// OGG Opus stream (from stdin) to PCM16 24kHz mono on stdout.
func opusDecodeContinuousCmd(filterChain string) []string {
	return []string{
		"-hide_banner",
		"-loglevel", "error",
		"-f", "ogg",
		"-i", "pipe:0",
		"-af", filterChain,
		"-ar", "24000",
		"-ac", "1",
		"-f", "s16le",
		"pipe:1",
	}
}

// opusEncodeContinuousCmd returns the ffmpeg argv that encodes a
// PCM16 24kHz mono stream (from stdin) to OGG Opus 48kHz mono on
// stdout.
//
// -flush_packets 1 and -oggpagesize 256 force the OGG muxer to
// write small pages (one Opus frame per page) and flush
// immediately. The default OGG page size of 4096 batches ~17
// Opus frames per page, which causes the consumer to block
// reading a full page at a time — unacceptable for
// sentence-by-sentence TTS where we need frames to start
// flowing within ~20-50ms of Cartesia returning the first
// audio bytes.
func opusEncodeContinuousCmd(bitrate int) []string {
	return []string{
		"-hide_banner",
		"-loglevel", "error",
		"-f", "s16le",
		"-ar", "24000",
		"-ac", "1",
		"-i", "pipe:0",
		"-c:a", "libopus",
		"-b:a", fmt.Sprintf("%d", bitrate),
		"-flush_packets", "1",
		"-oggpagesize", "256",
		"-f", "ogg",
		"pipe:1",
	}
}

// continuousPcm is the result of starting a continuous ffmpeg. The
// caller writes to stdin, reads from stdout, and calls kill on
// shutdown.
type continuousPcm struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  io.ReadCloser
	kill    func()
}

// startContinuous starts an ffmpeg subprocess with the given argv
// (excluding the program name). The returned continuousPcm has
// stdin (write PCM in) and stdout (read PCM/OGG out) pipes ready.
// A goroutine is spawned to drain stderr into log. The caller is
// responsible for draining stdout and writing stdin (with the
// drain-in-goroutine pattern) to avoid pipe-buffer deadlock.
//
// On any setup error, all already-opened pipes are closed and the
// subprocess (if started) is killed.
func startContinuous(ctx context.Context, argv []string) (*continuousPcm, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("continuous: empty argv")
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", argv...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("continuous: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("continuous: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("continuous: stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, fmt.Errorf("continuous: start: %w", err)
	}

	// Drain stderr in background. ffmpeg uses stderr for its log.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, e := stderr.Read(buf)
			if n > 0 {
				log.Printf("ffmpeg[stderr]: %s", string(buf[:n]))
			}
			if e != nil {
				return
			}
		}
	}()

	cp := &continuousPcm{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
		kill: func() {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		},
	}
	return cp, nil
}

// writeAll writes all of data to cp.stdin, retrying on partial
// writes. Returns an error if the pipe is broken.
func (cp *continuousPcm) writeAll(data []byte) error {
	for len(data) > 0 {
		n, err := cp.stdin.Write(data)
		if n > 0 {
			data = data[n:]
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// oggContinuousReader demuxes a continuous OGG Opus stream from
// an io.Reader and skips the leading OpusHead and OpusTags
// packets. After that, each NextOpusPacket returns one 20ms Opus
// frame, or io.EOF at end of stream.
type oggContinuousReader struct {
	r        *oggOpusReader
	skipHead int
}

// newOggContinuousReader wraps r and discards the first two
// packets (OpusHead and OpusTags) automatically. Subsequent calls
// return real Opus frames.
func newOggContinuousReader(r io.Reader) *oggContinuousReader {
	return &oggContinuousReader{r: newOggOpusReaderImpl(r)}
}

// NextOpusPacket returns the next Opus frame, or io.EOF. The
// first two calls transparently consume OpusHead and OpusTags.
func (o *oggContinuousReader) NextOpusPacket() ([]byte, error) {
	for o.skipHead < 2 {
		pkt, err := o.r.NextOpusPacket()
		if err != nil {
			return nil, err
		}
		if len(pkt) >= 8 {
			magic := string(pkt[:8])
			if magic == "OpusHead" || magic == "OpusTags" {
				o.skipHead++
				continue
			}
		}
		// Not a header — this is a real Opus frame. Mark headers
		// done and return.
		o.skipHead = 2
		return pkt, nil
	}
	return o.r.NextOpusPacket()
}

// chunkBuffer accumulates variable-size PCM reads and yields
// fixed-size chunks (typically 100ms = 4800 bytes) for the
// Realtime input buffer.
type chunkBuffer struct {
	mu   sync.Mutex
	buf  []byte
	size int
}

func newChunkBuffer(chunkSize int) *chunkBuffer {
	return &chunkBuffer{size: chunkSize}
}

// push adds data to the buffer. Returns as many complete chunks
// as possible plus a boolean indicating if at least one chunk
// was returned. The remainder stays in the buffer for the next
// call.
func (c *chunkBuffer) push(data []byte) (chunks [][]byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buf = append(c.buf, data...)
	for len(c.buf) >= c.size {
		chunks = append(chunks, c.buf[:c.size])
		c.buf = c.buf[c.size:]
	}
	return chunks
}

// flush returns any remaining bytes (less than chunkSize) and
// clears the buffer. Used at end of stream.
func (c *chunkBuffer) flush() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.buf) == 0 {
		return nil
	}
	out := c.buf
	c.buf = nil
	return out
}

// silencePcm returns n bytes of PCM16 silence (zero).
func silencePcm(n int) []byte {
	return make([]byte, n)
}

// opusRawReader reads a stream of raw Opus packets produced by
// ffmpeg's "-f data" muxer. Each packet is prefixed with a
// 4-byte big-endian length. The reader returns one Opus
// frame per NextOpusPacket() call.
type opusRawReader struct {
	r io.Reader
}

func newOpusRawReader(r io.Reader) *opusRawReader {
	return &opusRawReader{r: r}
}

// NextOpusPacket reads the next 4-byte length, then the Opus
// frame body, and returns the frame bytes (without the length
// prefix). Returns io.EOF at end of stream.
func (o *opusRawReader) NextOpusPacket() ([]byte, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(o.r, lenBuf[:]); err != nil {
		return nil, err
	}
	length := int(lenBuf[0])<<24 | int(lenBuf[1])<<16 | int(lenBuf[2])<<8 | int(lenBuf[3])
	if length == 0 {
		return []byte{}, nil
	}
	if length < 0 || length > 65536 {
		return nil, fmt.Errorf("opus_raw: invalid length %d", length)
	}
	pkt := make([]byte, length)
	if _, err := io.ReadFull(o.r, pkt); err != nil {
		return nil, err
	}
	return pkt, nil
}
