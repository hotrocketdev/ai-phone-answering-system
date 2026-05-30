# Telnyx Adapter Implementation Plan

**Branch**: `feature/telnyx-direct-websocket-adapter`
**Status**: Pre-implementation
**Date**: 2026-05-30

---

## Prerequisites

- [ ] VPS provisioned with Caddy (see `docs/VPS_CADDY_DEPLOYMENT_CHECKLIST.md`)
- [ ] Public domain with DNS configured
- [ ] Twilio webhook verified working on VPS
- [ ] Telnyx account created with API key
- [ ] Telnyx phone number purchased (UK +44)

---

## Phase 1 — Telnyx Webhook Setup

File: `voice-gateway/internal/provider/telnyx/webhook.go` (new)

- [ ] Parse Telnyx JSON webhook payload (`call.initiated`)
- [ ] Validate HMAC-SHA256 signature using `TELNYX_WEBHOOK_SECRET`
- [ ] Extract `call_control_id`, `from`, `to`
- [ ] Return Telnyx Call Control JSON commands:
  - `call.answered` — accept the call
  - `stream.start` — start bidirectional media stream to our WebSocket

### Webhook Handler Flow

```
POST /api/public/voice/webhook (Telnyx)
  → Validate HMAC signature
  → If invalid → 401
  → If valid → Respond with answer + stream commands
  → Telnyx opens WS to wss://voxlane.example.com/stream/{call_control_id}
```

## Phase 2 — WebSocket Event Mapping

File: `voice-gateway/internal/provider/telnyx/adapter.go` (new)

Telnyx WebSocket is simpler than Twilio:
- **No JSON wrapper**: raw PCMU bytes on binary frames
- **No events**: connection close = hangup
- **No multiplexing**: one call per connection

| Telnyx Event | Adapter Event |
|-------------|---------------|
| WS connection opened | `EventConnected` |
| Binary frame received | `AudioFrame` (PCMU, 8kHz, inbound) |
| Binary frame to send | `EncodeAudio` → raw bytes |
| WS connection closed | `EventDisconnected` |

### ReadLoop (simplified)

```go
func (a *Adapter) ReadLoop() {
    defer func() { a.conn.Close(); close(a.Frames); close(a.Events) }()
    a.Events <- provider.Event{Type: provider.EventConnected}
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

### EncodeAudio (simplified)

```go
func (a *Adapter) EncodeAudio(frame provider.AudioFrame) ([]byte, error) {
    return frame.Payload, nil // raw bytes, no JSON wrapper
}
```

### GenerateCallControl

```go
func (a *Adapter) GenerateCallControl(callID string, ctrl provider.CallControlResponse) ([]byte, string, error) {
    // Return JSON, not XML (unlike Twilio)
    resp := map[string]interface{}{
        "data": map[string]interface{}{
            "call_control_id": callID,
            "commands": []map[string]interface{}{
                {"command": "answer"},
                {"command": "stream.start", "stream_url": ctrl.StreamURL, "codec": "PCMU"},
            },
        },
    }
    body, _ := json.Marshal(resp)
    return body, "application/json", nil
}
```

## Phase 3 — Provider Selection

File: `voice-gateway/cmd/gateway/main.go` (modify)

```go
var adapter provider.Adapter
switch cfg.TelephonyProvider {
case "telnyx":
    adapter = providertelnyx.New(conn, callSid, pCfg.Telnyx)
default:
    adapter = providertwilio.New(conn, callSid, pCfg.Twilio)
}
```

Config: `TELEPHONY_PROVIDER=twilio|telnyx`

## Phase 4 — PCMU First Test

1. Set `TELEPHONY_PROVIDER=telnyx`
2. Configure Telnyx webhook to VPS endpoint
3. Dial Telnyx number
4. Verify:
   - [ ] Webhook receives `call.initiated`
   - [ ] WebSocket opens at `/stream/{call_control_id}`
   - [ ] Gateway receives inbound audio frames
   - [ ] OpenAI responds with text
   - [ ] Cartesia renders audio
   - [ ] Outbound audio frames reach Telnyx
   - [ ] Caller hears greeting and can converse

## Phase 5 — L16 Second Test

After PCMU verified:

1. Change `TELNYX_STREAM_CODEC=L16`
2. Change `TELNYX_BIDIRECTIONAL_CODEC=L16`
3. Change Cartesia output to `pcm_s16le 16000`
4. Verify audio quality improvement
5. Document PCMU vs L16 quality comparison

## Phase 6 — Fixed Greeting Test

After PCMU verified:

1. Enable `FAST_STATIC_GREETING=true` (with streamSid gate)
2. Verify greeting plays before OpenAI responds
3. Measure latency improvement

## Phase 7 — Twilio Fallback Verification

After Telnyx verified:

1. Set `TELEPHONY_PROVIDER=twilio` → verify Twilio still works
2. Set `TELEPHONY_PROVIDER=telnyx` → verify Telnyx works
3. Document fallback procedure

---

## Do Not Implement Yet

- LiveKit integration
- HD audio codecs beyond L16
- SIP trunking
- Conference/multi-party calls
- Recording
