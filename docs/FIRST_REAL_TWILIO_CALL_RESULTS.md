# First Real Twilio Call Results

## Current Attempt

No new controlled validation call was run in this attempt.

Reason: the public webhook, backend, and gateway are healthy after the ngrok fix, but the local environment does not contain the required Cartesia runtime settings. Specifically, `CARTESIA_API_KEY`, `CARTESIA_VOICE_ID`, and `VOICE_RENDERER` are not available to the current shell/process environment, and prior Cartesia-mode gateway starts failed at config validation with `VOICE_RENDERER=cartesia requires CARTESIA_API_KEY`.

## Pre-Call Checks

| Check | Result | Evidence |
| ----- | ------ | -------- |
| Gateway health | Pass | `GET http://localhost:8080/health` returned 200. |
| Backend webhook | Pass | Local backend webhook returned 200 TwiML with WSS stream URL. |
| Gateway proxy webhook | Pass | Local gateway proxy webhook returned 200 TwiML with WSS stream URL. |
| Public webhook | Pass | Public ngrok webhook returned 200 TwiML with WSS stream URL. |
| ngrok tunnel | Pass | `https://kemberly-diastolic-subopaquely.ngrok-free.dev` forwards to `http://localhost:8080`. |
| Cartesia path selectable | Blocked | Required Cartesia environment is missing locally. |

## Call Evidence

| Item | Result |
| ---- | ------ |
| New Call SID | Not available; no new call requested. |
| Webhook status | 200 in synthetic local/public POST checks. |
| WebSocket opened | Not tested with a new live call. |
| streamSid received | Not tested with a new live call. |
| Cartesia path selected | No; required environment missing. |
| First media frame sent | Not tested with a new live call. |
| Caller heard Cartesia | Not tested with a new live call. |
| Fallback/application-error | Not tested with a new live call; previous application-error boundary is fixed. |

## Next Safe Validation

Set the required Cartesia environment for the gateway process, start exactly one gateway on port 8080 with `VOICE_RUNTIME=custom` and `VOICE_RENDERER=cartesia`, verify health and public webhook again, then run one live call and capture the Call SID.
