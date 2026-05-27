# Session Resume — 2026-05-27

## Quick Resume (3 steps)

### 1. Start Services
```powershell
# Redis (auto-start, check: redis-cli ping)

# Backend
cd C:\Builds\AI-Phone-Answer-System\backend
node_modules\.bin\tsc.cmd
$env:NESTJS_PORT="3001"; $env:HMAC_SECRET="dev-hmac-secret"; $env:GATEWAY_WS_URL="wss://kemberly-diastolic-subopaquely.ngrok-free.dev/stream"
Start-Process node -ArgumentList "dist/main.js" -WindowStyle Hidden

# Gateway (Deepgram)
cd C:\Builds\AI-Phone-Answer-System\voice-gateway
go build -o gateway.exe ./cmd/gateway
$env:HMAC_SECRET="dev-hmac-secret"; $env:REDIS_ADDR="localhost:6379"; $env:NESTJS_URL="http://localhost:3001"
$env:VOICE_RUNTIME="deepgram_agent"
$env:DEEPGRAM_API_KEY="5171787eeeb6b6353b3bc5eeab1adcbae5d83977"
$env:DEEPGRAM_LISTEN_MODEL="flux-general-en"
Start-Process gateway.exe -WindowStyle Hidden

# ngrok
ngrok http 8080
```

### 2. Verify
```powershell
curl http://localhost:8080/health
curl -H "ngrok-skip-browser-warning: 1" https://kemberly-diastolic-subopaquely.ngrok-free.dev/health
```

### 3. Call: `+441789336134`

---

## Current Status

| Component | Provider | State |
|-----------|----------|-------|
| Telephony | Twilio | ✅ |
| Runtime | Deepgram Agent | ✅ aura-2-pandora-en |
| Custom backup | OpenAI gpt-realtime-1.5 | ✅ marin voice |
| Cartesia | Scaffold | 🔶 Needs key |
| ElevenLabs | Scaffold | 🔶 |
| Grok | Scaffold | 🔶 |

**Deepgram works end-to-end.** Voice slightly mechanical but functional.

**Switch to OpenAI**: `$env:VOICE_RUNTIME="custom"; $env:OPENAI_API_KEY="sk-proj-..."`

---

## Keys (gitignored .env)
- Deepgram: `5171787eeeb6b6353b3bc5eeab1adcbae5d83977`
- OpenAI: `sk-proj-SU3j...`
- Twilio: in .env
- Telnyx: in .env

## ngrok
`https://kemberly-diastolic-subopaquely.ngrok-free.dev` → `localhost:8080`
