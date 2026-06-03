# LiveKit HD Audio Spike — Plan

**Date:** 2026-06-03
**Branch:** `feat/livekit-hd-spike`
**Status:** Design / scaffold only. No production changes.
**Author:** VoxLane engineering (controlled review)

---

## 1. Purpose

Prove that VoxLane can deliver near-human voice quality through a non-PSTN media path (LiveKit + Opus at 48 kHz) without disturbing the current PCMU production runtime.

This is a **proof-of-concept spike**, not a production migration. The goal is to validate the architecture and measure audio quality. Production integration is a separate, gated decision.

---

## 2. Why PSTN Cannot Hit Near-Human Voice Quality

PSTN is narrowband by design. The frequency response is limited by the copper/fiber last mile and the caller's phone handset:

| Codec | Sample Rate | Frequency Response | Quality |
|-------|-------------|-------------------|---------|
| G.711 PCMU | 8 kHz | ~3.4 kHz | Narrowband (PSTN standard) |
| G.722 | 16 kHz | ~7 kHz | Wideband (ISDN/VoIP) |
| Opus (WebRTC) | 48 kHz | ~20 kHz | HD (near-human) |
| EVS / AMR-WB | 32 kHz | ~14 kHz | HD (mobile carrier) |

Cartesia generates audio at 24 kHz PCM. In the current Telnyx direct WebSocket path, this is downsampled to 8 kHz (PCMU) or 16 kHz (G722). The HD audio is lost in the downsampling. No codec or provider change within the PSTN path can exceed the ~7 kHz G.722 ceiling for phone callers.

The G722 controlled test (2026-06-03) confirmed this: marginal improvement in latency and mechanical feel, but voice quality was "more or less the same" as PCMU, and the constant Telnyx comfort noise was still present at the same level.

**The only path to near-human quality is a non-PSTN media path that supports Opus/WebRTC.** LiveKit is the recommended path.

---

## 3. Current PCMU Baseline (Do Not Change)

- `TELNYX_STREAM_BIDIRECTIONAL_CODEC=PCMU`
- `CARTESIA_OUTPUT_ENCODING=pcm_mulaw`
- `CARTESIA_OUTPUT_SAMPLE_RATE=8000`
- `AUDIO_TRANSCODE_OUTBOUND_TO=none`
- `TELNYX_STREAM_TRACK=inbound_track`
- `TELNYX_STREAM_BIDIRECTIONAL_TARGET_LEGS=self`
- `FAST_STATIC_GREETING=true`
- `VOICE_RUNTIME=custom`, `VOICE_RENDERER=cartesia`, `CARTESIA_SPEED=1`, `TELEPHONY_PROVIDER=telnyx`

All debug capture flags disabled. Production binary on VPS is PCMU. This baseline is locked and must remain untouched during the spike.

---

## 4. G722 Result (Already Tested)

- G722 is technically viable (main audio pipeline decoded correctly, all booking slots captured, no errors).
- G722 did not materially improve perceived voice quality (user-reported: "more or less the same").
- Marginal improvements: latency, mechanical sound, transcript quality.
- Telnyx comfort noise is present at the same level on both PCMU and G722.
- G722 remains available behind env flags but is not the default.

---

## 5. LiveKit Target Architecture

### Phase 1 — One-way audio proof (this spike)

```
Cartesia HD PCM (24 kHz)
  → Go publisher (experimental/livekit/publisher/)
    → LiveKit room (self-hosted Docker or LiveKit Cloud free tier)
      → Browser client (experimental/livekit/web-client/)
        → User hears HD voice through speakers
```

### Phase 2 — Two-way conversation proof (future, not this spike)

```
Browser microphone
  → LiveKit room
    → OpenAI Realtime (reasoning)
      → Cartesia HD PCM
        → LiveKit room
          → Browser speakers
```

### Phase 3 — PSTN bridge via LiveKit SIP (future, not this spike)

```
UK PSTN caller
  → Telnyx SIP trunk
    → LiveKit SIP service
      → LiveKit room
        → Agent (Go or Python)
          → OpenAI Realtime + Cartesia
            → LiveKit room
              → LiveKit SIP → Telnyx → PSTN caller (G.722 wideband)
```

**Phase 3 is out of scope for this spike.** Phase 1 is the only target.

---

## 6. Spike Scope (Minimal)

### In scope
- Deploy LiveKit server (Docker, self-hosted, or Cloud free tier).
- Go publisher that connects to a LiveKit room and publishes a Cartesia HD audio track.
- Cartesia API integration: stream HD PCM (pcm_s16le, 24 kHz) from Cartesia into the publisher.
- Opus encoding: publisher encodes PCM to Opus and publishes to LiveKit.
- Simple HTML/JS web client that connects to the room and plays the audio.
- Audio quality comparison: PCMU capture vs LiveKit HD capture.
- Latency measurement: time from Cartesia first byte to browser playback.
- Documentation of results.

### Explicitly out of scope
- Production phone calls (no Telnyx changes).
- Telnyx production webhook changes.
- Booking flow integration.
- OpenAI Realtime integration.
- Two-way conversation (browser microphone → OpenAI → Cartesia).
- LiveKit SIP trunk to Telnyx.
- Tenant dashboard, database, ResDiary, authentication.
- Replacing or modifying the production voice-gateway binary.
- Changing the PCMU production runtime.
- Changing the receptionist prompt, booking state, Cartesia voice ID, or OpenAI model.
- Any production deployment of LiveKit.

---

## 7. Infrastructure Options

### Option A — LiveKit Cloud (free tier)
- **Pros:** No infrastructure to manage, quick to set up, free tier includes enough minutes for spike testing.
- **Cons:** Requires internet connectivity, API key management, external dependency.
- **Setup time:** ~15 minutes.

### Option B — Self-hosted LiveKit server (Docker)
- **Pros:** Full control, no external dependency, can run on the same VPS or a separate dev box.
- **Cons:** Infrastructure to manage, need to expose WebSocket port, need TURN server for browser connectivity outside LAN.
- **Setup time:** ~1-2 hours (including TURN/STUN config).

### Option C — Local Docker (developer machine)
- **Pros:** Zero infrastructure, no network exposure, fast iteration.
- **Cons:** Only the developer can test, no external browser access.
- **Setup time:** ~15 minutes.

**Recommendation for spike: Option A (LiveKit Cloud free tier).** Fastest to set up, no infrastructure overhead, sufficient for proving the audio path. Can switch to self-hosted later if needed.

---

## 8. Required Env Vars (Spike Only, Not Production)

```
# experimental/livekit/publisher/.env (not committed)
LIVEKIT_URL=wss://your-project.livekit.cloud
LIVEKIT_API_KEY=your-api-key
LIVEKIT_API_SECRET=your-api-secret
CARTESIA_API_KEY=your-cartesia-key
CARTESIA_VOICE_ID=2f251ac3-89a9-4a77-a452-704b474ccd01
CARTESIA_MODEL=sonic-3.5
SPIKE_GREETING_TEXT=Hello, this is a LiveKit HD audio test from VoxLane.
```

**These are placeholder env names only.** No real secrets. The actual values are entered by the developer running the spike locally.

---

## 9. Security Considerations

- LiveKit API keys are scoped to the spike project only.
- No production credentials are used in the spike.
- The spike does not connect to the production Telnyx webhook, OpenAI Realtime, or Cartesia production config.
- The spike publisher runs as a standalone Go process, not inside the production voice-gateway.
- The web client is a single HTML file served locally or from a static host, not integrated into any production frontend.
- Token generation uses the LiveKit API key with a short TTL (1 hour) and room-scoped permissions.

---

## 10. Rollback / Safety

- The spike is on a feature branch (`feat/livekit-hd-spike`), not `main`.
- No production code is modified. The experimental directory (`experimental/livekit/`) is separate from `voice-gateway/`.
- No systemd services are created or modified.
- No production `.env` is modified.
- No production binary is rebuilt or deployed.
- **Rollback is simply deleting the experimental branch:** `git branch -D feat/livekit-hd-spike`.
- The PCMU production runtime on VPS is completely independent of the spike and continues to operate normally.

---

## 11. Success Criteria

1. **LiveKit room can be created** — either via LiveKit Cloud dashboard or self-hosted Docker.
2. **Browser client can connect** — simple HTML page with `livekit-client` connects to the room and receives the audio track.
3. **Cartesia HD PCM/Opus audio can be heard in browser** — the greeting text is generated by Cartesia at 24 kHz PCM, encoded to Opus, published to LiveKit, and played through the browser speakers.
4. **Audio quality is clearly better than PCMU phone path** — subjective comparison: the HD audio should be noticeably cleaner, wider frequency response, no narrowband "tinniness".
5. **Latency is measured** — time from Cartesia first byte to browser playback should be < 3 seconds (greeting latency).
6. **Production PCMU path remains unchanged** — no production binary rebuild, no production env change, no production service restart.
7. **Rollback is verified** — deleting the experimental branch has no effect on production.

---

## 12. Experimental Directory Structure

```
experimental/livekit/
├── README.md              # Overview, status, how to run
├── server-notes.md        # LiveKit server setup notes (Docker or Cloud)
├── publisher/
│   ├── go.mod             # Go module
│   ├── main.go            # Go publisher: Cartesia → Opus → LiveKit
│   ├── .env.example       # Placeholder env names (no real secrets)
│   └── README.md          # How to run the publisher
├── web-client/
│   ├── index.html         # Simple HTML page with livekit-client
│   ├── app.js             # Connect to room, play audio
│   └── README.md          # How to open in browser
└── results/
    └── (empty — populated after spike runs)
```

---

## 13. Recommended Next Implementation Step

**Step 1: Set up LiveKit Cloud project** (free tier)
- Create a LiveKit Cloud account
- Create a new project
- Note the `LIVEKIT_URL`, `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET`
- Generate a test token for a room

**Step 2: Scaffold the Go publisher** (`experimental/livekit/publisher/main.go`)
- Connect to LiveKit room using Go SDK
- Create an Opus audio track with `NewLocalSampleTrack`
- Implement a `SampleProvider` that reads PCM frames from Cartesia's WebSocket stream
- Use Cartesia's existing WebSocket streaming API (pcm_s16le, 24000 Hz, no transcoding)
- Publish the track to the room

**Step 3: Scaffold the web client** (`experimental/livekit/web-client/index.html`)
- Include `livekit-client` from CDN
- Connect to the LiveKit room with a token
- Subscribe to the audio track
- Attach to an `<audio>` element for playback

**Step 4: Run the spike**
- Start the Go publisher
- Open the web client in a browser
- Hear the Cartesia greeting in HD quality
- Measure latency, compare to PCMU

**Step 5: Document results** (`experimental/livekit/results/`)
- Audio quality notes
- Latency measurements
- Comparison with PCMU baseline
- Recommendation: proceed to Phase 2 (two-way) or stop

---

## 14. What Is NOT In This Spike

- No LiveKit SIP trunk to Telnyx.
- No OpenAI Realtime integration.
- No booking flow.
- No production deployment.
- No PCMU removal or replacement.
- No Twilio fallback removal.
- No production binary rebuild.
- No production env change.
- No production service modification.
- No receptionist prompt change.
- No Cartesia voice/model/speed change.
- No OpenAI model change.

---

## 15. References

- LiveKit docs: https://docs.livekit.io
- LiveKit Go SDK: https://pkg.go.dev/github.com/livekit/server-sdk-go
- LiveKit JS client: https://github.com/livekit/client-sdk-js
- LiveKit Cloud free tier: https://livekit.io/cloud
- Cartesia streaming API: https://docs.cartesia.ai
- Opus codec: https://opus-codec.org
- Voice quality strategy: `docs/context/VOICE_QUALITY_STACK_STRATEGY.md`
- PCMU baseline lock: commit `8c31bc6`
- G722 test result: commit `f48b869`
