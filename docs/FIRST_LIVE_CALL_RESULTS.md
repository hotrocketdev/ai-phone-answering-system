# VoxLane — First Live Phone Call Results

**Date**: 2026-05-23  
**Tester**: [name]  
**Twilio number**: [redacted]  

---

## Pre-Flight Verification

| Check | Status |
|-------|--------|
| Redis running (6379) | ✅ |
| NestJS backend (3001) | ✅ TwiML correct |
| Go gateway (8080) | ✅ |
| ngrok tunnel active | ✅ |
| Webhook URL accessible | ✅ |
| WebSocket endpoint reachable | ✅ |

## Test Script

1. ☐ Call the Twilio number from a real phone
2. ☐ Wait for AI greeting
3. ☐ Say: "Hi, I'd like to book a table for four tomorrow at seven"
4. ☐ Let the AI respond
5. ☐ Interrupt the AI mid-sentence: "Actually make that six people"
6. ☐ Continue the booking flow
7. ☐ Confirm the booking
8. ☐ End the call naturally

## Results

### Call Connected?
- ☐ Yes
- ☐ No

### AI Answered?
- ☐ Yes — greeting heard
- ☐ No — silence / error

### Audio Quality
- ☐ Clear and natural
- ☐ Some artifacts
- ☐ Poor — robotic, distorted

### AI Voice Description
[Describe what the shimmer voice sounded like — warm? natural? robotic?]

### Response Latency
- ☐ Fast (<1s)
- ☐ Acceptable (1-2s)
- ☐ Slow (>2s)
- ☐ Very slow (>4s)

### Barge-In Behaviour
- ☐ Worked — AI stopped immediately
- ☐ Partial — AI stopped but awkward
- ☐ Failed — AI kept talking

### Booking Flow
- ☐ Completed — AI collected details, checked availability, confirmed
- ☐ Partial — [describe]
- ☐ Failed — [describe]

### Tool Call Execution
- ☐ check_availability called successfully
- ☐ create_booking called successfully
- ☐ Booking reference returned
- ☐ Neither executed

### Call Ended Cleanly?
- ☐ Yes — graceful hangup
- ☐ No — crash, error, or dropped

### Errors Observed
[Any errors in gateway logs, backend logs, or call experience]

## Log Summary

### Go Gateway
```
[CAxxx] Twilio Media Stream connecting...
[CAxxx] session starting
[CAxxx] POST http://localhost:3001/api/internal/tools/check-availability
[CAxxx] tool response: ...
[CAxxx] session ended
```

### NestJS Backend
```
[CAxxx] check_availability: date=..., time=..., partySize=...
[CAxxx] create_booking: name=..., date=..., time=..., party=...
```

## Issues Found

| # | Issue | Severity | Fix Required |
|---|-------|----------|--------------|
| 1 | | | |
| 2 | | | |

## Assessment

- ☐ System works end-to-end — ready for next phase
- ☐ Mostly works — minor fixes needed before production
- ☐ Major issues — significant rework required
- ☐ Failed entirely — fundamental problems

## Next Recommended Task

[Based on results, what should be done next]
