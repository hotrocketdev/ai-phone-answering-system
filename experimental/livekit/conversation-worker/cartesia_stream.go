// Streaming Cartesia TTS helper for the Stage 1.5 Realtime pipeline.
//
// The existing cartesia.go (Synthesize) is non-streaming: it
// POSTs text to /tts/bytes, reads the entire response body, and
// decodes it to a []int16. That works for the stitched mode
// where one HTTP round-trip per turn is fine.
//
// For the realtime-cartesia mode we want to send each sentence
// to Cartesia as soon as the LLM finishes it, and pipe the
// returned PCM directly into the long-lived outbound Opus
// encoder. The latency win comes from starting the next TTS
// call as soon as a sentence is complete, not waiting for the
// full response.done marker.
//
// StreamSynthesize streams the response body chunk-by-chunk
// straight into the supplied io.Writer without buffering or
// decoding. The encoding (pcm_s16le / pcm_f32le) and
// sample_rate passed to Cartesia must match what the consumer
// (the outbound ffmpeg encoder) expects.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

// StreamSynthesize POSTs text to Cartesia /tts/bytes, reads the
// response body in chunks, and writes each chunk directly to w.
// It returns nil on a clean end-of-stream, or an error if the
// HTTP request fails, Cartesia returns a non-2xx, or the write
// to w fails.
//
// The encoding parameter ("pcm_s16le" or "pcm_f32le") and
// sampleRate (Hz) are passed through to Cartesia's
// output_format. Whatever bytes Cartesia returns are written to
// w verbatim — there is no decoding. The consumer is
// responsible for matching the format to its input expectations.
func StreamSynthesize(apiKey, text, voiceID, modelID, encoding string, sampleRate int, w io.Writer) error {
	if w == nil {
		return fmt.Errorf("cartesia: nil writer")
	}
	body := map[string]interface{}{
		"model_id":   modelID,
		"transcript": text,
		"voice": map[string]interface{}{
			"mode": "id",
			"id":   voiceID,
		},
		"output_format": map[string]interface{}{
			"container":   "raw",
			"encoding":    encoding,
			"sample_rate": sampleRate,
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("cartesia: marshal body: %w", err)
	}
	req, err := http.NewRequest("POST", "https://api.cartesia.ai/tts/bytes", bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("cartesia: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("Cartesia-Version", "2024-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("cartesia: http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("cartesia: http %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Stream raw PCM bytes from Cartesia straight into the
	// outbound encoder. Buffer size of 8192 matches the typical
	// ffmpeg pipe read; larger buffers reduce syscalls but
	// don't materially affect latency here.
	buf := make([]byte, 8192)
	var totalBytes int
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return fmt.Errorf("cartesia: write to sink: %w", werr)
			}
			totalBytes += n
		}
		if err != nil {
			if err == io.EOF {
				log.Printf("cartesia_stream: pcm_bytes=%d sample_rate=%d encoding=%s", totalBytes, sampleRate, encoding)
				return nil
			}
			return fmt.Errorf("cartesia: read: %w", err)
		}
	}
}

// isSentenceEnd reports whether s ends at a natural sentence
// boundary. Closing quotes/brackets after a sentence-terminating
// punctuation also count as a sentence end (so we don't flush
// mid-sentence on "Hi," then again on "How can I help?").
//
// The check is intentionally narrow: it must NOT fire on a
// trailing comma, semicolon, or colon — those are intra-sentence
// pauses and Cartesia handles them fine if the whole sentence
// is sent together.
func isSentenceEnd(s string) bool {
	if s == "" {
		return false
	}
	r := []rune(s)
	last := r[len(r)-1]
	if last == '.' || last == '!' || last == '?' || last == '\n' {
		return true
	}
	// Closing punctuation that follows a sentence terminator:
	// "How can I help?"  -> last char is " — but the "? pair is
	// what we care about. The check is "last char is a closing
	// quote/bracket AND the char before it is a terminator."
	if last == '"' || last == '\'' || last == ')' || last == ']' {
		if len(r) >= 2 {
			prev := r[len(r)-2]
			if prev == '.' || prev == '!' || prev == '?' {
				return true
			}
		}
	}
	return false
}
