# LiveKit Publisher — Spike Scaffold

**Status:** Scaffold only — actual Go code not yet written

---

## Purpose

Go publisher that connects to a LiveKit room and publishes a Cartesia HD audio track. This is the server-side component of the one-way audio proof.

---

## Planned Architecture

```
Cartesia WebSocket stream (pcm_s16le, 24 kHz)
  → Go SampleProvider (reads PCM frames)
    → LiveKit NewLocalSampleTrack (Opus codec)
      → Publish to LiveKit room
```

---

## Planned Dependencies

```go
// go.mod
module github.com/voxlane/livekit-spike-publisher

go 1.22

require (
    github.com/livekit/server-sdk-go v1.0.0
    github.com/livekit/protocol v1.0.0
)
```

---

## Planned Steps (when implemented)

1. Connect to LiveKit room using `livekit.ConnectToRoom()`
2. Generate access token using `livekit.NewAccessToken(apiKey, apiSecret)`
3. Create a `SampleProvider` that streams PCM frames from Cartesia
4. Create an Opus audio track using `livekit.NewLocalSampleTrack(trackName, provider, opts...)`
5. Publish the track to the room
6. Wait for browser client to connect
7. Stream the Cartesia greeting

---

## Env Vars (placeholder names only — no real secrets)

Copy `.env.example` to `.env` and fill in real values locally. Do NOT commit `.env`.

```
LIVEKIT_URL=wss://your-project.livekit.cloud
LIVEKIT_API_KEY=your-api-key
LIVEKIT_API_SECRET=your-api-secret
CARTESIA_API_KEY=your-cartesia-key
CARTESIA_VOICE_ID=2f251ac3-89a9-4a77-a452-704b474ccd01
CARTESIA_MODEL=sonic-3.5
SPIKE_GREETING_TEXT=Hello, this is a LiveKit HD audio test from VoxLane.
```

---

## How to Run (when implemented)

```bash
cd experimental/livekit/publisher
cp .env.example .env
# Edit .env with real values (NOT committed)
go mod tidy
go run main.go
```

Expected output:
- Connects to LiveKit room
- Streams Cartesia greeting
- Browser client hears HD audio

---

## Safety

- Runs as a standalone Go process, not inside the production voice-gateway.
- Does not connect to production Telnyx webhook, OpenAI Realtime, or Cartesia production config.
- Uses spike-scoped credentials only.
- No production binary rebuild.
