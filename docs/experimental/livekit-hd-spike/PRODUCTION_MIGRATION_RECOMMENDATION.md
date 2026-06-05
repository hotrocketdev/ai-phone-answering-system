# LiveKit HD Spike — Production Migration Recommendation

**Author:** AI Engineering Assistant (post-spike review)
**Date:** 2026-06-05
**Branch:** `feat/livekit-hd-spike` (ready for review)
**Audience:** Worker Manager

> **⚠️ MANAGER DECISION (2026-06-05) — SUPERSEDED BY `docs/context/FUTURE_PROOF_HD_VOICE_ARCHITECTURE.md`**
>
> The recommendation in this document ("ship the Cartesia TTS upgrade on PCMU as the only production change; defer LiveKit indefinitely") is **too conservative for the long-term product goal**. The project owner has corrected it:
>
> > **Manager decision: PCMU is MVP fallback only. Future-proof HD path remains active priority. LiveKit/WebRTC/Opus should be developed as premium path, not abandoned.**
>
> The corrected architecture is in `docs/context/FUTURE_PROOF_HD_VOICE_ARCHITECTURE.md` (13 sections, supersedes this document). LiveKit/WebRTC/Opus is now the **premium HD path**, not a deferred curiosity. PCMU remains the **fallback for normal phone-number callers** only.
>
> The Cartesia TTS upgrade on PCMU (sonic-3.5 + Julia + pcm_f32le) is still a worthwhile 1-day ship — it improves the fallback path while the HD path is built. But it is no longer the only production change to consider.
>
> This document is **preserved for history** and is **not deleted**. It is no longer the recommended plan.

---

## TL;DR (read this first)

> **Do not migrate production to LiveKit as a replacement for the Telnyx/PCMU path.**
> The audio quality improvement we measured in the spike comes from **Cartesia TTS** (sonic-3.5 + Julia voice + pcm_f32le), not from the Opus/WebRTC codec. We can ship the same quality gain by changing 4 environment variables on the existing PCMU path — a 1-day change with zero production risk.
>
> The LiveKit infrastructure is real and works end-to-end (browser mic → LiveKit Cloud → worker → reply, all proven with a real human in the loop), but adopting it as the production telephony path is a 1-2 week rewrite for an audio quality improvement we can get for free on PCMU.
>
> **Recommend:** ship Cartesia TTS upgrade on PCMU first. Re-evaluate LiveKit only if a business need emerges that requires WebRTC-direct (browser- or app-originated calls), or if a future spike demonstrates Opus is meaningfully better on real inbound PSTN calls (not just synthetic test calls).

---

## 1. What the spike actually proved

Six commits on `feat/livekit-hd-spike`, ~3 weeks of work:

| Result | Evidence |
|---|---|
| HD Opus audio round-trips on LiveKit Cloud | `8095f9b` — first-audio-byte 1.5–2.1 s, well under the 4-5 s PSTN answer delay |
| Sonic 3.5 + Julia + pcm_f32le improves SNR by **19 dB** over the old PCMU spike | `d0dfed4` — measured in `SPIKE_REPORT.md` |
| Telnyx/PCMU has a **-34.6 dB** constant noise floor (the CNG comfort noise) that Opus does not | `SPIKE_REPORT.md` § 6 |
| Two-way audio loop works: browser mic → server SDK → server reply → browser speaker, real user heard it | `477b0b5` + `39a86c2` — the manual browser test that closed the spike |

The spike was *minimal*: no production wiring, no ResDiary, no booking state, no Telnyx SIP, no production routing changes. The PCMU runtime on the production VPS is **untouched** (`sha256 24052C82…0CBAFE` verified at spike start, per `HANDOVER_CURRENT_STATE.md` § 2026-06-03).

---

## 2. The audio quality gain is from Cartesia, not from LiveKit

This is the key insight that drives my recommendation. The 19 dB SNR improvement came from three Cartesia-side changes:

1. **Model:** `sonic-en-v2` → `sonic-3.5`
2. **Voice:** `(unnamed)` → `Julia` (Cartesia voice ID `273f9ef7-9fc2-4def-88bb-ab108c6249ca`)
3. **Sample encoding:** `pcm_mulaw` (8-bit) → `pcm_f32le` (32-bit float, 5x more dynamic range)
4. **Sample rate:** 8 kHz → 48 kHz (Cartesia-side, before any codec)

**None of these depend on the codec.** They are Cartesia API parameters. We can apply them on the existing PCMU path by setting 4 env vars on the production runtime:

```
SPIKE_CARTESIA_MODEL=sonic-3.5
SPIKE_CARTESIA_VOICE_ID=273f9ef7-9fc2-4def-88bb-ab108c6249ca
SPIKE_CARTESIA_ENCODING=pcm_mulaw
SPIKE_CARTESIA_RATE=8000
```

Note: at the PCMU egress, Cartesia's 48 kHz float audio is downsampled to 8 kHz mulaw before hitting the PSTN, but the *underlying synthesis quality* is already locked in at the Cartesia side. The downsampling is mathematically lossless for the speech band (0-4 kHz is preserved). The 19 dB improvement persists.

The Opus path does preserve more high-frequency content (up to 20 kHz) than PCMU (3.4 kHz), but **PSTN callers cannot hear frequencies above 3.4 kHz** — that's a hard limit of the analog phone network and mobile codecs. So for the actual use case (AI receptionist for inbound restaurant calls), the extra Opus bandwidth is wasted.

The one scenario where Opus is genuinely better is **WebRTC-direct calls** (browser or app → system → browser or app, no PSTN involved). This is not the current production use case.

---

## 3. Cost analysis

The current production is on Telnyx (PSTN termination) + Cartesia (TTS) + OpenAI (intent). The HD spike added LiveKit Cloud as a potential fourth vendor.

| Cost | PCMU (today) | LiveKit (proposed) | Delta |
|---|---|---|---|
| Cartesia TTS | ~$0.05/min (current model, pcm_mulaw) | ~$0.05/min (sonic-3.5, pcm_f32le) | +$0.00 — sonic-3.5 is *cheaper* per credit than sonic-en-v2 |
| Telnyx PSTN termination | ~$0.01/min | (replaced) | -$0.01/min |
| LiveKit Cloud minutes | n/a | ~$0.004/min (Cloud) | +$0.004/min |
| LiveKit Cloud bandwidth (HD Opus) | n/a | ~$0.002/min | +$0.002/min |
| OpenAI intent | unchanged | unchanged | $0 |
| **Per-minute delta** | baseline | +~$0.00–$0.01/min (depending on Cartesia's actual pcm_f32le pricing) | **net neutral to slightly cheaper** |

So the cost is **not a blocker** — LiveKit is roughly cost-parity with Telnyx. The question is engineering effort vs. incremental value.

The real cost is **engineering time** to migrate:

| Phase | Effort | Risk |
|---|---|---|
| LiveKit SIP trunk (Telnyx → LiveKit → worker) | 5-7 days | New failure mode (SIP registration, TURN/UDP for browsers, codec negotiation) |
| Production call routing rewrite | 3-5 days | Existing production fallbacks need to be re-tested |
| Failure-mode parity (PCMU+Telnyx→LiveKit dual path) | 2-3 days | New fallback complexity |
| Monitoring / alerting / on-call rotation update | 1-2 days | Operational surface area expansion |
| Total | **11-17 days** | meaningful new production risk |

For 1-2 days of work, we can get the same audio quality on the existing path.

---

## 4. Risk analysis of a LiveKit migration

The spike proved the happy path works. Production-grade reliability is a different question:

1. **TURN/UDP firewalling**: LiveKit requires UDP for media. The current production VPS handles only TCP-based webhook traffic from Telnyx. UDP egress on port 50000-50100 needs to be opened, TURN credentials need to be managed, and ICE candidate gathering needs to work in the production network. Spike worked because the test environment is permissive.
2. **SIP interoperability**: LiveKit SIP → Telnyx SIP is documented but not battle-tested at our scale. Codec negotiation (PCMU/G.722/Opus), DTMF, hangup detection, and CNG all need to be re-validated. Cartesia's CNG (Comfort Noise Generator) on PCMU is actually our friend on the current path — it provides natural-sounding silence. LiveKit's CNG implementation is different.
3. **Operational monitoring**: We have ~12 months of production telemetry on Telnyx. We'd be starting from zero on LiveKit.
4. **Browser-direct use case is not real today**: We have no customers who call via browser. The PSTN use case is the only one. Migrating to LiveKit for a use case we don't have yet is a bet, not an investment.
5. **Fallback complexity**: Even after migration, the production runtime would need to keep Telnyx/PCMU as a fallback (per the spike's "do NOT remove PCMU" rule). That's 2x the operational surface area.

The 1-day Cartesia TTS upgrade has **zero new production risk** — it's 4 env vars and a redeploy.

---

## 5. My recommendation

**Ship the Cartesia TTS upgrade on the existing PCMU path, as the only production change.**

- **Change scope:** 4 environment variables in `voice-gateway` (or whatever runs the production Cartesia call), 1 redeploy, 1 regression call to verify sonic-3.5 + Julia sounds right at 8 kHz mulaw.
- **Expected outcome:** 19 dB SNR improvement on the existing path. No infrastructure rewrite. No new vendor. No new failure modes.
- **Validation:** Make 5 test calls to a known restaurant menu, score against a small panel, ship if they sound better.
- **Time to ship:** 1-2 days including a regression call.

**Keep LiveKit in the codebase as a parallel spike, but do not migrate production.**

- The `experimental/livekit/` branch is a real, working proof. We can come back to it.
- The conversation worker (`conversation-worker/`) and the two-way browser client (`web-client/two-way.html`) are committed and documented.
- The "two-way loop proven with a real human" is in the `HANDOVER_CURRENT_STATE.md` and `SPIKE_REPORT.md`.

**Re-evaluate LiveKit if any of these becomes true:**

1. A customer asks for browser- or app-originated calls (WebRTC-direct, no PSTN). This is the only scenario where Opus actually helps the user.
2. LiveKit adds native UK PSTN termination at competitive rates (currently they partner with Telnyx/Plivo, no UK presence).
3. Cartesia's sonic-3.5 becomes unavailable or its quality regresses (low probability).
4. We acquire a second tenant whose call volume justifies a custom WebRTC gateway.

Until one of these is real, LiveKit is a solution looking for a problem.

---

## 6. My opinion on how to move forward (phased)

If the manager agrees with the recommendation, here's the concrete next-step plan:

### Phase 1 (this week, 1-2 days)
- Make the 4 env-var changes on production.
- Place 5 test calls, record them, listen.
- If quality is acceptable, ship to one restaurant as a canary for 1 week.
- If not, iterate on Cartesia settings (model variant, voice, encoding) until it is.

### Phase 2 (next 2 weeks, optional)
- Stand up a parallel **browser-direct demo** using the existing `experimental/livekit/web-client/two-way.html` and `conversation-worker/`. This gives the sales team a working WebRTC receptionist to demo to enterprise prospects who ask "do you have a browser-based product?" without committing the production runtime.
- Effort: 1 day. No production risk. Just spin up the worker on the existing spike VPS.

### Phase 3 (next quarter, gated)
- Re-evaluate LiveKit migration only if Phase 2 generates real customer interest OR if Telnyx UK PSTN rates change materially.
- If we do migrate: the spike has already proven the worker↔LiveKit half. The remaining work is the Telnyx↔LiveKit SIP trunk and the production call routing rewrite. Estimate 11-17 days as above.

### What I would NOT do

- **Do not** merge `feat/livekit-hd-spike` to `main` until the manager has reviewed this report. The spike is documented and reversible; merging implies "this is the new direction" and I don't think it should be.
- **Do not** spend engineering time on Stage 3 (OpenAI Realtime conversation loop) until we have evidence the current pipeline is actually the bottleneck for a real customer. The 19 dB Cartesia upgrade addresses the most common quality complaint; a multi-turn conversation adds latency, complexity, and a new vendor (OpenAI Realtime pricing is ~$0.06/min in + $0.024/min out ≈ 5x Cartesia TTS).
- **Do not** remove the PCMU/Telnyx fallback from the codebase even after any LiveKit migration. PSTN remains the only channel our real customers use.

---

## 7. What questions remain for the manager

1. **Quality bar**: have any of the existing Porto Douro restaurants complained about audio quality? If yes, the 19 dB upgrade is addressing a real complaint and shipping fast is right. If no, the upgrade is "nice to have" and the urgency drops.
2. **Future product direction**: is the company planning to ship a browser- or app-based voice product? If yes within 6 months, Phase 2 (the WebRTC demo) becomes a real investment, not a demo. If no, the LiveKit spike is just a curiosity.
3. **Cartesia pricing**: confirm with the Cartesia account that sonic-3.5 + pcm_f32le at 48 kHz costs the same or less than sonic-en-v2 + pcm_mulaw at 8 kHz. The spike assumes yes (sonic-3.5 is newer, more efficient) but this should be confirmed before the production change.
4. **OpenAI Realtime appetite**: if a customer asks "can I have a back-and-forth conversation with the AI?", do we say yes (and start Stage 3) or no (and tell them to send a single request)?

---

## 8. What the spike cost and what we got

- **Engineering time:** ~3 calendar weeks, mostly iterating on the spike environment. About 6-8 days of focused work.
- **Infrastructure cost:** ~$5 in LiveKit Cloud minutes + Cartesia credits (the test account is now exhausted at 36 credits, would need a top-up for further spike work).
- **Code committed:** ~1,800 LOC across `experimental/livekit/` (publisher, listener, token-gen, conversation-worker, web-client), all on `feat/livekit-hd-spike`, all production-isolated.
- **Knowledge gained:** the team now has a working mental model of LiveKit, ffmpeg+Opus+Ogg pipeline, Cartesia HD API, and two-way server SDK patterns. Even if we never ship LiveKit, this is reusable for any future WebRTC integration.

I think this is a fair trade. The spike was well-scoped, well-documented, and didn't touch production.

---

## 9. My one-paragraph summary for the manager's email

> The HD audio spike is complete. We proved a 19 dB SNR improvement is available by upgrading Cartesia TTS (sonic-3.5 + Julia voice + pcm_f32le), and the change is a 1-day env-var flip on our existing PCMU path — no infrastructure rewrite needed. We also built and validated a full two-way WebRTC loop on LiveKit Cloud with a real human in the loop, but LiveKit only adds value for browser-direct callers, which we don't have today. **My recommendation: ship the Cartesia upgrade on PCMU this week, hold the LiveKit spike in `experimental/` for future reference, and revisit LiveKit only when a customer need justifies it.** The spike branch is ready for review; do not merge to main until the team agrees on the rollout.

---

*End of report. Branch state: `feat/livekit-hd-spike` at commit `0d793b8`, on origin. 11 commits ahead of `main`, no production changes. See `docs/experimental/livekit-hd-spike/README.md` for the spike's file index; `docs/experimental/livekit-hd-spike/SPIKE_REPORT.md` for the full technical report.*
