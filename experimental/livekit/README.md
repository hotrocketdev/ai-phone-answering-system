# LiveKit HD Audio — Experimental Workspace

**Created:** 2026-05-28
**Updated:** 2026-06-03 (minimal spike scaffold)
**Status:** Proof-of-concept spike — no production code
**Branch:** `feat/livekit-hd-spike`

---

## Purpose

This workspace holds the **LiveKit HD audio spike** — a proof-of-concept that VoxLane can deliver near-human voice quality through a non-PSTN media path (LiveKit + Opus at 48 kHz) without disturbing the current PCMU production runtime.

This is a **proof-of-concept spike**, not a production migration. Nothing here is wired into the production runtime.

---

## Spike Plan

See [`docs/context/LIVEKIT_HD_SPIKE_PLAN.md`](../../docs/context/LIVEKIT_HD_SPIKE_PLAN.md) for the full design.

**TL;DR:** Cartesia HD PCM (24 kHz) → Go publisher → LiveKit room → browser client hears HD voice. No SIP, no OpenAI, no booking, no Telnyx.

---

## Reference Docs

- `docs/context/LIVEKIT_HD_SPIKE_PLAN.md` — Full spike design (2026-06-03)
- `docs/context/VOICE_QUALITY_STACK_STRATEGY.md` — Why LiveKit is the recommended path
- `docs/NEXTGEN-LIVEKIT-HD-AUDIO-PLAN.md` — Original next-gen plan (2026-05-28, partially superseded)
- LiveKit docs: https://docs.livekit.io
- LiveKit Go SDK: https://pkg.go.dev/github.com/livekit/server-sdk-go

---

## Architecture Target (Phase 1 — this spike)

```
Cartesia HD PCM (24 kHz, pcm_s16le)
  → Go publisher (experimental/livekit/publisher/)
    → LiveKit room (LiveKit Cloud free tier)
      → Browser client (experimental/livekit/web-client/)
        → User hears HD voice through speakers
```

**Phase 2 (two-way conversation) and Phase 3 (PSTN bridge via LiveKit SIP) are out of scope for this spike.**

---

## Key Constraints

- Production PCMU runtime on VPS is locked and must remain untouched.
- No production binary rebuild, no production env change, no production service restart.
- No production Telnyx webhook change.
- No Cartesia production config change.
- No OpenAI model change.
- No booking flow integration.
- All spike credentials are scoped to the spike project only — no production secrets.

---

## Spike Phases (2026-06-03 minimal version)

### Phase 1 — One-way audio proof (target)

- [x] Set up LiveKit Cloud project (free tier)
- [x] Scaffold Go publisher (Cartesia HD → PCMU → LiveKit) — PCMU intermediate, Opus deferred
- [x] Scaffold web client (HTML + livekit-client)
- [x] Run publisher pre-flight: token generated, room joined, track published, 5s tone streamed
- [ ] Run end-to-end browser test (user must execute — see [results/BROWSER_AUDIO_TEST_RUNBOOK.md](results/BROWSER_AUDIO_TEST_RUNBOOK.md))
- [ ] Replace PCMU with Opus (deferred — see [results/README.md](results/README.md) "Opus/HD Follow-Up Plan")
- [ ] Document results

### Out of scope (deferred)

- Two-way conversation (browser mic → OpenAI → Cartesia)
- LiveKit SIP trunk to Telnyx
- Production deployment
- Booking integration
- PCMU removal or replacement

---

## Directory Structure

```
experimental/livekit/
├── README.md              # This file
├── server-notes.md        # LiveKit Cloud or self-hosted setup notes
├── publisher/
│   ├── .env.example       # Placeholder env names (no real secrets)
│   └── README.md          # How to run the publisher
├── web-client/
│   ├── index.html         # Simple HTML page with livekit-client (scaffold only)
│   └── README.md          # How to open in browser
└── results/
    └── README.md          # Results will be documented here after spike runs
```

**Actual Go publisher code and working web client are not yet written.** They will be added in a follow-up step after the plan is reviewed.

---

## Rollback

The spike is on a feature branch (`feat/livekit-hd-spike`). To roll back:

```bash
git checkout main
git branch -D feat/livekit-hd-spike
```

The PCMU production runtime on VPS is completely independent and continues to operate normally.

---

## Original Research Notes (2026-05-28, superseded)

The original research-phase notes (Phase 1 research complete, Phases 2-4 pending, Twilio-based comparison) are superseded by the 2026-06-03 minimal spike. The full next-gen plan is in `docs/NEXTGEN-LIVEKIT-HD-AUDIO-PLAN.md`.
