# xAI Phone Worker (production)

**Status:** Scaffold. Replaces the spike (`xai-phone-harness`) for
production deployment. Telephony I/O not wired yet — dispatcher
(ResDiary + Depos + manager queue) is implemented but uses
synthetic keys.

## What's here

- `src/xai-client.js` — xAI WSS protocol layer (same as the spike)
- `src/dispatcher.js` — ResDiary + Depos + manager-queue calls (real product integrations)
- `src/dispatcher.test.js` — in-process fake-server unit tests for the dispatcher
- `src/log.js` — minimal METRIC logger
- `src/index.js` — production worker entry point (Telnyx I/O stubbed)

## What's NOT here yet

- **Telnyx media stream source** (`src/telnyx-source.js`) — TODO
- **Telnyx media stream sink** (`src/telnyx-sink.js`) — TODO
- **Production logging redirect** — currently stdout; production needs
  `/var/log/voxlane/xai-worker.log` (or equivalent)
- **Live call instrumentation** — inter-packet timing on both
  directions, per the manager's directive (item 3)

## Run the dispatcher tests

```bash
cd experimental/livekit/xai-phone-worker
npm install
npm run test:dispatcher
```

This starts a fake ResDiary + Depos + manager-queue server on
`127.0.0.1:0`, points the dispatcher at it, and exercises the three
function paths. Use this to validate the dispatcher's shape before
the user provides real credentials.

## Run against real APIs (after credentials arrive)

```bash
# .env (NOT committed)
XAI_API_KEY=...
RESDIARY_API_KEY=...
RESDIARY_BASE_URL=https://api.resdiary.com/v1
DEPOS_API_KEY=...
DEPOS_BASE_URL=https://api.deposits.com/v1
MANAGER_QUEUE_URL=...
MANAGER_QUEUE_KEY=...

npm start
```

## What this needs from the manager to go live

1. **ResDiary API credentials** — confirmed available 2026-06-08
2. **Depos API credentials** — TBD
3. **Manager queue endpoint** — TBD (or use a local stub)
4. **Telnyx media stream integration** — separate workstream, real
   pacing concerns. Per the manager, this is "real work, not a
   connector swap — file has no jitter, no backpressure, no caller
   hangup."
5. **Codec change in production gateway** — `TELNYX_STREAM_CODEC=PCMU`
   → opus 24 kHz. This unlocks the audio quality that the manager
   called "almost human."

## Production untouched

All work on `feat/livekit-hd-spike` in `experimental/livekit/`. No
production .env, systemd, webhook, gateway, or main-branch changes.
