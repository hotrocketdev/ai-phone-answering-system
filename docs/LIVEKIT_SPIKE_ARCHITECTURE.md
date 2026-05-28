# LiveKit HD Audio — Spike Architecture

**Branch**: `spike/livekit-hd-audio`  
**Status**: Research phase — no code changes to production  
**Date**: 2026-05-28  

**References existing architecture docs**:
- `ADR-001-voice-transport-strategy.md` — Decision to move to HD transport
- `NEXTGEN-LIVEKIT-HD-AUDIO-PLAN.md` — Full next-gen plan
- `VOICE-STACK-DECISION-MATRIX.md` — Provider comparison
- `PRODUCTION-ROADMAP-VOICE-QUALITY-FIRST.md` — Staged roadmap
- `CODEBASE-CLEANUP-AND-FOCUS-PLAN.md` — Cleanup candidates

---

## 1. Rationale

Current production path uses Twilio Media Streams (µ-law 8kHz narrowband). PSTN codec strips frequencies above 4kHz, making Cartesia Sonic 3.5 voices sound compressed and "mechanical" compared to the browser playground (44.1kHz WAV).

HD audio over SIP delivers wideband (G.722 at 16kHz, Opus up to 48kHz) — potentially 4x the frequency range, making Cartesia voices sound as good as the playground.

## 2. Proposed Architecture

```
UK SIP Number (Telnyx/Simwood)
  → SIP INVITE
    → LiveKit SIP Service (Go)
      → LiveKit Room (WebRTC SFU)
        → LiveKit Agent Worker (Python)
          → STT: Deepgram / OpenAI
          → LLM: OpenAI Realtime / Grok
          → TTS: Cartesia Sonic 3.5 (via plugin)
        → WebRTC audio to SIP caller
```

## 3. LiveKit Research Summary

### LiveKit Server
- Open-source WebRTC SFU (Apache 2.0)
- Can be self-hosted (Docker) or LiveKit Cloud
- Provides rooms, participants, tracks, data channels
- Global edge deployment available

### LiveKit Agents Framework
- Python primary (v1.5.12, 10.7k GitHub stars)
- Plugin ecosystem: OpenAI, Cartesia, Deepgram, Silero, ElevenLabs
- Built-in turn detection, interruption handling
- Function calling / tool use support
- AgentServer for production deployment

### LiveKit SIP
- Go-based SIP bridge (`github.com/livekit/sip`)
- Accepts inbound SIP INVITEs → routes to LiveKit rooms
- Supports Digest authentication
- DTMF passthrough
- Requires Redis for state

### Cartesia Integration
- Official `livekit-plugins-cartesia` Python package
- Both direct API and LiveKit Inference (hosted) modes
- Streaming TTS with low latency
- STT also supported via Cartesia

## 4. Codec Comparison

| Codec | Sample Rate | Bandwidth | Quality |
|-------|------------|-----------|---------|
| G.711 µ-law (current) | 8kHz | 64 kbps | Narrowband (300-3400 Hz) |
| G.722 | 16kHz | 64 kbps | Wideband (50-7000 Hz) |
| Opus | 8-48kHz | 6-510 kbps | Fullband, adaptive |

**Expected improvement**: G.722/Opus delivers 2-4x the frequency range of µ-law. Cartesia voices will sound significantly more natural.

## 5. UK SIP Provider Comparison

| Provider | UK Numbers | HD Codecs | Pricing | Notes |
|----------|-----------|-----------|---------|-------|
| **Telnyx** | Yes (0121, London) | G.722, Opus | $0.005/min inbound | Already have account. SIP trunking supported. |
| **Simwood** | Yes (UK geographic) | G.722 | Free geographic numbers | UK specialist. Lower cost. |
| **DIDWW** | Yes (92 countries) | G.722 | Pay-as-you-go | Broad coverage. |
| **Vonage** | Yes | G.722 | Commercial | Enterprise-focused. |

**Recommendation**: Telnyx for immediate testing (existing account). Simwood for production UK numbers (free geographic, UK focus).

## 6. Reusability Assessment

| Current Component | Reusable in LiveKit? | Notes |
|-------------------|---------------------|-------|
| Go Voice Gateway | Partially | WS handling not needed; provider abstraction reusable |
| Audio Pipeline | No | LiveKit handles codec negotiation |
| State Machine | Yes | Conversation logic is provider-agnostic |
| NestJS Backend | Yes | Tool API, booking logic unchanged |
| Provider Abstractions | Partially | LLM/Renderer interfaces remain useful |
| Session Manager | Rewrite | LiveKit Agents framework replaces this |
| Redis Store | Yes | Same pattern for session state |

## 7. Engineering Complexity

| Area | Effort |
|------|--------|
| LiveKit Server setup (Docker) | Low |
| LiveKit SIP service | Medium |
| Agent worker (Python) | Medium — rewrite from Go |
| Cartesia plugin | Low — official integration |
| Tool calling | Medium — function_tool decorator |
| Production deployment | Medium — AgentServer + Redis |
| **Total estimate** | **2-3 weeks for PoC** |

## 8. Architecture Boundaries

```
Current (MVP):
  Twilio → Go Gateway → OpenAI → Cartesia → Twilio

Future (HD):
  SIP → LiveKit SIP → LiveKit Room → Agent Worker → Cartesia → LiveKit → SIP

Fallback (preserved):
  Twilio → Go Gateway → OpenAI → Cartesia → Twilio (unchanged)
```

Both paths share: OpenAI/Grok for LLM, Cartesia for TTS, NestJS for tools/booking.

## 9. Next Steps

1. Set up LiveKit server locally (Docker)
2. Configure Telnyx SIP trunk pointing to LiveKit SIP service
3. Create minimal Python agent worker with Cartesia plugin
4. Test audio quality comparison: µ-law vs Opus/G.722
5. Measure latency: SIP setup time, TTS first byte, end-to-end

## 10. Risks

- LiveKit Agents framework is Python — team needs Python expertise
- SIP provider latency varies by region
- LiveKit SIP service adds operational complexity
- Cartesia via LiveKit may have different latency than direct API
