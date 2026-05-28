# Agent Runtime Evaluation — Go vs LiveKit Agents Python

**Date**: 2026-05-28  
**Status**: Research notes — no decision made

---

## Current Runtime: Go Gateway

The Go voice gateway handles:
- WebSocket management (Twilio ↔ OpenAI)
- Audio transcoding (u-law ↔ PCM)
- Session lifecycle
- Conversation state machine
- Tool call execution (HMAC → NestJS)
- Voice renderer routing (OpenAI native / Cartesia)
- Provider abstraction (Twilio adapter)

**Strengths**: Single binary, low latency, fine-grained control, existing investment, zero-cost goroutines.

**Weaknesses**: All transport-specific logic mixed with orchestration. Session manager has accumulated experimental code.

## Alternative: LiveKit Agents Python

LiveKit Agents framework provides:
- Built-in room/session management
- Plugin ecosystem (Cartesia, OpenAI, Deepgram, Silero)
- Turn detection, interruption handling
- Function calling / tool use
- AgentServer for production deployment
- WebRTC SFU for HD audio

**Strengths**: Batteries-included agent framework, official Cartesia plugin, built-in HD audio via WebRTC.

**Weaknesses**: Python runtime (different from existing Go stack), less control over audio pipeline, new deployment model.

## Comparison Matrix

| Concern | Go Gateway | LiveKit Agents |
|---------|-----------|----------------|
| Latency | ~460ms end-to-end | ~570ms (adds SIP + LiveKit setup) |
| Audio quality | µ-law 8kHz narrowband | Opus/G.722 wideband |
| Maintainability | One codebase, Go-only | Python agent + Go SIP service |
| Tool calling | Direct HMAC HTTP to NestJS | function_tool decorator, HTTP calls |
| Provider abstraction | Custom interfaces | Plugin system |
| Interruption | Manual + OpenAI VAD | Built-in turn detection |
| Deployment | Single binary + Docker | AgentServer + LiveKit server + Redis |
| Developer experience | Go expertise required | Python (broader talent pool) |
| Debugging | Logs, single process | Agent Console (LiveKit Cloud) |
| Scaling | Goroutine per call | AgentServer worker pool |

## Recommendation: Complement, Don't Replace

The Go gateway should remain as the **orchestration and business logic layer**. LiveKit should handle **transport and media**.

```
SIP → LiveKit SIP Service (Go) → LiveKit Room → Agent Worker (Python)
                                              → Go Gateway (business logic)
                                              → Cartesia (TTS)
                                              → NestJS (tools)
```

The Go gateway's state machine, tool execution, Redis session store, and config management are production-ready and should not be rewritten. LiveKit adds HD transport without replacing these.

## Non-Recommendation

Do NOT migrate the entire orchestration layer to Python LiveKit Agents. The existing Go code represents significant investment and is working. LiveKit should be the transport layer, not the business logic layer.
