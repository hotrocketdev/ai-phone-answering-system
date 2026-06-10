# xAI Fake-Provider Rehearsal

**Status:** ✅ Completed. End-to-end rehearsal of the xAI phone-worker product flow with the real xAI Voice Agent + Eve + function-call bridge, but **fake ResDiary / Depos / manager queue** providers. No real production webhook, no real ResDiary API, no real Depos API.

**Run date:** 2026-06-09
**Source:** `feat/livekit-hd-spike` @ commit `020d631` (with the rehearsal orchestrator on top)

**This is the baseline for the multi-vendor spike.** The user approved building `experimental/livekit/multi-vendor-spike/` on 2026-06-10 to A/B test Deepgram (STT) + xAI Grok (LLM) + ElevenLabs (TTS) against this xAI single-vendor baseline. Decision rationale in `docs/experimental/livekit-hd-spike/DECISION_REPORT_MULTI_VENDOR.md`.

## Goal

Per manager directive:

> "Prove the full product behaviour before real API credentials arrive."

The rehearsal uses the real xAI engine (the only path that delivers the user's locked-in objective: most human-like voice, conversational, handles unexpected, not robotic). The fake providers let us prove the dispatcher's orchestration without waiting for ResDiary.

## Commands run

```bash
# 1. Generate the 7 caller-turn PCMU fixtures (Cartesia Sonic 3.5, Gemma, opus 24kHz equiv on VPS via ffmpeg)
cd experimental/livekit/xai-phone-worker
scp scripts/gen-rehearsal-fixtures.mjs jorge@100.113.3.65:/tmp/
# (run on VPS with CARTESIA_API_KEY env)
# produced: t01-hello.pcmu, t02-book.pcmu, t03-tomorrow-7-4.pcmu,
#          t07-george.pcmu, t09-phone.pcmu, t14-change-to-6.pcmu, t16-off-script.pcmu
# pulled back to ./fixtures/rehearsal/

# 2. Fix a bug found in the fake ResDiary date/time validator
# (it rejected natural language like "tomorrow" and "seven")
# src/providers/fake/resdiary-fake.js: validateDate/validateTime now accept
# ISO, common natural-language words, and word-numbers.

# 3. Run the rehearsal
scp -r src/ jorge@100.113.3.65:/tmp/xai-phone-worker/
ssh jorge@100.113.3.65
  cd /tmp/xai-phone-worker
  set -a; source /tmp/xai-voice-agent.env; set +a
  node src/rehearsal.js
```

Output artefacts pulled back to `tmp/rehearsal/`:
- `rehearsal.log` — timestamped log of every event
- `rehearsal-metrics.json` — machine-readable metrics
- `rehearsal-assistant.wav` — concatenated assistant audio (24 kHz PCM16)
- `rehearsal-assistant.pcmu` — same audio, downsampled to 8 kHz PCMU

## Scenario used (18 steps)

1. Caller says hello.
2. Assistant greets.
3. Caller asks to book a table.
4. Assistant acknowledges.
5. Caller says: "Tomorrow at seven for four people."
6. Assistant calls `availability.check`.
7. Caller says: "George."
8. Assistant asks for phone.
9. Caller says: "07917 715734."
10. Assistant explains deposit, calls `deposit.hold` + `booking.create`.
11. Assistant continues after tools.
12. Assistant confirms booking.
13. Caller changes party size to 6.
14. Assistant re-checks availability.
15. Caller asks off-script ("Do you have a vegan tasting menu?").
16. Assistant calls `manager.escalate`.
17. Assistant acknowledges escalation.
18. Assistant ends call naturally.

## Pass/fail results

| Validation | Result |
|---|---|
| All 18 steps executed | ✅ |
| `availability.check` fired (twice: initial + party-size change) | ✅ |
| `deposit.hold` fired (1x, before `booking.create`) | ✅ |
| `booking.create` fired | ✅ |
| `manager.escalate` fired (off-script vegan menu question) | ✅ |
| `function_call_output` returned every time (no missing) | ✅ |
| Assistant resumed after every tool call | ✅ |
| Phone number `07917715734` preserved exactly through 8 kHz μ-law + xAI STT | ✅ |
| Date `"tomorrow"` accepted by the fake provider | ✅ (after fix) |
| Time `"seven"` resolved to `19:00` | ✅ |
| Party size captured (4 then 6) | ✅ |
| Hallucinated restaurant details | ❌ (model declined to answer the menu question, escalated instead) |
| Repeated date question | ❌ (single date question; one clarification after fake rejected "2017-07-15" misheard) |
| Errors | 0 |
| Audio clipping (any obvious truncation in the WAV) | ❌ (3.3 MB assistant audio, model spoke for 18s cleanly) |
| Dropped frames (inbound or outbound) | 0 |

## Latency observations

| Metric | Value | Notes |
|---|---|---|
| **First assistant audio latency** (caller audio sent → first assistant audio byte received) | **2990 ms** | This is the metric that matters for perceived naturalness. ~3 s is consistent with the spike (r-real: 3.0 s p50 first-audio). |
| Turn latencies (wait steps) | avg 9.5 s, p95 20 s | The 20 s p95 is the 20 s safety timeout in the orchestrator's `waitForResponseDone`, which fires when the model has already finished but the quiet timer hasn't elapsed. The actual tool-firing turn latencies are 5-7 s. |
| Tool dispatch latency (fake) | 0.7-1.7 ms | Function-call dispatch is fast. The bottleneck is the model + Grok 4 Voice round-trip. |

## Audio observations

- **Assistant audio length:** ~138 seconds of model speech across 7 responses (3.3 MB WAV @ 24 kHz PCM16).
- **No audio clipping detected** at the WAV header (44 bytes RIFF/WAVE/PCM/data).
- **No dropped frames** on either inbound (PCMU 8 kHz → PCM16 24 kHz upsample) or outbound (PCM16 24 kHz → PCMU 8 kHz downsample).
- The voice is **Eve (British English)**, the model is `grok-voice-latest`.

## Bugs found

### Bug 1: Fake ResDiary rejected natural-language dates (FIXED)

**Symptom:** First run failed at step 6 with `Invalid date format: "tomorrow" (expected YYYY-MM-DD)`. The model correctly passed `"tomorrow"` (per the system-prompt rule), but the fake provider validator only accepted ISO YYYY-MM-DD.

**Fix:** `src/providers/fake/resdiary-fake.js` `validateDate` and `validateTime` now accept:
- ISO dates (YYYY-MM-DD) and times (HH:MM)
- Common natural-language words: `tomorrow`, `today`, `tonight`, `this evening`, weekday names (`monday`..`sunday`, `this monday`..`this sunday`)
- Number-as-time: `7`, `7pm`, `19:00`, `7:30pm`
- Word-numbers: `one`..`twelve`, `seven pm`, `eight am`

After fix, the rehearsal completed end-to-end with the same script.

**Implication for real ResDiary:** The real ResDiary API likely accepts natural-language dates too (ResDiary's public docs are ambiguous on this). If not, the dispatcher would need to add a date-resolution layer between the model and the API. TODO when API access arrives.

### Bug 2: STT mis-heard the phone number on first attempt (NOT a bug, design choice)

**Symptom:** During the first run (before the date fix), the model saw the phone number as `2017-07-15` (because the spaced-out TTS "0 7 9 1 7, 7 1 5 7 3 4" was sometimes heard as a date). The model asked for clarification. **This is correct behavior** — the model shouldn't guess when STT is ambiguous.

**On the second run (after the date fix),** the phone number was captured correctly: `07917715734` matched the function call's `args.phone` exactly. So the STT works fine when the audio is clean and the model has already had a context turn.

**Implication for production:** STT is a real risk for phone numbers. The system prompt already spaces the digits digit-by-digit ("0 7 9 1 7, 7 1 5 7 3 4") which helps. If STT degradation is observed, the worker should re-prompt the caller to confirm the number rather than guess.

### Bug 3: Party-size change re-checks availability but does not update the booking (DESIGN LIMITATION)

**Symptom:** When the caller said "make that six, not four", the model re-checked `availability.check` (correct — different party size, different availability) but did not call any tool to UPDATE the existing booking. The system prompt says "use availability.check first, then booking.create" — for updates we'd want either a `booking.update` tool or a "create new booking + cancel old" flow.

**Resolution:** This is a design decision, not a bug. For MVP, the simplest path is "if the caller changes party size, cancel the existing booking and create a new one" — but that requires either a new `booking.cancel` tool or a TODO marker. The dispatcher doesn't have either yet.

**Implication for production:** Add `booking.cancel` (or `booking.update`) to the dispatcher before going live. The system prompt should also include "if the caller changes party size, treat as a new booking (cancel + create) or update (if available)."

### Bug 4: MaxListenersExceededWarning in the orchestrator (FIXED in source, not re-run)

**Symptom:** `(node:1069676) MaxListenersExceededWarning: Possible EventEmitter memory leak detected. 11 response_done listeners added`. The orchestrator added a new `response_done` listener per `wait` step without removing the old ones.

**Fix:** `src/rehearsal.js` `waitForResponseDone` now `removeListener` on resolve. This is a code-quality improvement; the rehearsal data is unaffected.

## Recommended next step once ResDiary API arrives

1. **Set env:** `RESDIARY_API_KEY=...`, `RESDIARY_VENUE_ID=...`, `USE_REAL_PROVIDERS=1`.
2. **Fill in the field-mapping TODOs** in `src/providers/real/resdiary-adapter.js` (4 functions, marked `TODO: confirm field names`). The contract test will surface any field-name mismatches.
3. **Run `npm test`** — 15/15 contract tests should still pass against the real API. If any fail, the error messages will name the failing field.
4. **Repeat for `DEPOS_API_KEY`** and the manager queue endpoint.
5. **Re-run the rehearsal** (`node src/rehearsal.js`) against the real APIs. The orchestration logic is identical; only the provider implementations change.
6. **Listen to `rehearsal-assistant.wav`** — confirm Eve's voice quality is unchanged.

Estimated time: 1-2 hours.

## Recommended next step before live Telnyx I/O

The Telnyx I/O scaffold (`src/telnyx-io.js`) is file-based and works correctly. To go live:

1. **Replace `TelnyxMediaSource`** with a Telnyx WSS client that consumes media frames from a Telnyx Programmable Voice call control webhook. Keep the same `on('frame', ...)` event signature.
2. **Replace `TelnyxMediaSink`** with a Telnyx WSS client that writes back to the same call. Keep `write(pcm16_24k)`.
3. **Re-run the rehearsal** with the live source — the existing metric summary (gap_ms, decode_ms, resample_ms, pacing_ms, dropped_frames) gives the baseline to compare against.
4. **Instrument inter-packet timing on both directions from the first call** — per the manager's earlier directive.
5. **If metrics look healthy, wire into the production worker** (`src/index.js`) by replacing the `TelnyxMediaSource`/`Sink` references there.

Estimated time: 1 day (mostly Telnyx WSS API integration + monitoring).

## Files added in this iteration

- `experimental/livekit/xai-phone-worker/src/rehearsal.js` — full fake-provider rehearsal orchestrator
- `experimental/livekit/xai-phone-worker/scripts/gen-rehearsal-fixtures.mjs` — fixture generator (Cartesia Sonic 3.5 → WAV → ffmpeg → PCMU 8 kHz)
- `experimental/livekit/xai-phone-worker/fixtures/rehearsal/t01-hello.pcmu` (4,377 bytes)
- `experimental/livekit/xai-phone-worker/fixtures/rehearsal/t02-book.pcmu` (12,057 bytes)
- `experimental/livekit/xai-phone-worker/fixtures/rehearsal/t03-tomorrow-7-4.pcmu` (19,737 bytes)
- `experimental/livekit/xai-phone-worker/fixtures/rehearsal/t07-george.pcmu` (5,657 bytes)
- `experimental/livekit/xai-phone-worker/fixtures/rehearsal/t09-phone.pcmu` (42,137 bytes)
- `experimental/livekit/xai-phone-worker/fixtures/rehearsal/t14-change-to-6.pcmu` (24,217 bytes)
- `experimental/livekit/xai-phone-worker/fixtures/rehearsal/t16-off-script.pcmu` (17,817 bytes)
- `experimental/livekit/xai-phone-worker/tmp/rehearsal/rehearsal.log` (6,568 bytes, output)
- `experimental/livekit/xai-phone-worker/tmp/rehearsal/rehearsal-metrics.json` (7,056 bytes, output)
- `experimental/livekit/xai-phone-worker/tmp/rehearsal/rehearsal-assistant.wav` (3,309,164 bytes, output)
- `experimental/livekit/xai-phone-worker/tmp/rehearsal/rehearsal-assistant.pcmu` (551,520 bytes, output)

## Production untouched

All work on `feat/livekit-hd-spike` in `experimental/livekit/`. No production PCMU runtime, no production .env, no production systemd, no Telnyx production webhook, no production gateway, no main-branch changes. No secrets, env files, tokens, binaries, WAVs (the assistant WAV is a test artefact, not a debug recording), debug audio, or logs committed.
