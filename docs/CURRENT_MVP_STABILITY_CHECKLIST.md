# Current MVP Stability Checklist

**Date**: 2026-05-28  
**Runtime**: `VOICE_RUNTIME=custom`, `VOICE_RENDERER=cartesia`

---

## Pre-Call Health Checks

Run before any demo call:

- [ ] Gateway health: `curl http://localhost:8080/health` → `{"status":"ok"}`
- [ ] Backend health: `curl -X POST http://localhost:3001/api/public/voice/webhook -d "CallSid=TEST"` → 200
- [ ] Redis running: `redis-cli ping` → PONG
- [ ] ngrok running: check `http://127.0.0.1:4040/api/tunnels` → https URL visible
- [ ] Public webhook: `curl -H "ngrok-skip-browser-warning: 1" -X POST https://kemberly-diastolic-subopaquely.ngrok-free.dev/api/public/voice/webhook -d "CallSid=TEST"` → 200 XML

## Env Verification

- [ ] `VOICE_RUNTIME=custom`
- [ ] `VOICE_RENDERER=cartesia`
- [ ] `CARTESIA_API_KEY` present (not printed)
- [ ] `CARTESIA_VOICE_ID` present
- [ ] `OPENAI_API_KEY` present (not printed)
- [ ] `OPENAI_REALTIME_MODEL=gpt-realtime-1.5`
- [ ] `CARTESIA_MODEL=sonic-3.5`
- [ ] `REDIS_ADDR=localhost:6379`

## Startup Log Verification

Gateway should log on startup:
- [ ] `Runtime: custom`
- [ ] `Voice: marin` (unused, cosmetic)
- [ ] `Model: gpt-realtime-1.5`

Per-call log should show:
- [ ] `voice renderer: cartesia`
- [ ] `OpenAI session.updated response: {"type":"session.updated"...` — no error
- [ ] `cartesia: streaming: voice=... model=sonic-3.5`
- [ ] `cartesia: X chunks sent to Twilio` (X > 0)
- [ ] `outbound media frame sent source=cartesia` (multiple)
- [ ] No `source=openai` frames
- [ ] No `session error:` entries
- [ ] No `panic` entries

## Call Flow Verification

| Stage | Expected | Check |
|-------|----------|-------|
| Webhook | 200 XML | ✅/❌ |
| Stream opens | `Twilio Media Stream connecting...` | ✅/❌ |
| OpenAI session | `session.updated` without error | ✅/❌ |
| Cartesia engages | `voice renderer: cartesia` | ✅/❌ |
| Greeting plays | `cartesia: sending text` | ✅/❌ |
| Cartesia audio | `cartesia: X chunks sent` X>0 | ✅/❌ |
| Caller hears voice | Correct Cartesia voice | ✅/❌ |
| Call ends | `session ended` without error | ✅/❌ |

## Known Failure Modes

| Symptom | Likely Cause |
|---------|-------------|
| Gateway DOWN | Process crashed (rare after crash guards) |
| Backend DOWN | Node process killed (stale processes accumulating) |
| Fallback `<Say>` | OpenAI session rejected, or gateway crash |
| No audio | Cartesia renderer not created (env var missing) |
| American voice | OpenAI audio leaking (cartesiaRender suppression failed) |
| Choppy audio | Frame pacing issue (should be rare with direct send) |
