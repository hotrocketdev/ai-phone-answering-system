# Telnyx Direct WebSocket Adapter

**Branch**: `feature/telnyx-direct-websocket-adapter` (merged to `main`)  
**Status**: **Phase 1 PCMU implemented** — adapter deployed, live test pending  
**Date**: 2026-05-30

---

## Goal

Replace ngrok + Twilio Media Streams with a direct Telnyx WebSocket connection. Telnyx routes SIP calls directly to a public WebSocket endpoint, eliminating both ngrok instability and Twilio's proprietary Media Streams protocol.

---

## Architecture

```
Caller → Telnyx SIP → Public WebSocket (wss://voxlane.example.com/stream/{callSid})
                             ↓
                       Caddy (TLS termination)
                             ↓
                       Gateway:8080 (WebSocket upgrade)
                             ↓
                       Session (OpenAI + Cartesia)
```

No ngrok. No Twilio Media Streams. No SIP trunking needed.

---

## Telnyx Call Flow

### 1. Webhook (Inbound Call)

Telnyx sends HTTP POST to configured webhook URL:
```
POST https://voxlane.example.com/api/public/voice/webhook
```

Payload (JSON):
```json
{
  "data": {
    "event_type": "call.initiated",
    "payload": {
      "call_control_id": "v2:...",
      "connection_id": "...",
      "caller_id_name": "+44...",
      "from": "+44...",
      "to": "+44...",
      "direction": "incoming"
    }
  }
}
```

### 2. Answer + Stream Command

Our webhook responds with Telnyx Call Control command to answer and bridge audio to our WebSocket:
```json
{
  "data": {
    "event_type": "call.answered",
    "payload": {
      "call_control_id": "...",
      "client_state": "..."
    }
  }
}
```

Then send stream command:
```json
{
  "stream_url": "wss://voxlane.example.com/stream/{callSid}",
  "stream_bidirectional_codec": "PCMU",
  "stream_bidirectional_target_bitrate": 64000,
  "stream_track": "both_tracks"
}
```

### 3. WebSocket Media Stream

After the stream command, Telnyx opens a WebSocket to our endpoint and sends/receives raw PCMU audio frames.

**Inbound**: Telnyx → Our WS: PCMU (8kHz, 8-bit μ-law) raw bytes, no JSON wrapper
**Outbound**: Our WS → Telnyx: PCMU (8kHz, 8-bit μ-law) raw bytes, no JSON wrapper

---

## Protocol Comparison

| Feature | Twilio Media Streams | Telnyx Direct WebSocket |
|---------|---------------------|------------------------|
| Transport | WebSocket | WebSocket |
| Inbound format | JSON `{"event":"media","media":{"payload":"<base64>"}}` | Raw PCMU bytes |
| Outbound format | JSON `{"event":"media","streamSid":"...","media":{"payload":"<base64>"}}` | Raw PCMU bytes |
| Stream ID | `streamSid` in JSON | None — per-connection |
| Audio codec | PCMU (μ-law 8kHz) | PCMU (μ-law 8kHz) |
| HD codec | None | L16 16kHz (future) |
| Bidirectional | Yes | Yes |
| VAD/events | Start/Stop/Mark events | None — raw audio only |
| Signaling | TwiML XML | REST API + JSON webhooks |
| NAT traversal | Twilio handles | Needs public IP (VPS) |
| Authentication | Twilio auth token | API key + webhook signature |

---

## Gateway Integration

### Adapter Interface

The existing `provider.Adapter` interface needs a new implementation:

```go
type TelnyxAdapter struct {
    conn        *websocket.Conn
    callID      string
    writeMu     sync.Mutex
    Frames      chan provider.AudioFrame
    Events      chan provider.Event
}
```

### Key Differences from Twilio Adapter

1. **No JSON wrapper**: Telnyx sends/receives raw PCMU bytes, not base64-encoded JSON
2. **No streamSid**: Each WS connection corresponds to one call — no multiplexing
3. **No events**: No `start`/`stop`/`mark` events. Hangup detected by WS close.
4. **Webhook format**: JSON, not form-urlencoded
5. **Authentication**: HMAC signature validation on webhooks

### ReadLoop

```go
func (a *TelnyxAdapter) ReadLoop() {
    defer func() { a.conn.Close(); close(a.Frames); close(a.Events) }()
    for {
        msgType, data, err := a.conn.ReadMessage()
        if err != nil {
            a.Events <- provider.Event{Type: provider.EventDisconnected, Error: err}
            return
        }
        if msgType == websocket.BinaryMessage {
            a.Frames <- provider.AudioFrame{
                Codec: "PCMU", SampleRate: 8000,
                Payload: data, Direction: "inbound",
                CallID: a.callID,
            }
        }
    }
}
```

### EncodeAudio

```go
func (a *TelnyxAdapter) EncodeAudio(frame provider.AudioFrame) ([]byte, error) {
    return frame.Payload, nil // raw bytes, no JSON wrapper
}
```

### WriteRaw

```go
func (a *TelnyxAdapter) WriteRaw(data []byte) error {
    a.writeMu.Lock()
    defer a.writeMu.Unlock()
    return a.conn.WriteMessage(websocket.BinaryMessage, data)
}
```

---

## Configuration

```bash
# Telnyx API
TELEPHONY_PROVIDER=twilio        # Default: twilio, future: telnyx
TELNYX_API_KEY=
TELNYX_CONNECTION_ID=
TELNYX_PHONE_NUMBER=
TELNYX_WEBHOOK_SECRET=           # HMAC secret for webhook validation
TELNYX_STREAM_CODEC=PCMU         # PCMU or L16
TELNYX_BIDIRECTIONAL_CODEC=PCMU  # PCMU or L16
```

---

## Codec Ladder

**G711U/PCMU 8kHz is compatibility-only**, not a quality target. It matches Twilio's current µ-law path and proves the Telnyx adapter pipe works. The business goal is near-human voice quality.

| Priority | Codec | Sample Rate | Bitrate | Quality | Status |
|----------|-------|-------------|---------|---------|--------|
| 1 | **PCMU (G711U)** | 8000 Hz | 64 kbps | Telephone (compatibility-only) | ✅ Adapter implemented, live test pending |
| 2 | **G722** | 16000 Hz | 64 kbps | HD voice (first real quality target) | ⬜ Requires Cartesia L16 16kHz output |
| 3 | **OPUS** | 8000–48000 Hz | 6–510 kbps | Modern HD (best if supported) | ⬜ Research needed |
| 4 | **AMR-WB** | 16000 Hz | 12–24 kbps | Mobile HD (optional) | ⬜ Optional comparison |
| 5 | **L16 48kHz** | 48000 Hz | 768 kbps | Full HD studio | ⬜ Future (LiveKit) |

### Codec Plan

1. **PCMU first** — prove adapter pipe works, matches Cartesia's current `pcm_mulaw` output
2. **G722 next** — change Cartesia output to `pcm_s16le 16000`, remap to G722 format in adapter. First real HD quality.
3. **OPUS after** — if Telnyx Media Streaming supports OPUS bidirectional, evaluate quality
4. **LiveKit** — only if Telnyx direct WS doesn't reach target quality with G722/OPUS

### Telnyx Portal Codec Settings

For the Telnyx application (Inbound tab), enable:
- ✅ G722
- ✅ OPUS  
- ✅ AMR-WB
- ✅ G711U (fallback only)

---

## Security

- Telnyx webhooks are signed with HMAC-SHA256 using the webhook secret
- Gateway validates webhook signatures before processing
- API key never exposed in client-side code
- WebSocket connections are per-call (no cross-call leakage)

---

## Migration Path

1. Provision VPS with Caddy (see PUBLIC_ENDPOINT_MIGRATION_PLAN.md)
2. Purchase Telnyx number (UK +44)
3. Configure Telnyx webhook to `https://voxlane.example.com/api/public/voice/webhook`
4. Implement `TelnyxAdapter` in `voice-gateway/internal/provider/telnyx/`
5. Wire `TELEPHONY_PROVIDER=telnyx` in gateway main.go
6. Test with local call → verify PCMU audio flows
7. Once stable, switch `TELEPHONY_PROVIDER=twilio` default to `telnyx`
8. Keep Twilio adapter as fallback

---

## Open Questions

- Does Telnyx bidirectional audio support L16? (check official docs)
- What is the max WS connection timeout?
- How does Telnyx handle reconnection on network loss?
- Can we receive DTMF digits via the media stream?
