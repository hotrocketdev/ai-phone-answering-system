// Package telnyx implements the provider.Adapter for Telnyx Media Streaming.
//
// Telnyx uses bidirectional RTP streaming over WebSocket:
// - Outbound: JSON media event with a base64 encoded RTP payload (raw audio, no RTP header)
// - Inbound: JSON media event with base64 encoded RTP payload (raw audio, no RTP header)
//
// Reference: https://developers.telnyx.com/docs/voice/programmable-voice/media-streaming
package telnyx

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"

	"github.com/gorilla/websocket"

	"github.com/voxlane/voice-gateway/internal/audio"
	"github.com/voxlane/voice-gateway/internal/provider"
)

// ─── Adapter ─────────────────────────────────────────────────────────────

type Adapter struct {
	conn     *websocket.Conn
	cfg      provider.TelnyxConfig
	callID   string
	streamID string

	writeMu sync.Mutex

	Frames chan provider.AudioFrame
	Events chan provider.Event

	outPacketCount  int
	inboundCodec    string
	inboundRate     int
	inboundChannels int
	trackMu         sync.Mutex
	trackStats      map[string]*trackStats
	trackCaptures   map[string]*trackCapture
}

// New creates a Telnyx adapter from a gorilla WebSocket connection.
func New(conn *websocket.Conn, callID string, cfg provider.TelnyxConfig) *Adapter {
	return &Adapter{
		conn:          conn,
		cfg:           cfg,
		callID:        callID,
		Frames:        make(chan provider.AudioFrame, 8),
		Events:        make(chan provider.Event, 16),
		trackStats:    make(map[string]*trackStats),
		trackCaptures: make(map[string]*trackCapture),
	}
}

// ReadLoop reads Telnyx JSON media events from the WebSocket.
func (a *Adapter) ReadLoop() {
	defer func() {
		a.conn.Close()
		close(a.Frames)
		close(a.Events)
	}()

	a.Events <- provider.Event{Type: provider.EventConnected}

	for {
		msgType, raw, err := a.conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				a.Events <- provider.Event{Type: provider.EventDisconnected}
			} else {
				a.Events <- provider.Event{Type: provider.EventDisconnected, Error: err}
			}
			return
		}

		if msgType == websocket.TextMessage {
			frame, event := a.ParseMediaEvent(raw)
			if frame != nil {
				select {
				case a.Frames <- *frame:
				default:
				}
			}
			if event != nil {
				a.Events <- *event
			}
			continue
		}

		if msgType == websocket.BinaryMessage {
			if len(raw) == 0 {
				continue
			}
			select {
			case a.Frames <- provider.AudioFrame{
				Codec:      a.currentInboundCodec(),
				SampleRate: 8000,
				Payload:    raw,
				Direction:  "inbound",
				CallID:     a.callID,
			}:
			default:
			}
		}
	}
}

func (a *Adapter) Type() provider.Type { return provider.TypeTelnyx }

func (a *Adapter) ValidateRequest(_ context.Context, _ map[string]string, _ []byte) (string, error) {
	return a.callID, nil
}

func (a *Adapter) GenerateCallControl(_ string, ctrl provider.CallControlResponse) ([]byte, string, error) {
	body := fmt.Sprintf(`{"stream_url":"%s","stream_track":"both_tracks","client_state":"%s"}`,
		ctrl.StreamURL, a.callID)
	return []byte(body), "application/json", nil
}

func (a *Adapter) ParseMediaEvent(raw []byte) (*provider.AudioFrame, *provider.Event) {
	var msg struct {
		Event          string `json:"event"`
		StreamID       string `json:"stream_id,omitempty"`
		SequenceNumber string `json:"sequence_number,omitempty"`
		Start          *struct {
			MediaFormat *struct {
				Encoding   string `json:"encoding"`
				SampleRate int    `json:"sample_rate"`
				Channels   int    `json:"channels"`
			} `json:"media_format,omitempty"`
		} `json:"start,omitempty"`
		Media *struct {
			Track     string `json:"track"`
			Chunk     string `json:"chunk"`
			Timestamp string `json:"timestamp"`
			Payload   string `json:"payload"`
		} `json:"media,omitempty"`
		Mark *struct {
			Name string `json:"name"`
		} `json:"mark,omitempty"`
		Payload *struct {
			Code   int    `json:"code"`
			Title  string `json:"title"`
			Detail string `json:"detail"`
		} `json:"payload,omitempty"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, &provider.Event{Type: provider.EventError, Error: err}
	}

	if msg.StreamID != "" {
		a.streamID = msg.StreamID
	}

	switch msg.Event {
	case "connected":
		return nil, &provider.Event{Type: provider.EventConnected}
	case "start":
		if msg.Start != nil && msg.Start.MediaFormat != nil {
			a.inboundCodec = normalizeTelnyxCodec(msg.Start.MediaFormat.Encoding)
			a.inboundRate = msg.Start.MediaFormat.SampleRate
			a.inboundChannels = msg.Start.MediaFormat.Channels
			log.Printf("[telnyx] inbound media format encoding=%s sample_rate=%d channels=%d",
				msg.Start.MediaFormat.Encoding, msg.Start.MediaFormat.SampleRate, msg.Start.MediaFormat.Channels)
		}
		return nil, &provider.Event{Type: provider.EventStarted}
	case "media":
		if msg.Media == nil || msg.Media.Payload == "" {
			return nil, nil
		}
		track := msg.Media.Track
		if track == "" {
			track = "inbound"
		}
		audio, err := base64.StdEncoding.DecodeString(msg.Media.Payload)
		if err != nil {
			return nil, &provider.Event{Type: provider.EventError, Error: err}
		}
		codec := a.currentInboundCodec()
		a.noteTrackFrame(track, audio, codec)
		return &provider.AudioFrame{
			Codec:      codec,
			SampleRate: 8000,
			Payload:    audio,
			Timestamp:  msg.Media.Timestamp,
			Direction:  track,
			CallID:     a.callID,
			StreamID:   a.streamID,
		}, nil
	case "stop":
		return nil, &provider.Event{Type: provider.EventStopped}
	case "mark":
		label := ""
		if msg.Mark != nil {
			label = msg.Mark.Name
		}
		return nil, &provider.Event{Type: provider.EventMark, Label: label}
	case "error":
		err := fmt.Errorf("telnyx stream error")
		if msg.Payload != nil {
			err = fmt.Errorf("telnyx stream error %d %s: %s", msg.Payload.Code, msg.Payload.Title, msg.Payload.Detail)
		}
		return nil, &provider.Event{Type: provider.EventError, Error: err}
	}
	return nil, nil
}

// EncodeAudio returns raw PCMU RTP payload bytes. Telnyx wraps these bytes in
// a JSON media envelope in WriteRaw; no 12-byte RTP header is sent.
func (a *Adapter) EncodeAudio(frame provider.AudioFrame) ([]byte, error) {
	pcmu := frame.Payload
	a.outPacketCount++
	if a.outPacketCount <= 5 {
		log.Printf("[telnyx] RTP payload out packet=%d payload_len=%d", a.outPacketCount, len(pcmu))
	}
	return pcmu, nil
}

func (a *Adapter) EncodeMark(label string) ([]byte, error) {
	msg := map[string]interface{}{
		"event": "mark",
		"mark":  map[string]string{"name": label},
	}
	return json.Marshal(msg)
}

// WriteRaw sends a Telnyx media event containing base64 encoded RTP payload data.
func (a *Adapter) WriteRaw(data []byte) error {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	msg, err := encodeOutboundMedia(data)
	if err != nil {
		return err
	}
	return a.conn.WriteMessage(websocket.TextMessage, msg)
}

func (a *Adapter) CloseMessage() []byte { return nil }
func (a *Adapter) CallID() string       { return a.callID }
func (a *Adapter) StreamID() string     { return a.streamID }
func (a *Adapter) Close() error         { return a.conn.Close() }

func (a *Adapter) currentInboundCodec() string {
	if a.inboundCodec == "" {
		return "pcmu"
	}
	return a.inboundCodec
}

func normalizeTelnyxCodec(codec string) string {
	switch codec {
	case "PCMA", "pcma", "alaw", "A-LAW":
		return "pcma"
	case "PCMU", "pcmu", "ulaw", "U-LAW":
		return "pcmu"
	default:
		return codec
	}
}

func encodeOutboundMedia(payload []byte) ([]byte, error) {
	msg := map[string]interface{}{
		"event": "media",
		"media": map[string]string{
			"payload": base64.StdEncoding.EncodeToString(payload),
		},
	}
	return json.Marshal(msg)
}

type trackStats struct {
	frames int
	bytes  int
	first  map[byte]int
}

type trackCapture struct {
	pcmuPath string
	wavPath  string
	pcmu     []byte
	pcm16    []byte
	frames   int
	closed   bool
}

const debugTrackCaptureFrames = 900

func (a *Adapter) noteTrackFrame(track string, payload []byte, codec string) {
	a.trackMu.Lock()
	defer a.trackMu.Unlock()

	if a.trackStats == nil {
		a.trackStats = make(map[string]*trackStats)
	}
	if a.trackCaptures == nil {
		a.trackCaptures = make(map[string]*trackCapture)
	}

	stats := a.trackStats[track]
	if stats == nil {
		stats = &trackStats{first: make(map[byte]int)}
		a.trackStats[track] = stats
	}
	stats.frames++
	stats.bytes += len(payload)
	if len(payload) > 0 {
		stats.first[payload[0]]++
	}

	if stats.frames <= 5 || stats.frames%50 == 0 {
		log.Printf("[telnyx] track metadata call_suffix=%s track=%s codec=%s payload_len=%d first_bytes=%s direction_assigned=%s",
			callIDSuffix(a.callID), track, codec, len(payload), firstByteSummary(stats.first), track)
	}

	if os.Getenv("DEBUG_TELNYX_TRACK_CAPTURE") != "true" {
		return
	}
	capture := a.trackCaptures[track]
	if capture == nil {
		capture = newTrackCapture(a.callID, track, codec)
		a.trackCaptures[track] = capture
	}
	capture.add(payload, a.callID, track, codec)
}

func newTrackCapture(callID, track, codec string) *trackCapture {
	safeID := safeFilename(callID)
	safeTrack := safeFilename(track)
	base := filepath.Join(os.TempDir(), "voxlane-"+safeTrack+"-track-"+safeID)
	c := &trackCapture{
		pcmuPath: base + "." + codec,
		wavPath:  base + ".wav",
	}
	log.Printf("[telnyx] track capture enabled call_suffix=%s track=%s codec=%s raw=%s wav=%s max_frames=%d",
		callIDSuffix(callID), track, codec, c.pcmuPath, c.wavPath, debugTrackCaptureFrames)
	return c
}

func (c *trackCapture) add(pcmu []byte, callID, track, codec string) {
	if c.closed || c.frames >= debugTrackCaptureFrames {
		return
	}
	c.pcmu = append(c.pcmu, pcmu...)
	pcm16, err := audio.G711ToPCM16(codec, pcmu)
	if err != nil {
		log.Printf("[telnyx] track capture decode failed call_suffix=%s track=%s codec=%s: %v", callIDSuffix(callID), track, codec, err)
		return
	}
	c.pcm16 = append(c.pcm16, pcm16...)
	c.frames++
	if c.frames == debugTrackCaptureFrames {
		c.close(callID, track)
	}
}

func (c *trackCapture) close(callID, track string) {
	if c.closed || len(c.pcmu) == 0 {
		return
	}
	c.closed = true
	if err := os.WriteFile(c.pcmuPath, c.pcmu, 0600); err != nil {
		log.Printf("[telnyx] track capture pcmu write failed call_suffix=%s track=%s: %v", callIDSuffix(callID), track, err)
		return
	}
	if err := writePCM16WAV(c.wavPath, c.pcm16, 8000); err != nil {
		log.Printf("[telnyx] track capture wav write failed call_suffix=%s track=%s: %v", callIDSuffix(callID), track, err)
		return
	}
	log.Printf("[telnyx] track capture saved call_suffix=%s track=%s pcmu=%s wav=%s frames=%d bytes=%d",
		callIDSuffix(callID), track, c.pcmuPath, c.wavPath, c.frames, len(c.pcmu))
}

func firstByteSummary(counts map[byte]int) string {
	type kv struct {
		b byte
		n int
	}
	items := make([]kv, 0, len(counts))
	for b, n := range counts {
		items = append(items, kv{b: b, n: n})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].n > items[j].n })
	if len(items) > 3 {
		items = items[:3]
	}
	out := ""
	for i, item := range items {
		if i > 0 {
			out += ","
		}
		out += fmt.Sprintf("0x%02X:%d", item.b, item.n)
	}
	return out
}

func callIDSuffix(callID string) string {
	if len(callID) <= 8 {
		return callID
	}
	return callID[len(callID)-8:]
}

func safeFilename(value string) string {
	return regexp.MustCompile(`[^a-zA-Z0-9_.-]+`).ReplaceAllString(value, "_")
}

func writePCM16WAV(path string, pcm16 []byte, sampleRate uint32) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	dataSize := uint32(len(pcm16))
	byteRate := sampleRate * 2
	blockAlign := uint16(2)
	if _, err := f.Write([]byte("RIFF")); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, uint32(36)+dataSize); err != nil {
		return err
	}
	if _, err := f.Write([]byte("WAVEfmt ")); err != nil {
		return err
	}
	for _, v := range []interface{}{
		uint32(16), uint16(1), uint16(1), sampleRate, byteRate, blockAlign, uint16(16),
	} {
		if err := binary.Write(f, binary.LittleEndian, v); err != nil {
			return err
		}
	}
	if _, err := f.Write([]byte("data")); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, dataSize); err != nil {
		return err
	}
	_, err = f.Write(pcm16)
	return err
}
