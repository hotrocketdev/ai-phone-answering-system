# LiveKit HD Audio — Experimental Workspace

**Created**: 2026-05-28  
**Status**: Research only — no production code

---

## Purpose

This workspace holds research, config samples, and spike notes for the LiveKit + SIP + Cartesia HD audio path. Nothing here is wired into the production runtime.

## Reference Docs

- `docs/ADR-001-voice-transport-strategy.md` — Decision to move to HD transport
- `docs/NEXTGEN-LIVEKIT-HD-AUDIO-PLAN.md` — Full next-gen plan
- `docs/VOICE-STACK-DECISION-MATRIX.md` — Provider comparison
- `docs/PRODUCTION-ROADMAP-VOICE-QUALITY-FIRST.md` — Staged roadmap
- `docs/AGENT_RUNTIME_EVALUATION.md` — Go vs LiveKit Agents
- `docs/PRODUCTION_RUNTIME_BOUNDARIES.md` — What's reusable

## Architecture Target

```
UK SIP Number (Telnyx/Simwood)
  → SIP INVITE
    → LiveKit SIP Service
      → LiveKit Room (WebRTC SFU)
        → Agent Worker (Python or Go)
          → OpenAI Realtime / Grok (conversation)
          → Cartesia Sonic 3.5 (voice)
          → NestJS (tools/booking)
        → HD Audio to caller (Opus/G.722)
```

## Key Decisions Required

1. SIP Provider: Telnyx (existing account) vs Simwood (UK specialist)
2. Agent Runtime: Python LiveKit Agents vs Go gateway extension
3. Cartesia Config: Higher sample rate (24000+) vs current 8000
4. Conversation Brain: Keep OpenAI vs evaluate Grok

## Dependencies (for prototype)

- LiveKit server (Docker: `livekit/livekit-server`)
- LiveKit SIP service (Docker: `livekit/sip`)
- Redis (already have)
- Cartesia API key (already have)
- SIP trunk credentials (pending)

## Spike Phases (per NEXTGEN plan)

### Phase 1 — Research ✅
- [x] LiveKit Agents architecture understood
- [x] Cartesia LiveKit plugin confirmed
- [x] SIP codec support documented
- [x] UK provider options evaluated

### Phase 2 — Prototype (pending)
- [ ] Deploy LiveKit server locally
- [ ] Configure Telnyx SIP trunk
- [ ] Create minimal agent with Cartesia greeting
- [ ] Test audio quality

### Phase 3 — Compare (pending)
- [ ] Side-by-side comparison with Twilio path
- [ ] Latency measurement
- [ ] Voice quality assessment

### Phase 4 — Decide (pending)
- [ ] Production architecture decision
