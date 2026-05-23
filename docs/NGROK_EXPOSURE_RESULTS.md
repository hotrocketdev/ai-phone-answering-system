# VoxLane — ngrok Exposure Results

**Date**: 2026-05-23  
**Status**: ✅ PASS — all endpoints validated  

---

## Public URL

| Protocol | URL |
|----------|-----|
| HTTPS | `https://kemberly-diastolic-subopaquely.ngrok-free.dev` |
| WSS | `wss://kemberly-diastolic-subopaquely.ngrok-free.dev/stream` |

## Validation Results

| Endpoint | Method | Result |
|----------|--------|--------|
| `/health` | GET | ✅ `{"status":"ok"}` |
| `/health/ready` | GET | ✅ `{"status":"ready","redis":true,"activeSessions":0}` |
| `/api/public/voice/webhook` | POST | ✅ Returns valid TwiML with correct ngrok WSS URL |
| `/stream/CA_NGROK_TEST` | WSS | ✅ State=Open |

## TwiML Response

```xml
<?xml version="1.0" encoding="UTF-8"?>
<Response>
  <Connect>
    <Stream url="wss://kemberly-diastolic-subopaquely.ngrok-free.dev/stream">
      <Parameter name="callerId" value="{{From}}"/>
    </Stream>
  </Connect>
  <Say>Sorry, we're experiencing technical difficulties.</Say>
</Response>
```

✅ Correct WSS URL embedded  
✅ Correct XML format  
✅ Fallback `<Say>` present

## Architecture Fix

Free tier ngrok allows one tunnel. The Go gateway now proxies Twilio webhook requests to NestJS:

```
Twilio POST /api/public/voice/webhook
  → ngrok (port 8080)
    → Go Gateway proxy
      → NestJS (port 3001)
        → returns TwiML with ngrok WSS URL
```

Added `proxyToBackend()` in `cmd/gateway/main.go`.

## Next Steps for Live Call

1. Configure Twilio phone number voice webhook:
   - URL: `https://kemberly-diastolic-subopaquely.ngrok-free.dev/api/public/voice/webhook`
   - Method: POST
2. Call the Twilio number
3. Verify: greeting plays, AI responds, booking flow works
