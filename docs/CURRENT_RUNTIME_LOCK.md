# Current Runtime Lock

**Date**: 2026-05-28  
**Status**: Enforced — do not change without explicit approval

---

## Production Runtime

```
VOICE_RUNTIME=custom
VOICE_RENDERER=cartesia
```

This is the ONLY supported production path. All other runtimes are experimental or archived.

## What This Means

| Layer | Provider | Role |
|-------|----------|------|
| Telephony | Twilio Media Streams | Call transport (MVP) |
| Conversation | OpenAI Realtime | Text/conversation only |
| Voice | Cartesia Sonic 3.5 | All spoken output |
| Business Logic | NestJS | Booking tools, webhooks |

## Hard Rules

1. **OpenAI audio must be dropped** in Cartesia mode. No American voice leakage.
2. **Cartesia is the only voice renderer.** No OpenAI native voice, no Deepgram.
3. **Twilio is temporary.** It is the MVP transport, not the final production transport.
4. **LiveKit is future, not current.** It lives on `spike/livekit-hd-audio` only.
5. **No new providers** without explicit architecture decision.
6. **No manual turn detection** — OpenAI Realtime handles VAD and turn taking.
7. **Single outbound audio writer** per call — no overlapping audio streams.

## Env Lock

```
VOICE_RUNTIME=custom
VOICE_RENDERER=cartesia
OPENAI_REALTIME_MODEL=gpt-realtime-1.5
CARTESIA_MODEL=sonic-3.5
```

## What Is Deliberately NOT In Production

- Deepgram Voice Agent — archived, experimental only
- OpenAI native voice — replaced by Cartesia
- ElevenLabs — scaffold only
- Grok — scaffold only
- LiveKit/SIP — spike branch only
- Cartesia direct greeting — debug flag only

## Changing This Lock

Any change to VOICE_RUNTIME or VOICE_RENDERER requires:
1. Architecture decision documented
2. Spike branch tested
3. Explicit approval
4. This document updated
