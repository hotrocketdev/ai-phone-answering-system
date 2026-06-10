# multi-vendor-spike

**Status:** SKELETON COMPLETE. No vendor SDKs installed yet. No live API calls made. No money spent.

## What's here

A multi-vendor voice pipeline scaffold for the VoxLane receptionist. The goal is to A/B test
combinations of STT + LLM + TTS + transport + memory vendors to find the cheapest combo that
hits < 1.0s first-audio latency. The current single-vendor baseline (xAI Voice Agent WSS) is
1.8-2.1s.

Three bundles are pre-built:

| Bundle | STT | LLM | TTS | Cost/min | First-audio |
|---|---|---|---|---|---|
| `xai-bundle` (baseline) | xAI bundled | xAI bundled | xAI Eve | $0.05 | 1.8-2.1s |
| `hybrid-deepgram` | Deepgram Nova-3 | xAI Grok | xAI Eve | $0.05 | 0.8-1.1s expected |
| `hybrid-elevenlabs` | Deepgram Nova-3 | xAI Grok | ElevenLabs Charlotte | $0.25 | 0.7-1.0s expected |

## Why this is multi-vendor

The user (Jorge) decided on 2026-06-10 that the future architecture must be multi-vendor,
even if we ship on xAI single-vendor for the first release. Reasons:
- "It will be easy to change" (user)
- Voice quality + latency are the competitive differentiators
- Vendor pricing changes constantly; we want to be able to swap without a rewrite
- Some vendors are best-in-class for one component but not others (Deepgram STT, xAI Grok LLM, ElevenLabs TTS)

## How the spike is structured

```
src/
├── index.js              # worker entry; --vendor-bundle <name>
├── orchestrator.js       # vendor-agnostic pipeline
├── rehearsal.js          # 18-step script + head-to-head comparison
├── contract.test.js      # vendor contract smoke tests
└── vendors/
    ├── contracts.ts      # FROZEN vendor interfaces
    ├── bundles.js        # bundle factory
    ├── stt/
    │   ├── xai.js        # xAI bundled STT (working)
    │   └── deepgram.js   # Deepgram Nova-3 (stub, ready for SDK install)
    ├── llm/
    │   ├── xai-grok.js   # xAI Grok text LLM (stub, ready for fetch wiring)
    │   └── cerebras.js   # Cerebras Llama 3.3 70B (stub, ready for SDK install)
    ├── tts/
    │   ├── xai-eve.js    # xAI Eve (working through WSS)
    │   └── elevenlabs.js # ElevenLabs Charlotte (stub, ready for SDK install)
    ├── transport/
    │   ├── file-loopback.js  # file-based rehearsal transport (working)
    │   └── livekit.js        # LiveKit transport (stub, ready for SDK install)
    └── memory/
        ├── redis-mem.js  # in-memory stub (working, used in tests)
        └── redis.js      # Redis client (stub, ready for ioredis install)
```

## What's not here (pending user approval)

These are blocked on explicit user approval. **Do not install these without sign-off:**

| Package | Enables | Cost | Why pending |
|---|---|---|---|
| `@deepgram/sdk` | Deepgram Nova-3 STT live test | $0 (free $200 trial) | user hasn't approved the install |
| `elevenlabs` | ElevenLabs TTS live test | $0 (free 10K chars) | user wants the spike scaffold first |
| `@cerebras/cerebras-cloud-sdk` | Cerebras LLM live test | $0 (free trial) | user hasn't approved |
| `@livekit/agents` | LiveKit transport live test | $0 (1K free participant-min) | browser path was NO-GO; phone path unproven |
| `ioredis` | Redis client (production VPS) | $0 (VPS already has Redis) | user hasn't approved |

The new chat session should ask the user before installing any of these.

## Run the contract tests (no SDKs needed)

```bash
cd experimental/livekit/multi-vendor-spike
npm install ws dotenv
npm test
```

Expected: 10/10 pass. The SDK-backed vendors throw on missing key, which is the correct
behaviour. The in-memory stubs (memory/redis-mem, transport/file-loopback) load and pass.

## Run a single bundle rehearsal

```bash
# xAI baseline (works without any new SDKs)
node src/rehearsal.js --vendor-bundle xai-bundle

# Deepgram + xAI (requires DEEPGRAM_API_KEY + SDK install)
DEEPGRAM_API_KEY=... node src/rehearsal.js --vendor-bundle hybrid-deepgram

# Deepgram + xAI + ElevenLabs (requires all three)
DEEPGRAM_API_KEY=... ELEVENLABS_API_KEY=... \
  node src/rehearsal.js --vendor-bundle hybrid-elevenlabs
```

## Run head-to-head comparison

```bash
DEEPGRAM_API_KEY=... ELEVENLABS_API_KEY=... \
  node src/rehearsal.js --vendor-bundle compare
```

Outputs a `tmp/rehearsal/COMPARE_REPORT.md` with a side-by-side table.

## Production untouched

All work on `feat/livekit-hd-spike` in `experimental/livekit/multi-vendor-spike/`. No
production PCMU runtime, no production .env, no production systemd, no Telnyx production
webhook, no production gateway, no main-branch changes. No secrets, env files, tokens,
binaries, WAVs, debug audio, or logs committed. No live API calls made.

## See also

- `docs/experimental/livekit-hd-spike/DECISION_REPORT_MULTI_VENDOR.md` — the main handoff doc
- `docs/experimental/livekit-hd-spike/DECISION_LOG.md` — running log of every decision
- `docs/experimental/livekit-hd-spike/XAI_FAKE_PROVIDER_REHEARSAL.md` — baseline numbers
- `experimental/livekit/xai-phone-worker/` — the current production-bound worker
