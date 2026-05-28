# Production Runtime Boundaries

**Date**: 2026-05-28  
**Branch**: main  
**Status**: Architecture documentation

---

## Current Production Runtime

```
VOICE_RUNTIME=custom
VOICE_RENDERER=cartesia
```

```
Twilio Media Streams → Go Gateway → OpenAI Realtime → Cartesia Sonic 3.5 → Twilio
```

---

## Transport-Specific Code (Twilio)

These packages/modules are tightly coupled to Twilio Media Streams:

| Package | Dependency | Notes |
|---------|-----------|-------|
| `internal/provider/twilio/adapter.go` | Twilio WebSocket protocol | ParseMediaEvent, EncodeAudio specific to Twilio JSON format |
| `internal/twilio/handler.go` | Twilio Media Streams | Legacy handler, partially supplanted by provider adapter |
| `cmd/gateway/main.go` | Twilio WS upgrade | `/stream/{callSid}` handler, gorilla WebSocket upgrader |
| `backend/src/modules/voice/voice.controller.ts` | Twilio TwiML | Generates `<Connect><Stream>` XML |

## Transport-Agnostic Code (Reusable)

These packages are independent of Twilio and reusable in LiveKit/SIP:

| Package | Purpose | Notes |
|---------|---------|-------|
| `internal/audio/` | u-law codec, PCM resampler | Pure DSP, no transport assumptions |
| `internal/config/` | Environment loading, validation | Provider-agnostic |
| `internal/openai/` | OpenAI Realtime WS client | Transport-agnostic conversation brain |
| `internal/redis/` | Session state persistence | Standard Redis operations |
| `internal/session/sm/` | Conversation state machine | Pure Go logic, no I/O |
| `internal/renderer/cartesia/` | Cartesia Sonic TTS | Transport-agnostic voice rendering |
| `internal/llm/` | LLM provider abstraction | Interface only, no transport |
| `internal/renderer/` | Renderer provider abstraction | Interface only |
| `backend/src/modules/tools/` | Booking tool API | Business logic, transport-agnostic |
| `backend/src/modules/booking/` | Booking service | Business logic |
| `shared/src/` | TypeScript types | Pure data |

## Codec Assumptions (u-law)

The current path assumes u-law 8kHz throughout:

| Component | Assumption | Impact |
|-----------|-----------|--------|
| `audio/mulaw.go` | u-law ↔ PCM16 conversion | Needed only for Twilio path |
| `audio/resampler.go` | 8kHz ↔ 24kHz resampling | Not needed in HD path |
| `provider/twilio/adapter.go` | u-law payload encoding | Twilio-specific |
| `renderer/cartesia/renderer.go` | pcm_mulaw 8000 output format | Configurable — can change to higher rate |

For HD path, Cartesia output format changes from `pcm_mulaw 8000` to `pcm_s16le 24000` (or higher). Resampler no longer needed. u-law codec no longer needed for outbound.

## Migration Risk Matrix

| Component | Risk | Mitigation |
|-----------|------|------------|
| Session manager | Medium | Heavily refactored during debugging; needs stabilisation before migration |
| Provider adapter | Low | Interface already exists; Twilio adapter is isolated |
| Audio pipeline | Low | Only needed for Twilio; HD path bypasses it |
| State machine | None | Pure logic, no transport dependency |
| NestJS backend | None | Business logic, HTTP-based |
| Config validation | Low | Add HD runtime option without breaking existing |
| Frame pacing / remainder buffer | Medium | Recent additions; need tests before migration |

## Reusability Summary

For LiveKit+SIP path:
- ✅ Keep: state machine, config, Redis, NestJS, Cartesia renderer, LLM abstraction, tool calling
- ❌ Replace: Twilio adapter, audio pipeline, WS handler, TwiML generation
- 🔶 Adapt: session manager (needs LiveKit-compatible session lifecycle), OpenAI client (may switch to Grok)
