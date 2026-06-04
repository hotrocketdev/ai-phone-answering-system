# LiveKit HD Spike — Next-Step Decision Matrix

**Date:** 2026-06-04
**Branch:** `feat/livekit-hd-spike`
**Pre-condition:** Spike is complete and HD path is proven. Production PCMU runtime is untouched.

This document compares five possible next steps (A–E) along six dimensions, then recommends a single next step at the bottom.

---

## A. Continue Sonic 3.5 + LiveKit Opus and build two-way conversation loop

Build a browser-side spike that captures mic audio, sends it to a backend, runs OpenAI Realtime, returns Cartesia/Opus audio, and plays it back through the same LiveKit Opus track. Also add acoustic echo cancellation.

- **Benefit:** Closes the only major missing piece. With two-way, the LiveKit path becomes a complete replacement for the PSTN path. Voice quality and latency gain become user-visible, not just measurable. This is the natural "yes, the technology works — let's build the product" step.
- **Risk:** Two-way introduces new failure modes: mic permission UX, echo / feedback, half-duplex artifacts, OpenAI turn boundary at the LiveKit boundary, browser autoplay on the reply direction. None of these are in the current spike. Estimated 60 % chance of a surprise that requires acoustic echo cancellation.
- **Effort:** 5–10 days. New `web-client/app.js` for mic capture + Opus publish-back, new Go backend bridge (publisher becomes bidirectional), AEC (Web Audio's `echoCancellation: true` may suffice for spike), OpenAI Realtime integration via WebSocket. No production binary changes.
- **Production impact:** **Zero.** This is a new spike on the same `feat/livekit-hd-spike` branch (or a new branch off it). The current PCMU production runtime is unchanged. The spike can be abandoned without affecting production.
- **Success criteria:**
  1. Browser hears Cartesia Opus voice reply within 2.5 s of mic input.
  2. No echo of TTS audio in the next mic input (AEC works).
  3. OpenAI turn boundary at 1–3 s of caller speech.
  4. ≥ 90 % of test exchanges feel natural (manual subjective).
- **Rollback plan:** Delete the new spike code and the merged commit. No production files were touched, so rollback is "rm -rf new spike dir + git revert".

---

## B. Compare one alternative TTS provider through same LiveKit Opus harness

Pick one alternative (e.g. ElevenLabs, Play.ht, or OpenAI TTS) and route it through the same ffmpeg + LiveKit Opus pipeline. Measure silence RMS, voice quality, latency, and per-character cost on a fixed greeting.

- **Benefit:** Validates that Sonic 3.5 is the right pick. If the alternative is materially better, the spike's "f32le noise" was a Sonic 3.5 + s16le interaction, and switching providers could give us better HD audio without the f32le workaround. If the alternative is worse, Sonic 3.5 + f32le is confirmed.
- **Risk:** Cost — every TTS provider other than Cartesia is more expensive. Effort to wire up a second TTS client is non-trivial (different API, different streaming model, possibly different encoding). Time-sink risk: comparing TTS providers can turn into a long, low-return investigation.
- **Effort:** 2–4 days for one provider. New `elevenlabs.go` (or similar) with similar surface to `cartesia.go`, swap one env var, run the same latency script, compare numbers. Would need API key from a paid plan; some providers have free tiers for spike-sized traffic.
- **Production impact:** **Zero.** Spike-only. The alternative TTS is never called from the production PCMU runtime.
- **Success criteria:**
  1. Same ffmpeg + LiveKit Opus pipeline produces clean audio from the alternative TTS.
  2. Silence RMS within ±3 dB of Sonic 3.5 f32le.
  3. First-audio-byte within ±500 ms of Sonic 3.5 f32le.
  4. Per-character cost documented; if more than 2× Cartesia Sonic 3.5 cost, the alternative is not worth it without other clear advantages.
- **Rollback plan:** Delete the new TTS client file, remove the comparison section from `SPIKE_REPORT.md`, no production files touched.

---

## C. Test LiveKit SIP / PSTN bridge later

Wait until the HD path is fully proven two-way, then add a LiveKit SIP service connected to the Telnyx trunk. This would let PSTN callers hear HD audio too.

- **Benefit:** Closes the PSTN quality gap. Currently PSTN callers are stuck at G.711 (3.4 kHz) or G.722 (7 kHz). LiveKit SIP + Opus would deliver ~20 kHz to PSTN callers via transcoding in the LiveKit SIP service.
- **Risk:** LiveKit SIP service is a separate Docker image with its own deployment story. SIP trunk to Telnyx requires Telnyx-side configuration (a new "credential" and "SIP trunk" in Telnyx Mission Control). Wrong configuration could drop PSTN calls. This is the highest-risk option because it touches the production telephony provider.
- **Effort:** 2–4 weeks. Includes: LiveKit Cloud SIP service setup (paid tier), Telnyx SIP trunk config, DID number porting or alias routing, new spike for SIP integration, separate security review, fallback plan if SIP fails.
- **Production impact:** **High** if pursued. SIP trunking touches Telnyx production config, the call routing path, and possibly the DID numbers. **This option should be considered Phase 3, not Phase 1.**
- **Success criteria:**
  1. PSTN caller dials the existing DID and connects to LiveKit SIP.
  2. Caller hears HD Opus audio (not G.711/G.722).
  3. Outbound calls from VoxLane still work on Telnyx direct.
  4. Failover to PCMU direct path works if LiveKit SIP is down.
- **Rollback plan:** Remove the LiveKit SIP credential from Telnyx, point the DID back to the Telnyx direct trunk. Documented in Telnyx Mission Control and LiveKit Cloud dashboards.

---

## D. Pause LiveKit and return to tenant knowledge / product features

Stop the LiveKit work here. Spike is documented in `SPIKE_REPORT.md`. Pivot to: tenant-side product features (booking dashboard, customer knowledge base, multi-tenant onboarding), or any of the open fix-list items.

- **Benefit:** Stops burning engineering time on infrastructure that may never ship to production. Lets the team focus on revenue-generating features. The HD spike is preserved as documentation; it can be revisited in 6–12 months.
- **Risk:** Voice quality is the single biggest competitive differentiator for an AI receptionist product. Pausing HD indefinitely means the product stays at "G.711 phone quality" while competitors may move to HD. Also risks losing the institutional knowledge gained in this spike (Go publisher + LiveKit + Opus + Cartesia).
- **Effort:** 0 days for HD. Effort to pivot depends on what product features are chosen; assume 1–2 weeks to re-scope.
- **Production impact:** **Zero** for the HD pause. Could be high for the pivot depending on what features are chosen.
- **Success criteria:** N/A — this is a "do nothing on HD" decision. The success criteria are whatever the pivot's are.
- **Rollback plan:** N/A.

---

## E. Merge only docs later, keep spike branch separate

At some future point, decide to merge the **docs only** (`SPIKE_REPORT.md`, `NEXT_STEP_DECISION.md`, the latency section of `README.md`, the spike design + strategy docs) to `main`, but keep the spike code (`experimental/livekit/`) on a separate feature branch that is never merged to main.

- **Benefit:** Preserves the institutional knowledge in the main branch history (so future engineers see it when they read the main-branch docs), without committing to any production integration. Useful as a long-term record.
- **Risk:** Low. The spike code stays on the feature branch where it is. The docs land on main as reference material. No production runtime changes.
- **Effort:** 1–2 hours. Cherry-pick the doc-only commits onto a new branch off main, run tests, push, open a PR.
- **Production impact:** **Zero.** Docs only.
- **Success criteria:**
  1. Docs are readable from `main` (e.g. `docs/context/HD_AUDIO_SPIKE_SUMMARY.md`).
  2. `experimental/livekit/` still does not exist on main.
  3. The spike branch can still be checked out and re-run from a fresh clone.
- **Rollback plan:** `git revert <merge-sha>` from main. Trivial.

---

## Summary table

| Option | Benefit | Risk | Effort | Prod impact | Rollback ease |
|---|---|---|---|---|---|
| **A** Two-way conversation | Closes the biggest gap | New failure modes (echo, AEC) | 5–10 d | Zero | Easy |
| **B** Alternative TTS comparison | Validates Sonic 3.5 choice | Cost, time-sink | 2–4 d | Zero | Easy |
| **C** LiveKit SIP / PSTN bridge | Closes PSTN quality gap | Touches Telnyx, highest risk | 2–4 wk | **High** | Medium |
| **D** Pause / pivot | Frees up engineering time | Loses competitive moat, knowledge | 0 d for HD | Zero | N/A |
| **E** Merge docs only | Preserves knowledge on main | None meaningful | 1–2 h | Zero | Trivial |

---

## Recommendation

**Recommended next step: A (two-way conversation loop), with a short Option B alternative-TTS comparison first if and only if a second provider's API key is already available on hand.**

Reasoning:

1. The spike has already proven the **hardest** technical part: full-band Opus audio from Cartesia through ffmpeg to LiveKit to a browser, with no audible noise and 2.1 s first-audio-byte. The remaining work to a working receptionist bot over LiveKit is a known-shape engineering problem (mic capture, AEC, OpenAI Realtime bridging, full-duplex flow control), not an open research question.
2. Option A is the natural next step because it converts the spike from "proof of path" into "working product on a non-production track". This is the cheapest way to validate that the HD path actually works in real conversation (which it has not yet been tested in).
3. Option B is a useful sanity check on Sonic 3.5 but should be **time-boxed to ≤ 1 day**. If a second TTS provider is available, a quick A/B on the same greeting and the same Opus pipeline is high-value insurance. If no second provider is available, do not block on this — proceed to A.
4. Option C is a Phase 3, not Phase 1. The HD path is not yet proven two-way; SIP bridging should wait.
5. Option D is the safest "do nothing" choice but loses competitive ground.
6. Option E is a good housekeeping step and can be done at any time, including in parallel with A.

**Concrete sequence:**

1. Time-boxed TTS comparison (Option B) — ≤ 1 day, skip if no second provider.
2. Two-way conversation loop (Option A) — 5–10 days, on the same `feat/livekit-hd-spike` branch, no production touches.
3. At any point, merge docs only (Option E) so the spike findings are visible from main.

**Do NOT** pursue Option C until Option A is complete and a live regression call on production has confirmed that Sonic 3.5 + Opus is a viable PSTN replacement (a separate, larger spike).

**Do NOT** pursue Option D until the team has at least one user-visible product feature shipping per month. If product velocity is healthy, the HD pause is fine. If product velocity is slow, HD is a competitive moat and should stay in the queue.

---

*End of decision matrix.*
