# Current MVP Limitations

**Date**: 2026-05-28  
**Runtime**: `VOICE_RUNTIME=custom`, `VOICE_RENDERER=cartesia`

---

## Audio Quality

**Limitation**: Cartesia Sonic 3.5 voices are rendered at `pcm_mulaw 8000` to match Twilio Media Streams requirements. This is narrowband telephone quality (300-3400 Hz frequency range) — significantly lower than Cartesia's playground quality (44.1kHz WAV).

**Impact**: The voice sounds compressed and slightly "mechanical" compared to the Cartesia dashboard preview. This is a transport limitation, not a voice/model issue.

**Mitigation**: HD audio via LiveKit+SIP planned for Stage 3 (see `PRODUCTION-ROADMAP-VOICE-QUALITY-FIRST.md`).

## ngrok Dependency

**Limitation**: The public webhook URL depends on ngrok free tier. If ngrok disconnects or the URL changes, calls will fail.

**Impact**: Not suitable for production. ngrok is development-only.

**Mitigation**: Production deployment would use a static domain/SSL (Caddy/nginx on VPS).

## OpenAI Conversation Brain

**Limitation**: OpenAI `gpt-realtime-1.5` handles conversation orchestration. This model requires an active OpenAI API key with Realtime API access.

**Impact**: If the API key expires or Realtime access is revoked, calls will fall back to Twilio `<Say>`.

**Mitigation**: Grok evaluation planned for future cost/performance comparison. API key monitoring needed.

## Cartesia Voice Selection

**Limitation**: Only one Cartesia voice ID is configured per deployment. Multi-tenant voice selection not supported.

**Impact**: All calls use the same voice. Restaurants cannot choose their own voice.

**Mitigation**: Multi-tenant voice config planned for SaaS dashboard phase.

## Redis Not Always Required

**Limitation**: Redis is optional. If not available, sessions run in-memory only.

**Impact**: Session state lost on gateway restart. No call history persistence without Redis.

**Mitigation**: Production deployment must include Redis.

## Frame Pacing

**Limitation**: Cartesia audio frames are sent to Twilio as fast as they arrive (no explicit pacing). A 20ms pacer was tested but degraded quality.

**Impact**: May cause occasional audio timing jitter on poor connections.

**Mitigation**: Direct send works acceptably for MVP. HD audio path will use WebRTC native pacing via LiveKit.

## Single Instance

**Limitation**: The Go gateway runs as a single process. No load balancing across multiple instances.

**Impact**: Gateway crash = all calls drop. Single point of failure.

**Mitigation**: Acceptable for MVP. Production would need multiple instances with Redis session sharing.

## Voice is NOT Final

**Limitation**: The current voice quality is acceptable for demos, bug reports, and investor previews, but is not the final premium receptionist quality the product needs.

**Impact**: Demos are good enough. Production rollout requires HD audio transport.

**Mitigation**: See `NEXTGEN-LIVEKIT-HD-AUDIO-PLAN.md`.
