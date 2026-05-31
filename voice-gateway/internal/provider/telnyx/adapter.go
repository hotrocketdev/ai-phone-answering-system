// Package telnyx implements the provider.Adapter for Telnyx Media Streaming.
//
// Telnyx uses bidirectional RTP streaming over WebSocket:
// - Outbound: RTP-encapsulated PCMU payloads (172 bytes = 12 header + 160 payload)
// - Inbound: RTP-encapsulated PCMU payloads (header stripped to raw PCMU)
//
// Reference: https://developers.telnyx.com/docs/voice/voice-ai/media-streams
package telnyx

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"math/rand"
	"sync"

	"github.com/gorilla/websocket"

	"github.com/voxlane/voice-gateway/internal/provider"
)

// ─── Adapter ─────────────────────────────────────────────────────────────

type Adapter struct {
	conn   *websocket.Conn
	cfg    provider.TelnyxConfig
	callID string

	writeMu sync.Mutex

	Frames chan provider.AudioFrame
	Events chan provider.Event

	// RTP state
	rtpSeq   uint16
	rtpTS    uint32
	rtpSSRC  uint32
	rtpFirst bool // true until first packet sent
}

// New creates a Telnyx adapter from a gorilla WebSocket connection.
func New(conn *websocket.Conn, callID string, cfg provider.TelnyxConfig) *Adapter {
	return &Adapter{
		conn:     conn,
		cfg:      cfg,
		callID:   callID,
		Frames:   make(chan provider.AudioFrame, 8),
		Events:   make(chan provider.Event, 16),
		rtpSeq:   uint16(rand.Intn(65536)),
		rtpTS:    uint32(rand.Intn(65536)),
		rtpSSRC:  rand.Uint32(),
		rtpFirst: true,
	}
}

// ReadLoop reads RTP-framed PCMU data from the Telnyx WebSocket.
// Strips 12-byte RTP header, emits raw PCMU payloads.
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

		if msgType == websocket.BinaryMessage {
			// Strip RTP header (12 bytes) to get raw PCMU
			pcmu := raw
			if len(raw) >= 12 {
				pcmu = raw[12:]
			}
			if len(pcmu) == 0 {
				continue
			}
			select {
			case a.Frames <- provider.AudioFrame{
				Codec:      "pcmu",
				SampleRate: 8000,
				Payload:    pcmu,
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

func (a *Adapter) ParseMediaEvent(_ []byte) (*provider.AudioFrame, *provider.Event) {
	return nil, nil
}

// EncodeAudio wraps PCMU payload in an RTP header and returns the full packet.
func (a *Adapter) EncodeAudio(frame provider.AudioFrame) ([]byte, error) {
	pcmu := frame.Payload

	packet := make([]byte, 12+len(pcmu))
	packet[0] = 0x80 // V=2, P=0, X=0, CC=0
	if a.rtpFirst {
		packet[1] = 0x80 // marker bit set on first packet
		a.rtpFirst = false
	}
	// packet[1] bits 0-6 are payload type (0 for PCMU) — already 0

	binary.BigEndian.PutUint16(packet[2:], a.rtpSeq)
	a.rtpSeq++

	binary.BigEndian.PutUint32(packet[4:], a.rtpTS)
	a.rtpTS += uint32(len(pcmu)) // 160 for 20ms PCMU

	binary.BigEndian.PutUint32(packet[8:], a.rtpSSRC)

	copy(packet[12:], pcmu)

	// Debug first 5 packets
	if a.rtpSeq <= 5 {
		log.Printf("[telnyx] RTP out seq=%d ts=%d pt=0 ssrc=%x plen=%d tlen=%d",
			a.rtpSeq-1, a.rtpTS-uint32(len(pcmu)), a.rtpSSRC, len(pcmu), len(packet))
	}

	return packet, nil
}

func (a *Adapter) EncodeMark(_ string) ([]byte, error) { return nil, nil }

// WriteRaw sends binary data directly on the WebSocket.
func (a *Adapter) WriteRaw(data []byte) error {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	return a.conn.WriteMessage(websocket.BinaryMessage, data)
}

func (a *Adapter) CloseMessage() []byte { return nil }
func (a *Adapter) CallID() string      { return a.callID }
func (a *Adapter) StreamID() string     { return a.callID }
func (a *Adapter) Close() error         { return a.conn.Close() }
