# VoxLane — First Live Call Checklist

## Pre-Flight

- [ ] OpenAI API key in `.env` (`OPENAI_API_KEY`)
- [ ] Twilio Account SID + Auth Token in `.env` (`TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`)
- [ ] Twilio phone number purchased and active
- [ ] ngrok installed (`ngrok version`)

## Step 1 — Start Backend

```bash
docker compose up redis backend
```

Verify: `curl http://localhost:3000/api/public/voice/webhook` should return XML.

## Step 2 — Start Go Gateway

```bash
cd voice-gateway && go run ./cmd/gateway
```

Verify: `curl http://localhost:8080/health` → `{"status":"ok"}`

## Step 3 — Expose to Internet

```bash
ngrok http 8080
```

Copy the `https://xxxx.ngrok-free.app` URL.

## Step 4 — Update Environment

Set `GATEWAY_WS_URL` in `.env` to the ngrok WebSocket URL:

```
GATEWAY_WS_URL=wss://xxxx.ngrok-free.app/stream
```

Restart the backend after changing `.env`:
```bash
docker compose restart backend
```

## Step 5 — Point Twilio at Backend

In Twilio Console → Phone Numbers → your number → Voice Configuration:

- **Configure with**: Webhook
- **A call comes in**: Webhook — `POST https://xxxx.ngrok-free.app/api/public/voice/webhook`
- **HTTP method**: POST

Or via Twilio CLI:
```bash
twilio phone-numbers update $TWILIO_PHONE_NUMBER \
  --voice-url https://xxxx.ngrok-free.app/api/public/voice/webhook \
  --voice-method POST
```

## Step 6 — Test Call

Call your Twilio number from a real phone.

Expected flow:
1. Twilio receives call → POST to NestJS `/api/public/voice/webhook`
2. NestJS returns TwiML with `<Connect><Stream>` pointing to Go gateway
3. Twilio opens WebSocket to `wss://xxxx.ngrok-free.app/stream/{CallSid}`
4. Go gateway accepts, opens OpenAI Realtime session
5. AI answers with restaurant greeting (shimmer voice)
6. You speak — AI responds naturally
7. AI handles barge-in (interrupt mid-sentence)
8. AI calls fake booking tools
9. Call ends gracefully

## What to Listen For

| Quality Check | Good | Bad |
|--------------|------|-----|
| Greeting tone | Warm, natural | Robotic, scripted |
| Response speed | <1s from end of your speech | >2s dead air |
| Filler phrases | "Let me check" | "Certainly! I'd be happy to assist" |
| Interruption | AI stops immediately | AI keeps talking over you |
| Booking flow | Natural question progression | Lists all fields at once |
| Voice quality | Clear, natural | Tinny, metallic, distorted |
| Sentence length | Short, conversational | Long, formal |

## Troubleshooting

| Symptom | Check |
|---------|-------|
| Call connects but silence | Check Go gateway logs for OpenAI connection errors |
| Twilio says "application error" | Check NestJS webhook is reachable, returns valid XML |
| No audio | Verify `GATEWAY_WS_URL` is `wss://` not `ws://` |
| AI doesn't respond | Check `OPENAI_API_KEY` has Realtime API access |
| Metallic voice | Check if polyphase resampler is used (not linear) |
| "We're experiencing technical difficulties" | Fallback audio — OpenAI circuit breaker opened |

## Expected Logs (Go Gateway)

```
VoxLane Voice Gateway starting on :8080
[CAxxx] Twilio Media Stream connecting...
[CAxxx] session starting
[CAxxx] tool call: check_availability({"date":"..."})
[CAxxx] session ended
```

## Expected Logs (NestJS)

```
VoxLane Backend listening on :3000
[CAxxx] check_availability: date=2026-05-22, time=19:00, partySize=4
```
