# Session State — 2026-05-22 / 2026-05-23

**Updated**: 2026-05-23 23:00 BST  
**Purpose**: Resume point for next session after restart  

---

## What We Built

VoxLane AI Phone Answering System — realtime voice PoC for restaurant phone calls.

### Architecture

```
Twilio PSTN or Telnyx → ngrok → Go Gateway (:8080) → OpenAI Realtime (gpt-realtime-mini, shimmer voice)
                                 → Go Gateway proxy → NestJS (:3001) → fake booking tools
                                 ← Redis (:6379) session state
```

### Repository

```
https://github.com/hotrocketdev/ai-phone-answering-system
Branch: main
Commits: 20 (637761c → dab1d96)
```

---

## Last Commits (this session)

| SHA | Description |
|-----|-------------|
| `dab1d96` | Provider-agnostic voice layer (Twilio ✅, Telnyx 🔶, SignalWire ⬜) |
| `7479540` | VPS deployment pre-check note |
| `60a6b3d` | Live call test script + results template |
| `6fc9890` | ngrok exposure validated |
| `4e5ccd0` | ngrok installed, blocked on authtoken (resolved) |
| `fc6c41f` | Local integration test 16/16 pass |
| `8c82fd3` | Redis wired |
| `73e8c4a` | Go→NestJS tool calls |
| `c417eff` | docker-compose fix |

---

## Telnyx Setup (NEW — not yet live)

| Field | Value |
|-------|-------|
| API Key | (in .env) |
| Phone Number | +441218230230 (0121 Birmingham, $1/mo) |
| TeXML App ID | 2966595325193618449 |
| App Name | VoxLane |
| Voice Webhook URL | https://kemberly-diastolic-subopaquely.ngrok-free.dev/api/public/voice/webhook/telnyx |
| Number Status | ⚠️ requirement-info-pending — needs regulatory info in Portal |

### Portal Action Required
1. Go to https://portal.telnyx.com
2. Find number +441218230230
3. Complete regulatory requirements (business name, address, use case)
4. Once approved → number goes active → can make calls

---

## Provider Abstraction (NEW)

The voice layer is now provider-agnostic. Located in `voice-gateway/internal/provider/`:

| Package | Status |
|---------|--------|
| `internal/provider/provider.go` | Interface + neutral types (AudioFrame, Event, Adapter) |
| `internal/provider/twilio/adapter.go` | ✅ Production — wraps WebSocket with Frames/Events channels |
| `internal/provider/telnyx/adapter.go` | 🔶 Scaffold — implements interface, ParseMediaEvent not done |
| `internal/provider/signalwire/adapter.go` | ⬜ Placeholder — returns "not implemented" |

Provider selection via `VOICE_PROVIDER` env var (twilio/telnyx/signalwire).

NestJS webhook endpoints:
- `POST /api/public/voice/webhook` — Twilio (XML TwiML)
- `POST /api/public/voice/webhook/telnyx` — Telnyx (JSON)
- `POST /api/public/voice/webhook/signalwire` — 501

---

## ngrok Details

| Item | Value |
|------|-------|
| CLI version | 3.39.4 (installed via scoop) |
| Public URL | https://kemberly-diastolic-subopaquely.ngrok-free.dev |
| WSS URL | wss://kemberly-diastolic-subopaquely.ngrok-free.dev/stream |
| ngrok-skip-browser-warning header | Required for API calls (free tier) |
| Port exposed | 8080 (Go gateway proxies webhooks to NestJS :3001) |

---

## Environment Variables (.env)

Located at `C:\Builds\AI-Phone-Answer-System\.env` (gitignored).

| Variable | Value |
|----------|-------|
| `OPENAI_API_KEY` | (in .env) — Realtime API access confirmed |
| `OPENAI_REALTIME_MODEL` | gpt-realtime-mini |
| `VOICE_PROVIDER` | twilio (switch to telnyx when ready) |
| `TWILIO_ACCOUNT_SID` | (in .env) |
| `TWILIO_AUTH_TOKEN` | (in .env) |
| `TELNYX_API_KEY` | (in .env) |
| `TELNYX_CONNECTION_ID` | 2966595325193618449 |
| `REDIS_ADDR` | localhost:6379 |
| `GATEWAY_WS_URL` | wss://kemberly-diastolic-subopaquely.ngrok-free.dev/stream |
| `HMAC_SECRET` | dev-hmac-secret (dev only) |
| `NESTJS_URL` | http://localhost:3001 |

---

## How to Resume — Step by Step

### 1. Start Redis
Running as Windows service on port 6379. If not: `redis-server`

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
$env:OPENAI_API_KEY="(from .env)"
$env:HMAC_SECRET="dev-hmac-secret"
$env:REDIS_ADDR="localhost:6379"
$env:NESTJS_URL="http://localhost:3001"
$env:VOICE_PROVIDER="twilio"
go run ./cmd/gateway
```

### 4. Start ngrok
```powershell
ngrok http 8080
# If URL changes, update GATEWAY_WS_URL in .env and restart backend
```

### 5. Verify
```powershell
curl http://localhost:8080/health
curl -H "ngrok-skip-browser-warning: 1" https://kemberly-diastolic-subopaquely.ngrok-free.dev/health
```

---

## Tests

55 Go tests passing (audio, config, openai, redis, session/sm, twilio).  
16 integration tests passing.  
NestJS clean build.

---

## Known Issues

1. **NestJS build**: Use `node_modules\.bin\tsc.cmd` directly, not `nest build`
2. **Port 3000**: Occupied by Redeemio — NestJS uses port 3001
3. **ngrok free tier**: Single tunnel; Go gateway proxies webhooks to NestJS
4. **Docker unavailable**: Services run natively
5. **Telnyx adapter**: ParseMediaEvent not yet implemented — needs real-call format capture
6. **Number pending**: +441218230230 needs regulatory approval in Telnyx Portal

---

## Canonical Documents

| Document | Purpose |
|----------|---------|
| `docs/PROJECT_STATUS.md` | Full audit comparing blueprint vs implementation |
| `docs/MVP_TASKLIST.md` | Master task list — single source of truth |
| `docs/LOCAL_INTEGRATION_RESULTS.md` | 16/16 local integration tests |
| `docs/NGROK_EXPOSURE_RESULTS.md` | ngrok exposure validation |
| `docs/VOICE_PROVIDER_ABSTRACTION.md` | Provider abstraction architecture |
| `docs/FIRST_LIVE_CALL_RESULTS.md` | Template — ready to fill after first call |
| `LIVE_CALL_TESTING.md` | First call checklist |
| `VoxLane_Implementation_Blueprint.md` | Original implementation blueprint |
| `voxlane_architecture_critique.md` | Architecture review with cost analysis |

---

## Next Tasks (Next Session)

### Immediate — First Live Call

1. **Telnyx regulatory**: Complete number requirements in Portal
2. **Implement Telnyx adapter ParseMediaEvent/EncodeAudio**: Once number is active, capture WebSocket format from a test call
3. **Switch VOICE_PROVIDER=telnyx**: Update .env, restart services
4. **First live call**: Call +441218230230, follow test script in FIRST_LIVE_CALL_RESULTS.md

### Or — Twilio Path (if Twilio number gets approved first)

1. Configure Twilio number webhook to ngrok URL
2. Call the Twilio number
3. Fill in FIRST_LIVE_CALL_RESULTS.md

### Phase 4 backlog (not urgent)
- OpenAI reconnection
- Circuit breakers
- CDR recording
- Cost tracking
- VPS deployment (after ports.txt review)
