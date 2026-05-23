# Voice Provider Abstraction

**Date**: 2026-05-23  
**Status**: Implemented — Twilio is production, Telnyx is scaffold, SignalWire is placeholder  

---

## Why

Twilio's regulatory bundle approval process is delaying number purchase. The system must support multiple phone providers so we can switch to Telnyx while keeping Twilio available.

The abstraction isolates provider-specific logic (webhook format, media stream protocol, call control responses) behind a common interface. The rest of the system — session management, audio pipeline, OpenAI realtime, state machine — works with provider-neutral types.

## Architecture

```
┌──────────────────────────────────────────────┐
│              Session Manager                 │
│  (provider-neutral: AudioFrame, Event)       │
└──────────────────┬───────────────────────────┘
                   │ provider.Adapter interface
    ┌──────────────┼──────────────┐
    │              │              │
┌───▼───┐    ┌────▼────┐   ┌────▼────────┐
│Twilio │    │ Telnyx  │   │ SignalWire  │
│Adapter│    │ Adapter │   │ Adapter     │
│  ✅   │    │  🔶     │   │  ⬜         │
└───────┘    └─────────┘   └─────────────┘
```

## Provider Status

| Provider | Status | Notes |
|----------|--------|-------|
| Twilio | ✅ Implemented | Webhook tested via ngrok, WS stream validated |
| Telnyx | 🔶 Scaffold | Implements interface, not yet tested with real account |
| SignalWire | ⬜ Placeholder | Returns "not implemented" errors |

## Interface

Located in `voice-gateway/internal/provider/provider.go`:

- `ValidateRequest()` — authenticate inbound webhook
- `GenerateCallControl()` — produce provider-specific call response (TwiML / JSON)
- `ParseMediaEvent()` — decode WS message → `AudioFrame` or `Event`
- `EncodeAudio()` — encode outbound `AudioFrame` → provider WS message
- `EncodeMark()` — create sync/mark message
- `Close()` — graceful teardown

## Adding a New Provider

1. Create `internal/provider/<name>/adapter.go`
2. Implement the `provider.Adapter` interface
3. Add provider type constant in `provider.go`
4. Add config struct in `provider.go`
5. Add env var loading in `config/config.go`
6. Add validation in `config/config.go:validate()`
7. Add NestJS webhook endpoint in `voice.controller.ts`
8. Add env vars to `.env.example`

## Environment Variables

| Variable | Required For |
|----------|-------------|
| `VOICE_PROVIDER` | All — values: `twilio`, `telnyx`, `signalwire` |
| `TWILIO_ACCOUNT_SID` | `VOICE_PROVIDER=twilio` |
| `TWILIO_AUTH_TOKEN` | `VOICE_PROVIDER=twilio` |
| `TELNYX_API_KEY` | `VOICE_PROVIDER=telnyx` |
| `TELNYX_CONNECTION_ID` | `VOICE_PROVIDER=telnyx` |
| `TELNYX_PUBLIC_KEY` | `VOICE_PROVIDER=telnyx` |
| `SIGNALWIRE_PROJECT_ID` | `VOICE_PROVIDER=signalwire` |
| `SIGNALWIRE_TOKEN` | `VOICE_PROVIDER=signalwire` |
| `SIGNALWIRE_SPACE_URL` | `VOICE_PROVIDER=signalwire` |
| `GATEWAY_WS_URL` | All — WebSocket stream URL embedded in call control response |

## Provider Differences

| Feature | Twilio | Telnyx | SignalWire |
|---------|--------|--------|------------|
| Webhook format | TwiML XML | JSON | XML (TwiML-compatible) |
| Auth method | X-Twilio-Signature | API key header | HTTP Basic |
| Media encoding | u-law 8kHz base64 | PCMU/ulaw 8kHz | u-law 8kHz base64 |
| Stream protocol | WebSocket (JSON + base64) | WebSocket (JSON + base64) | WebSocket (JSON + base64) |
| Call ID field | CallSid | call_control_id | CallSid |

## Risks

- Telnyx media stream format may differ from assumptions (needs real-call validation)
- SignalWire is Twilio-compatible in theory but differences may exist
- Provider-specific error handling not yet implemented for non-Twilio paths
