# Production Roadmap — Voice Quality First

Date: 2026-05-28  
Project: AI Voice Receptionist / VoxLane

## Honest Interpretation

This product will not win purely because it can answer a phone.

It will win if it sounds like a premium receptionist and handles real restaurant calls naturally.

Voice quality is not a cosmetic issue. It is a core product feature.

## Current Reality

The app now works, but the current transport path limits audio quality.

## Roadmap

### Stage 1 — Stabilise Current MVP

Goal:
Reliable demo with Twilio + OpenAI + Cartesia.

Tasks:
- stabilise frame pacing
- remove choppiness
- lock env setup
- improve prompt for restaurant workflow
- confirm booking flow
- add logs/metrics

### Stage 2 — Clean Codebase

Goal:
Stop workers from breaking the working path.

Tasks:
- remove/disable unused runtime experiments
- archive Deepgram
- remove stale prompts
- centralise provider config
- document current working path

### Stage 3 — HD Audio Spike

Goal:
Prove whether LiveKit/SIP improves quality enough to justify migration.

Tasks:
- LiveKit room prototype
- Cartesia Sonic 3.5 in HD output
- SIP inbound call with Telnyx/Simwood/Vonage
- compare with Twilio path

### Stage 4 — Production Architecture Decision

If HD path wins:
- Twilio becomes fallback/dev path
- LiveKit/SIP becomes production target

If HD path fails:
- optimise Twilio path as far as possible
- consider Cartesia managed phone
- consider alternative managed voice agent providers

### Stage 5 — SaaS Readiness

Only after voice path decision:
- tenant onboarding
- restaurant config dashboard
- booking integrations
- call logs
- analytics
- billing
- production deployment

## Strategic Rule

Do not build the full SaaS dashboard before deciding the production voice transport. The voice transport affects onboarding, costs, reliability, and perceived product quality.

## Preferred Future

```text
SIP/HD Transport
→ LiveKit
→ Cartesia Sonic 3.5
→ OpenAI/Grok
→ Restaurant tools
```

## Fallback Future

```text
Twilio Media Streams
→ Cartesia µ-law
→ OpenAI
```

Good enough for demos, not ideal as final premium product.
