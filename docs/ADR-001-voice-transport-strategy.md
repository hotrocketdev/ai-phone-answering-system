# ADR-001 — Voice Transport Strategy

Date: 2026-05-28  
Project: AI Voice Receptionist / VoxLane  
Status: Proposed

## Decision

The current Twilio Media Streams path should remain as a short-term MVP/fallback path, but it should not be treated as the long-term production-quality voice architecture.

The long-term architecture should move toward HD voice transport using LiveKit + SIP/HD codecs, with Cartesia as the primary voice renderer.

## Why

The current working stack uses:

```text
Twilio Media Streams
→ Go Gateway
→ OpenAI Realtime
→ Cartesia Sonic 3.5
→ Twilio outbound media
```

This now works, but the audio quality is limited by the transport layer. Twilio Media Streams outbound audio requires `audio/x-mulaw` at `8000Hz`, which forces Cartesia to output old narrowband telephone audio.

This means the app cannot sound like the Cartesia playground while using Twilio Media Streams.

## Current Production Candidate

```text
Caller
→ Twilio Media Streams
→ Go Gateway
→ OpenAI Realtime
→ Cartesia Sonic 3.5
→ Twilio
```

Use this for MVP validation only.

## Long-Term Target

```text
Caller
→ SIP/HD Voice Provider
→ LiveKit SIP / LiveKit Agents
→ OpenAI or Grok conversation brain
→ Cartesia Sonic 3.5 high-quality audio
→ Caller
```

## Why LiveKit

LiveKit is WebRTC-first and has native agent/telephony infrastructure. Cartesia has documented LiveKit integration. LiveKit SIP supports wideband audio codecs such as G.722, and Telnyx has HD voice support with G.722 and Opus.

## Why Not Delete Twilio Yet

The current Twilio path is valuable because:
- It proves the product can answer real phone calls.
- It validates the business logic.
- It provides a demo/fallback path.
- It gives a stable comparison point for future HD transports.

## Consequences

We should:
- keep Twilio path stable
- stop broad experimental rewrites
- add HD voice as a separate branch/spike
- avoid mixing too many provider experiments in the same runtime
- document each transport as a separate runtime path

## Non-Goals

Do not:
- rewrite the whole gateway immediately
- remove Twilio before LiveKit/SIP is proven
- switch conversation brain before fixing transport quality
- continue adding more provider experiments into the same production path

## Final Direction

Short term:

```text
Twilio + OpenAI + Cartesia
```

Long term:

```text
LiveKit/SIP + OpenAI or Grok + Cartesia
```
