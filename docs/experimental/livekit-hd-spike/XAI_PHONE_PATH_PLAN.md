# xAI Phone Path — Plan

**Status:** Pre-ResDiary-API scaffold complete. All work on `feat/livekit-hd-spike` in `experimental/livekit/xai-phone-worker/`. Production untouched.

**Follow-up:** the user approved building the multi-vendor spike on 2026-06-10 to A/B test Deepgram STT + xAI Grok LLM + ElevenLabs TTS against this xAI single-vendor baseline. See `experimental/livekit/multi-vendor-spike/` and `docs/experimental/livekit-hd-spike/DECISION_REPORT_MULTI_VENDOR.md`. The xai-phone-worker remains the production-deployment target; the multi-vendor spike is parallel work to find the cheapest path to < 1.0s first-audio.

## What's in the locked stack

| Layer | Tech | Status |
|---|---|---|
| Telephony | Telnyx (existing production) | Untouched. Will request media stream when worker goes live. |
| Audio codec | opus 24 kHz (production) or L16 16 kHz fallback | Codec change in production gateway is **deferred** until the worker goes live. |
| Real-time transport | xAI Voice Agent WSS (`wss://api.x.ai/v1/realtime`) | **Validated end-to-end** in the spike (r1-r-real). |
| STT + LLM + TTS + VAD | xAI Voice Agent (grok-voice-latest + Eve British) | **Validated**. |
| Function-call bridge | Custom WSS client (Node.js) | **Validated end-to-end** in the spike. |
| Booking system | ResDiary | **Adapter skeleton built.** Real API integration awaits `RESDIARY_API_KEY` + `RESDIARY_VENUE_ID` (expected 2026-06-08). |
| Deposit / payment | Depos | **Adapter skeleton built.** Real API integration awaits `DEPOS_API_KEY`. |
| Manager escalation | Custom callback queue | **Adapter skeleton built.** Real endpoint integration awaits `MANAGER_QUEUE_URL` + `MANAGER_QUEUE_KEY`. |
| Telephony I/O | Telnyx media stream (in scaffold) | **Scaffold complete.** Real integration awaits live call test. |

## What was completed with fakes (this iteration)

1. **Provider contracts** (`src/providers.ts`): 4 frozen TypeScript-style interfaces (`AvailabilityProvider`, `BookingProvider`, `DepositProvider`, `ManagerQueueProvider`) with `Result<T, ProviderError>` types and idempotency-key parameters.

2. **Fake providers** (`src/providers/fake/`):
   - `resdiary-fake.js` — supports available/unavailable slots, alternative suggestions, invalid party size / date / time, provider timeout, provider error, idempotency replay.
   - `depos-fake.js` — supports successful hold, declined card, timeout, idempotency, configurable pence-per-cover.
   - `manager-queue-fake.js` — supports accepted message, queue failure, missing-caller-phone warning.

3. **Provider factory** (`src/providers/index.js`): `getProviders()` returns fakes by default; `USE_REAL_PROVIDERS=1` switches to the real adapters (when env is set).

4. **Real-adapter skeletons** (`src/providers/real/`):
   - `resdiary-adapter.js` — env-driven (`RESDIARY_API_KEY`, `RESDIARY_BASE_URL`, `RESDIARY_VENUE_ID`); HTTP plumbing in place; field-mapping functions are placeholder TODOs (await real schema confirmation).
   - `depos-adapter.js` — env-driven (`DEPOS_API_KEY`, `DEPOS_BASE_URL`, `DEPOSIT_PENCE_PER_COVER`); HTTP plumbing in place; `hold` + `compensate` endpoints with TODO field mappings.
   - `manager-queue-adapter.js` — env-driven (`MANAGER_QUEUE_URL`, `MANAGER_QUEUE_KEY`); HTTP plumbing in place; `send` endpoint with TODO field mapping.

5. **Dispatcher** (`src/dispatcher.js`): orchestrates the flow `availability -> hold -> book -> compensate-on-failure`. Idempotency key required on every call. Provider-agnostic (uses the factory).

6. **Contract tests** (`src/contract.test.js`): **15/15 pass.** Covers the 10 user scenarios plus 5 robustness tests (timeout, invalid input, idempotency, missing phone, error retry).

7. **Telnyx I/O scaffold** (`src/telnyx-io.js`): file-based simulation of Telnyx media stream in/out. **5/5 tests pass.** Records inbound gap, decode/resample/append time, outbound frame count, pacing, backpressure events.

## What waits for real credentials

| Blocked on | Resumes when |
|---|---|
| `RESDIARY_API_KEY` + `RESDIARY_VENUE_ID` | ResDiary API access arrives (expected 2026-06-08) — fill in the field-mapping TODOs in `src/providers/real/resdiary-adapter.js`, run the contract tests against the real API, listen for any field-name mismatches. |
| `DEPOS_API_KEY` | When the user has it — same as above for `src/providers/real/depos-adapter.js`. |
| `MANAGER_QUEUE_URL` + `MANAGER_QUEUE_KEY` | When the user has it — same as above for `src/providers/real/manager-queue-adapter.js`. |

Each is a 1-2 hour task once the key arrives: set env, run `USE_REAL_PROVIDERS=1 npm test`, fix any field-name mismatches the test surfaces, commit.

## What is ready for Telnyx live I/O later

The `src/telnyx-io.js` scaffold has the file-based simulation working. To go live:

1. **Replace `TelnyxMediaSource`** with a Telnyx WSS client that consumes media frames from a Telnyx Programmable Voice call control webhook. The contract (`on('frame', { pcm16_8k, pcm16_24k, gap_ms, decode_ms, resample_ms })`) stays the same.
2. **Replace `TelnyxMediaSink`** with a Telnyx WSS client that writes back to the same call. The contract (`write(pcm16_24k)`, `flush()`) stays the same.
3. **Re-run the metric summary on the first live call** — compare against the file-based baseline. Anything that looks weird (high p95 gap_ms, dropped frames, backpressure) is a real issue.
4. **Instrument inter-packet timing on both directions from the first call** — per the manager's directive ("the thing to instrument from the first live call is inter-packet timing on both directions — same jitter-delta logging discipline that would've diagnosed the original OGG burst in one run").

The scaffold already records all of this. Live wiring is "swap the file for a socket."

## Production untouched

- No production PCMU runtime changes
- No production .env changes
- No production systemd changes
- No Telnyx production webhook changes
- No production gateway changes
- No merges to main
- No secrets, env files, tokens, binaries, WAVs, debug audio committed
- Plan A fallback preserved
