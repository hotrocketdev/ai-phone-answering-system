# xAI Phone Worker (production)

**Status:** Pre-ResDiary-API scaffold complete. ResDiary + Depos + manager-queue dispatcher is fully wired with in-process fakes. Real-adapter skeletons (env-driven, no live calls) are in place. Telnyx I/O scaffold exercises the full audio path with file-based simulation.

## Files

- `src/xai-client.js` — xAI WSS protocol layer (copied from the spike, refactored)
- `src/dispatcher.js` — orchestrates `availability -> hold -> book -> compensate-on-failure`
- `src/providers.ts` — frozen TypeScript-style provider contracts (with JSDoc typedefs in the JS files)
- `src/providers/index.js` — provider factory (fakes by default, real adapters when `USE_REAL_PROVIDERS=1`)
- `src/providers/fake/resdiary-fake.js` — in-process ResDiary fake
- `src/providers/fake/depos-fake.js` — in-process Depos fake
- `src/providers/fake/manager-queue-fake.js` — in-process manager queue fake
- `src/providers/real/resdiary-adapter.js` — SKELETON (awaiting API access)
- `src/providers/real/depos-adapter.js` — SKELETON (awaiting API access)
- `src/providers/real/manager-queue-adapter.js` — SKELETON (awaiting endpoint)
- `src/telnyx-io.js` — Telnyx media stream I/O scaffold (file-based, swap to WSS for live)
- `src/pcmu-codec.js` — G.711 µ-law encode/decode + 24k↔8k resample (pure JS, no deps)
- `src/log.js` — minimal METRIC logger
- `src/index.js` — production worker entry point (Telnyx I/O stubbed — TODO)
- `src/contract.test.js` — 15/15 contract tests for the full booking flow
- `src/telnyx-io.test.js` — 5/5 tests for the Telnyx I/O scaffold
- `docs/../../livekit-hd-spike/XAI_PHONE_PATH_PLAN.md` — production path plan
- `docs/../../livekit-hd-spike/DISPATCHER_PROVIDER_CONTRACTS.md` — provider contracts

## Run the test suite

```bash
cd experimental/livekit/xai-phone-worker
npm install
npm test              # 20 tests total: 15 contract + 5 telnyx-io
```

## Run against real APIs (after credentials arrive)

```bash
# .env (NOT committed)
USE_REAL_PROVIDERS=1
XAI_API_KEY=...
RESDIARY_API_KEY=...
RESDIARY_VENUE_ID=...
DEPOS_API_KEY=...
MANAGER_QUEUE_URL=...
MANAGER_QUEUE_KEY=...

npm test              # same 20 tests, now against the real APIs
```

The dispatcher doesn't change. The factory in `src/providers/index.js` is the only file that switches.

## What's blocked on real API access

- ResDiary: `RESDIARY_API_KEY` + `RESDIARY_VENUE_ID` (expected 2026-06-08)
- Depos: `DEPOS_API_KEY`
- Manager queue: `MANAGER_QUEUE_URL` + `MANAGER_QUEUE_KEY`

When each arrives, fill in the field-mapping TODOs in the corresponding `src/providers/real/*.js`, flip `USE_REAL_PROVIDERS=1`, run the contract tests. The tests will surface any field-name mismatches.

## What's ready for Telnyx live I/O later

The `src/telnyx-io.js` scaffold is file-based but has the exact interface a live Telnyx WSS client would have. To go live, swap `TelnyxMediaSource` / `TelnyxMediaSink` for WSS clients; the rest of the pipeline is unchanged. Inter-packet timing instrumentation (per the manager's directive) is already there.

## Production untouched

All work on `feat/livekit-hd-spike` in `experimental/livekit/`. No production .env, systemd, webhook, gateway, or main-branch changes.
