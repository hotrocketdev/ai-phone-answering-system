# Plan D Voice Agent Spike — Final Report (NO-GO)

**To:** Worker Manager
**From:** Worker
**Date:** 2026-06-07
**Subject:** Plan D (xAI Voice Agent + Eve) spike concluded — **NO-GO on the LiveKit + xAI + Go SDK path** per the agreed decision gate

## TL;DR

After 8 test runs (r1–r8), the **outbound audio transport** for the xAI → LiveKit → browser path proved unsalvageable in the current LiveKit Go SDK + standard WebRTC browser environment:

- r5: `oggpagesize 4096` → "terrible, most phrases cut off" (page-buffer bursts)
- r7: `-page_duration 20000` band-aid → "mostly ok, some cut-offs" (reduced but not eliminated)
- **r8: L16 direct (PCM over WebRTC) → SDP negotiation failure, "codec is not supported by remote"**

Per the manager's decision gate: **L16 failed → mark the LiveKit/xAI spike as blocked/NO-GO → move back to production-stack improvements**.

Production stack, VPS .env, Telnyx webhook, production gateway — **all untouched**. No production changes. No commits to main. All work on `feat/livekit-hd-spike` in `experimental/livekit/`.

---

## What was achieved (preserved in code)

- xAI Voice Agent + Eve + `grok-voice-latest` — **works**, sounds great
- xAI WSS session management, `session.update`, temperature, system prompt — all working
- **Function-call bridge** — `OnFunctionCall` wired with real call_id from `function_call_arguments.done`, stub dispatcher for `availability.check` / `booking.create` / `manager.escalate`, `function_call_output` sent correctly, assistant resumes after tool. **3/3 function calls fired correctly in r4 and r7.**
- Inbound audio path (browser → LiveKit → OGG mux → ffmpeg decode → xAI) — works
- LLM eval, smoke tests, 30-min validation harness, log analyzer — all built and working

## What blocked us (audio transport)

| Issue | Tried | Verdict |
|---|---|---|
| ffmpeg Opus encoder + OGG muxer page-buffer bursts | `oggpagesize 256` (r1–r4, r6), `oggpagesize 4096` (r5), `-page_duration 20000` (r7) | r5 "terrible", r7 "mostly ok" — never smooth |
| LiveKit Go SDK has no built-in PCM-to-Opus encoder in `SampleProvider` | (SDK limitation, not a config issue) | Option 1 (PCM direct via SDK) **blocked by SDK** |
| Browser WebRTC SDP doesn't include L16 | r8: set `LocalSampleTrack` codec to `audio/L16 24kHz mono` | **SDP failure: "codec is not supported by remote"** |
| Other options | (would require: a) CGo Opus encoder — no gcc on build host; b) pure-Go Opus encoder — multi-day project; c) browser-side Web Audio API L16 decoder — custom) | Out of scope for the gate |

## Test history

| Run | Date | Audio | ffmpeg | VAD | temp | Verdict |
|---|---|---|---|---|---|---|
| 9/9 PASS | 2026-06-06 | Opus | (initial) | n/a | -1.0 | "did really well" |
| r1 | 2026-06-07 | Opus | `oggpagesize 256` | 2000 | 0.2 | FAIL: hallucination, no greeting |
| r2 | 2026-06-07 | Opus | `oggpagesize 256` | 2000 | 0.2 | FAIL: repeated "what date" 3x |
| r3 | 2026-06-07 | Opus | `oggpagesize 256` | 1500 | 0.2 | partial: function call emitted, no `OnFunctionCall` wired |
| r4 | 2026-06-07 | Opus | `oggpagesize 256` | 1500 | 0.2 | "mostly okay" — 3/3 function calls work, TEST-1234 returned |
| r5 | 2026-06-07 | Opus | `oggpagesize 4096` | 1200 | 0.7 | "terrible, most phrases cut off" |
| r6 | 2026-06-07 | Opus | `oggpagesize 256` (revert) | 1500 (revert) | 0.7 | (baseline, no browser test) |
| r7 | 2026-06-07 | Opus | `-page_duration 20000` | 1500 | 0.7 | "mostly ok, some cut-offs"; model invented date 2024-10-07; re-asked for name/phone |
| **r8** | 2026-06-07 | **L16** | (none) | 1500 | 0.7 | **SDP failure — codec not supported** |

## What is **not** in the failure (preserve this knowledge for any future re-attempt)

The xAI side is solid. The function-call bridge is solid. The 9/9 PASS test confirmed that the xAI voice quality is genuinely good — better than the production stack, per your direct comparison. If a future re-attempt of this spike is warranted (e.g., xAI ships a LiveKit-friendly PCM path, or the team adds a Go Opus encoder dependency), the inbound path, WSS client, and function-call bridge can be reused as-is.

## Decision gate (per the manager's directive)

> "If r7 still has audio cut-offs, run one L16 codec test. If L16 also fails, mark the current LiveKit/xAI spike as blocked/no-go for now and move back to production-stack improvements."

r7 had cut-offs → L16 test run → L16 failed → **spike is NO-GO**. Production-stack improvements is the next workstream.

## Cost / strategic context (for the manager decision)

- The user reported their experience with xAI was better than the production stack — not trading quality for cost, getting **better quality AND lower cost**
- Plan D (xAI Voice Agent + Eve) at $3/hr vs current production $7-13k/mo = potential **$4-10k/mo savings**
- Plan D is gated on 30-min production-style validation before any production promotion
- Production promotion would have been **additive worker mode** (does not replace existing 3 modes), so spike failure is bounded risk
- The spike failure does **not** impact production in any way

## Files / artifacts (all on `feat/livekit-hd-spike`, no production touched)

- `C:\builds\AI-Phone-Answer-System\experimental\livekit\xai-voice-agent\` — the spike (Go source, not yet committed)
- `experimental\livekit\xai-voice-agent\logs\30min-2026-06-07-r4.log` — last good baseline (Opus path, 3/3 function calls work, 8.2s p50, "mostly ok")
- `experimental\livekit\xai-voice-agent\logs\30min-2026-06-07-r5.log` — r5 (terrible, cut-offs)
- `experimental\livekit\xai-voice-agent\logs\30min-2026-06-07-r7.log` — r7 (band-aid, partial)
- `experimental\livekit\xai-voice-agent\logs\30min-2026-06-07-r8.log` — r8 (L16 SDP failure)
- `experimental\livekit\xai-voice-agent\report-30min-2026-06-07.md` — 20-item manager report for r1
- `experimental\livekit\xai-voice-agent\report-30min-2026-06-07-r2.md` — 20-item manager report for r2
- `experimental\livekit\xai-voice-agent\report-30min-2026-06-07-r3.md` — 20-item manager report for r3
- `docs\experimental\livekit-hd-spike\STAGE_1_5_COST_QUALITY_REPORT.md` — manager report, §12 spike tracker, §13 30-min validation
- `docs\experimental\livekit-hd-spike\XAI_FULL_STACK_VALIDATION.md` — 10-layer matrix

---

## Questions for the manager

Please confirm or amend the following:

1. **Confirm NO-GO on the LiveKit/xAI spike.** Should we mark this as "blocked on audio transport, re-evaluate if xAI ships a LiveKit-friendly PCM path or we revisit CGo"?

2. **Commit the spike code before pivoting?** The r4 + r7 + r8 work is currently uncommitted. Should we:
   - (a) Commit r4 + r7 + r8 + final NO-GO report to `feat/livekit-hd-spike` (preserves work for future re-attempt)
   - (b) Discard the spike code (clean slate, lose knowledge)
   - (c) Keep the branch as-is without committing

3. **Start the production-stack improvements (Opus's recommendations)?** Specifically:
   - **Codec upgrade** — migrate from Twilio PCMU/PCMA (8kHz) to Telnyx with wider-band codec (L16/16k+). Per Opus: "once you're off Twilio's codec floor you can push L16/16k then higher, and the same Cartesia voice will sound markedly warmer."
   - **VAD tuning** — OpenAI Realtime owns VAD; per Opus, "their endpointing keeps improving without you doing anything." Local tuning of silence thresholds and turn-taking rules.
   - **Turn-taking hardening** — barge-in is the expensive 80% (you have it over PSTN), the gap is graceful overlap handling and "sorry, what?" recovery.
   - **gpt-4o-realtime-preview deprecation** — pin to a versioned model string, isolate the schema-mapping layer.

4. **Priorities and resource allocation?** Specifically:
   - Should the worker pick this up immediately, or do you want to discuss the production-stack work with Opus first?
   - Are there any other workers available, or is this single-threaded?
   - What's the timeline expectation for the codec upgrade (Telnyx migration is non-trivial — could be 1-2 weeks)?

5. **Re-attempt of Plan D in future?** Should we:
   - (a) Archive the spike knowledge and revisit in Q3 2026 if xAI ships improvements
   - (b) Park it indefinitely
   - (c) Re-evaluate after the production-stack improvements are done and we have a clearer picture of what we need

6. **Anything else I missed?** — Is there any context from the Opus / 4.8 conversation or from your parallel work that should be folded into the production-stack plan?

---

## Critical rules (still in force)

- All work on `feat/livekit-hd-spike`
- All work inside `experimental/livekit/`
- Do NOT touch production PCMU runtime
- Do NOT modify production .env
- Do NOT modify production systemd
- Do NOT modify Telnyx production webhook
- Do NOT change production gateway
- Do NOT merge to main
- Do NOT commit secrets, env files, binaries, WAVs, logs, or debug audio
- Keep Plan A fallback preserved
