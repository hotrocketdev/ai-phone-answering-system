# Session State — 2026-05-22 / 2026-05-23

**Created**: 2026-05-23 17:00 BST  
**Purpose**: Resume point for next session after restart  

---

## What We Built

VoxLane AI Phone Answering System — realtime voice PoC for restaurant phone calls.

### Architecture

```
Twilio PSTN → ngrok → Go Gateway (:8080) → OpenAI Realtime (gpt-realtime-mini, shimmer voice)
                    → Go Gateway proxy → NestJS (:3001) → fake booking tools
                    ← Redis (:6379) session state
```

### Repository

```
https://github.com/hotrocketdev/ai-phone-answering-system
Branch: main
Commits: 17 (637761c → 6fc9890)
```

### Implementation Status

| Phase | Complete | Pending |
|-------|----------|---------|
| PHASE 1 — Core Call Pipeline | All 16 tasks | — |
| PHASE 2 — Conversation Engine | All 8 tasks | — |
| PHASE 3 — Tooling + Bookings | 3/10 | Go→NestJS wired; ResDiary, SMS, BullMQ not started |
| PHASE 4 — Resilience | 1/12 | Reconnect, circuit breaker, CDR, cost tracking not started |
| PHASE 5 — Cost Optimisation | 0/5 | Hybrid AI architecture not implemented |
| PHASE 6 — Live Testing | 5/11 | **ngrok validated, ready for first call** |
| PHASE 7 — MVP Release | 0/10 | Frontend not started |

### Key Files

| File | Purpose |
|------|---------|
| `voice-gateway/cmd/gateway/main.go` | Entry point, routes, proxy, Redis, graceful shutdown |
| `voice-gateway/internal/session/session.go` | Session lifecycle, Twilio→OpenAI orchestration, tool execution |
| `voice-gateway/internal/session/sm/state_machine.go` | 10-state conversation machine, guardrails, prompts |
| `voice-gateway/internal/audio/mulaw.go` | G.711 u-law codec (exhaustive reverse mapping) |
| `voice-gateway/internal/audio/resampler.go` | Polyphase FIR 8kHz↔24kHz resampler |
| `voice-gateway/internal/audio/pipeline.go` | Full audio chain: u-law↔PCM16↔resample↔base64 |
| `voice-gateway/internal/twilio/handler.go` | Twilio Media Streams WS handler |
| `voice-gateway/internal/openai/client.go` | OpenAI Realtime WS client, session config, VAD |
| `voice-gateway/internal/redis/session_store.go` | Redis session persistence, atomic Lua ops |
| `backend/src/modules/tools/tools.controller.ts` | 6 fake tool endpoints (HMAC-guarded) |
| `backend/src/modules/voice/voice.controller.ts` | Twilio webhook — returns TwiML |
| `backend/src/common/guards/hmac.guard.ts` | HMAC-SHA256 guard |
| `docker-compose.yml` | Redis, Go gateway, NestJS backend |

### Tests

55 Go tests passing across 7 packages (audio, config, openai, redis, session/sm, twilio).  
16 integration tests passing (10 HTTP, 3 HMAC security, 2 WebSocket, 1 TwiML).

---

## How to Resume — Step by Step

### 1. Start Redis
Already running as Windows service on port 6379. If not:
```powershell
redis-server
```

### 2. Start NestJS Backend
```powershell
cd C:\Builds\AI-Phone-Answer-System\backend
node_modules\.bin\tsc.cmd
$env:NESTJS_PORT="3001"
$env:HMAC_SECRET="dev-hmac-secret"
$env:GATEWAY_WS_URL="wss://kemberly-diastolic-subopaquely.ngrok-free.dev/stream"
node dist/main.js
```

### 3. Start Go Gateway
```powershell
cd C:\Builds\AI-Phone-Answer-System\voice-gateway
$env:OPENAI_API_KEY="sk-proj-..."
$env:HMAC_SECRET="dev-hmac-secret"
$env:REDIS_ADDR="localhost:6379"
$env:NESTJS_URL="http://localhost:3001"
go run ./cmd/gateway
```

### 4. Start ngrok
```powershell
ngrok http 8080
# URL: https://kemberly-diastolic-subopaquely.ngrok-free.dev
```

### 5. Verify
```powershell
# Health
curl http://localhost:8080/health

# Webhook (local)
curl -X POST http://localhost:3001/api/public/voice/webhook

# Via ngrok
curl -H "ngrok-skip-browser-warning: 1" https://kemberly-diastolic-subopaquely.ngrok-free.dev/health
```

---

## Environment Variables (.env)

Located at `C:\Builds\AI-Phone-Answer-System\.env` (gitignored).

| Variable | Value |
|----------|-------|
| `OPENAI_API_KEY` | (in .env) — Realtime API access confirmed |
| `OPENAI_REALTIME_MODEL` | gpt-realtime-mini |
| `TWILIO_ACCOUNT_SID` | (in .env) |
| `TWILIO_AUTH_TOKEN` | (in .env) |
| `REDIS_ADDR` | localhost:6379 |
| `GATEWAY_WS_URL` | wss://kemberly-diastolic-subopaquely.ngrok-free.dev/stream |
| `HMAC_SECRET` | dev-hmac-secret (dev only) |
| `NESTJS_URL` | http://localhost:3001 (only used by Go gateway) |

---

## ngrok Details

| Item | Value |
|------|-------|
| CLI version | 3.39.4 (installed via scoop) |
| Public URL | https://kemberly-diastolic-subopaquely.ngrok-free.dev |
| WSS URL | wss://kemberly-diastolic-subopaquely.ngrok-free.dev/stream |
| ngrok-skip-browser-warning header | Required for API calls (free tier) |
| Note | Port 8080 only (Go gateway proxies webhooks to NestJS) |

---

## Twilio Phone Number

| Field | Value |
|-------|-------|
| Account SID | (in .env) |
| Auth Token | (in .env) |
| Phone number | Not yet configured |
| Webhook URL to set | https://kemberly-diastolic-subopaquely.ngrok-free.dev/api/public/voice/webhook |
| Method | POST |

---

## Remaining Tasks (Next Session)

### Immediate — P6.7 + P6.8

1. **P6.7**: Configure Twilio phone number voice webhook
   - Set to `https://kemberly-diastolic-subopaquely.ngrok-free.dev/api/public/voice/webhook`
   - Method: POST

2. **P6.8**: Make first live phone call
   - Call the Twilio number from a real phone
   - Verify: greeting plays, AI responds, booking tools work, barge-in works
   - Expected first words: "Bella Roma, this is Alex. How can I help?"

### After First Call

3. **P6.9**: Voice quality assessment
4. **P6.10**: Latency measurement
5. **P6.11**: Barge-in validation

### Phase 4 backlog (not urgent)
- OpenAI reconnection
- Circuit breakers
- CDR recording
- Cost tracking

---

## Known Issues

1. **NestJS build**: Use `node_modules\.bin\tsc.cmd` directly, not `nest build` (webpack path issue with shared types)
2. **Port 3000**: Occupied by Redeemio Next.js app — NestJS uses port 3001
3. **ngrok free tier**: Only one tunnel; Go gateway proxies webhooks to NestJS to work around this
4. **Docker unavailable**: Docker Desktop Linux engine not running; services started natively
5. **OpenAI model**: Using `gpt-realtime-mini` (not the deprecated `gpt-4o-realtime-preview`)

---

## Canonic Documents

| Document | Purpose |
|----------|---------|
| `docs/PROJECT_STATUS.md` | Full audit comparing blueprint vs implementation |
| `docs/MVP_TASKLIST.md` | Master task list — single source of truth |
| `docs/LOCAL_INTEGRATION_RESULTS.md` | 16/16 local integration tests |
| `docs/NGROK_EXPOSURE_RESULTS.md` | ngrok exposure validation results |
| `LIVE_CALL_TESTING.md` | First call checklist |
| `VoxLane_Implementation_Blueprint.md` | Original implementation blueprint |
| `voxlane_architecture_critique.md` | Architecture review with cost analysis |
