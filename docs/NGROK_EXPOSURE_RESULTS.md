# VoxLane — ngrok Exposure Results

**Date**: 2026-05-23  
**Status**: BLOCKED — ngrok authtoken required  

---

## ngrok Installation

| Item | Status |
|------|--------|
| ngrok CLI | ✅ Installed v3.39.4 via scoop |
| Authtoken | ❌ Not configured |

## Pre-Exposure Validation

Local services verified before attempting public exposure:

| Service | Port | Status |
|---------|------|--------|
| Redis | 6379 | ✅ Running |
| Go Gateway | 8080 | ✅ Health: `{"status":"ok"}` |
| NestJS Backend | 3001 | ✅ Webhook returns TwiML |

## Blocked Step

ngrok requires a verified account and authtoken:

```
$ ngrok http 8080
ERROR: authentication failed
ERR_NGROK_4018
```

## Resolution Steps (when authtoken available)

```powershell
# 1. Configure authtoken
ngrok config add-authtoken <YOUR_NGROK_AUTHTOKEN>

# 2. Expose gateway
ngrok http 8080

# 3. Capture public URL from ngrok dashboard or API
$url = (Invoke-RestMethod http://127.0.0.1:4040/api/tunnels).tunnels[0].public_url
# Example: https://xxxx-xx-xx-xxx.ngrok-free.app

# 4. Update .env
# GATEWAY_WS_URL=wss://xxxx-xx-xx-xxx.ngrok-free.app/stream

# 5. Restart NestJS backend
# (so it picks up new GATEWAY_WS_URL)

# 6. Verify public endpoints
# curl https://xxxx.ngrok-free.app/health
# curl -X POST https://xxxx.ngrok-free.app/api/public/voice/webhook
# (validate TwiML contains ngrok WSS URL)

# 7. Configure Twilio phone number webhook
# Voice URL: https://xxxx.ngrok-free.app/api/public/voice/webhook
# Method: POST
```

## Required .env Change (after ngrok URL obtained)

```
GATEWAY_WS_URL=wss://xxxx-xx-xx-xxx.ngrok-free.app/stream
```

## Completed Validation (locally)

All 16 integration tests pass from `LOCAL_INTEGRATION_RESULTS.md`. The system is ready for public exposure — only the ngrok authtoken is needed to proceed to live Twilio testing.

## Remaining Blockers

| # | Blocker | Resolution |
|---|---------|------------|
| 1 | ngrok authtoken | Sign up at dashboard.ngrok.com, get token |
| 2 | Twilio phone number | Awaiting approval |
| 3 | Live call test | Depends on 1 + 2 |
