# LiveKit Publisher — Spike

**Status:** Working (PCMU intermediate). Opus HD is a follow-up.

---

## Purpose

Go publisher that connects to a LiveKit room and publishes a PCM audio track. Source is either:
- A Cartesia HTTP TTS call (when `CARTESIA_API_KEY` is set), or
- A 5-second 440 Hz sine wave test tone (fallback).

The spike proves the LiveKit Cloud end-to-end pipeline. It does **not** yet prove HD audio quality (PCMU is the codec, not Opus).

---

## Architecture

```
Cartesia HTTP TTS  (api.cartesia.ai/tts/bytes, pcm_s16le 8 kHz)
   OR
local 5s 440 Hz sine  (math.Sin in main.go)
  → 16-bit mono PCM
    → PCMSampleProvider  (20ms G.711 µ-law frames)
      → LiveKit track (PCMU codec)
        → LiveKit room (wss://...)
          → Browser (livekit-client@2.5.7)
```

---

## Dependencies (pinned for Go 1.26 compat)

- `github.com/livekit/server-sdk-go v1.0.16`
- `github.com/livekit/protocol v1.9.5`
- `github.com/pion/webrtc/v3 v3.2.44`
- `github.com/joho/godotenv v1.5.1`

Newer SDK versions have dependency conflicts with Go 1.26.3; see the spike results README for details.

---

## Env Vars

Copy `.env.example` to `../.env` (the spike root) and fill in real values locally. The publisher loads the parent `experimental/livekit/.env` via godotenv.

| Var | Required | Description |
|---|---|---|
| `LIVEKIT_URL` | yes | LiveKit Cloud (or self-hosted) wss:// URL |
| `LIVEKIT_API_KEY` | yes | From LiveKit Cloud dashboard |
| `LIVEKIT_API_SECRET` | yes | From LiveKit Cloud dashboard |
| `CARTESIA_API_KEY` | no | If set, synthesize greeting via Cartesia |
| `CARTESIA_VOICE_ID` | no | Default: `2f251ac3-89a9-4a77-a452-704b474ccd01` (production Alex voice) |
| `CARTESIA_MODEL` | no | Default: `sonic-3.5` |
| `SPIKE_GREETING_TEXT` | no | Default: `"Porto Douro Restaurants, Alex speaking. How can I help?"` |
| `SPIKE_ROOM` | no | Default: `voxlane-hd-spike` |
| `SPIKE_IDENTITY` | no | Default: `voxlane-publisher` |
| `SPIKE_WAIT_FOR_SUBSCRIBER` | no | If `true`, wait for a listener to join before publishing (default: `false`) |

---

## How to Run

### Default one-shot (publishes immediately, plays 5s, exits)

```bash
cd experimental/livekit/publisher
go run .
```

### Wait-for-subscriber mode (browser-friendly)

```bash
cd experimental/livekit/publisher
SPIKE_WAIT_FOR_SUBSCRIBER=true go run .
```

The publisher joins the room and waits. When a remote participant joins (e.g. the browser), it starts publishing and playing. The first listener triggers the play.

### Build and run binary

```bash
go build -o publisher.exe .
./publisher.exe
```

---

## What it does

1. Loads `experimental/livekit/.env` (gitignored) via godotenv.
2. Generates a LiveKit JWT for the configured room + identity (1-hour TTL).
3. Connects to LiveKit Cloud via WebSocket.
4. Optionally waits for a listener (if `SPIKE_WAIT_FOR_SUBSCRIBER=true`).
5. Synthesizes audio: Cartesia HTTP TTS (if `CARTESIA_API_KEY` set) or local 5s 440 Hz sine.
6. Encodes PCM to PCMU in 20ms frames via `PCMSampleProvider`.
7. Creates a LiveKit audio track with PCMU codec.
8. Publishes the track to the room.
9. Streams 5s of audio (or however long the Cartesia response is).
10. Exits cleanly.

---

## Tests

```bash
cd experimental/livekit/publisher
go test -v ./...
```

5 unit tests for the G.711 µ-law encoder:
- `TestLinearToMulawDeterministic` — same input always produces same output.
- `TestLinearToMulawSymmetry` — positive/negative of same magnitude differ only in the sign bit.
- `TestPCMSampleProviderFrameSize` — 8000 Hz, 20ms = 160 samples per frame.
- `TestPCMSampleProviderRejectsNon8k` — non-8000 Hz returns error.
- `TestPCMSampleProviderEmitsEOF` — 1 second of audio = 50 frames + EOF.

All pass.

---

## Safety

- Runs as a standalone Go process, not inside the production voice-gateway.
- Does not connect to production Telnyx webhook, OpenAI Realtime, or Cartesia production config.
- Uses spike-scoped LiveKit Cloud credentials only.
- No production binary rebuild, no production env change, no production service restart.

---

## Files

- `main.go` — entry point, room connection, audio orchestration
- `cartesia.go` — Cartesia HTTP TTS client
- `pcmsampleprovider.go` — G.711 µ-law sample provider
- `pcmsampleprovider_test.go` — unit tests
- `go.mod`, `go.sum` — pinned dependencies
- `.env.example` — placeholder template (committed)
- `publisher.exe` — compiled binary (not committed; build with `go build`)
