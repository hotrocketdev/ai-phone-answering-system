| Stage                       | Result | Evidence |
| --------------------------- | ------ | -------- |
| Twilio webhook POST         | Failed before fix | Latest failed Call SID `CAa82ecf22871bf9b093b276786e931504`; Twilio notification `NOb128efd8a74d32702281a363c3318c4a`; error `11200`; Twilio reported HTTP 404 for `https://kemberly-diastolic-subopaquely.ngrok-free.dev/api/public/voice/webhook`. |
| Gateway received webhook    | No for failed call | Public endpoint was offline at ngrok before fix, so request did not reach local gateway. |
| Gateway proxied to backend  | Yes after fix | `POST http://localhost:8080/api/public/voice/webhook` returned 200 TwiML via backend proxy. |
| Backend returned TwiML      | Yes | `POST http://localhost:3001/api/public/voice/webhook` returned 200 with `<Response>`. |
| TwiML has correct WSS URL   | Yes | Local backend and gateway-proxy webhook responses include `wss://kemberly-diastolic-subopaquely.ngrok-free.dev/stream/{CallSid}`. |
| WebSocket opened            | Not yet proven for new call | Latest failed call stopped at webhook HTTP 404 before TwiML/stream. |
| Twilio start event received | Not yet proven for new call | Requires next live call after public webhook fix. |
| streamSid captured          | Not yet proven for new call | Requires next live call after public webhook fix. |
| Runtime selected            | Gateway ready | Gateway startup logs show `Runtime: custom`. |
| Renderer selected           | Not evaluated | Current task stopped before AI/Cartesia stream validation. |
| First media frame sent      | Not yet proven for new call | Requires next live call after public webhook fix. |
| Twilio stop/error           | Known previous error | Twilio debugger/notification error `11200`: public webhook HTTP 404. |
| Gateway/backend crash       | No evidence for latest failure | Backend and gateway are running; latest failure boundary was public ngrok endpoint offline. |

Current public path status after fix:

- ngrok tunnel: `https://kemberly-diastolic-subopaquely.ngrok-free.dev` -> `http://localhost:8080`
- public webhook POST: 200
- public webhook body: TwiML with WSS stream URL

Stream validation pre-call check:

| Check | Result | Evidence |
| ----- | ------ | -------- |
| Local gateway health | Pass | `GET http://localhost:8080/health` returned 200. |
| Local backend webhook | Pass | `POST http://localhost:3001/api/public/voice/webhook` returned 200 TwiML with WSS stream URL. |
| Public ngrok webhook | Pass | `POST https://kemberly-diastolic-subopaquely.ngrok-free.dev/api/public/voice/webhook` returned 200 TwiML with WSS stream URL. |
| Required Cartesia runtime env | Blocked | `CARTESIA_API_KEY`, `CARTESIA_VOICE_ID`, and `VOICE_RENDERER` are not available in the current local environment; previous Cartesia-mode gateway starts failed at config validation with `VOICE_RENDERER=cartesia requires CARTESIA_API_KEY`. |
| Controlled validation call | Not run | A live call now could prove webhook and WebSocket reachability, but cannot prove `VOICE_RENDERER=cartesia` or caller-heard-Cartesia until the required Cartesia environment is present. |
