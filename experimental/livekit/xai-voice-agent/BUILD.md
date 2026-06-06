# xai-voice-agent

Isolated Go harness for the **Plan D spike** — xAI Grok Voice Agent API + Eve voice, bridged to a LiveKit room.

This is the leading candidate for the production VoxLane HD voice stack. See `docs/experimental/livekit-hd-spike/STAGE_1_5_COST_QUALITY_REPORT.md` §12 for the live status tracker.

## What this is

- A standalone Go binary (NOT a mode in the existing `conversation-worker`)
- WSS client to `wss://api.x.ai/v1/realtime?model=grok-voice-latest` (OpenAI Realtime API compatible)
- Bridges a LiveKit room (mic in, speaker out) to xAI's all-in-one voice stack
- Uses the same ffmpeg long-lived pipeline pattern as the main worker

## What this is NOT

- It is **not** production. The official LiveKit xAI plugin exists in Python and Node.js — for production, we will either port the worker to Python or write a minimal Go xAI plugin. This harness is the spike.
- It is **not** a replacement for the existing `conversation-worker`. The stitched mode (Stage 1, in prod) and `realtime-cartesia` mode (Stage 1.5, current spike) remain in place. This harness is additive.
- It does **not** touch production `.env`, prod gateway, prod systemd, or Telnyx prod webhook.

## Files

| File | Purpose |
|---|---|
| `main.go` | Entry point. Loads env, parses flags, dispatches to LiveKit bridge or smoke test. |
| `xai_client.go` | WSS client to xAI Voice Agent. Session config, audio input events, audio output events. |
| `xai_livekit.go` | LiveKit room connection + audio bridge (mic -> xAI, xAI -> speaker). |
| `xai_smoke.go` | Text-only smoke test (no LiveKit). `--no-livekit` flag. |
| `BUILD.md` | Build & run instructions (this file). |

## Quick build (Windows / Linux)

```bash
cd experimental/livekit/xai-voice-agent
go mod tidy
go build -o xai-voice-agent .
```

## Build for Linux (deploy to VPS)

```bash
cd experimental/livekit/xai-voice-agent
GOOS=linux GOARCH=amd64 go build -o xai-voice-agent-linux .
```

## Push to VPS

```bash
scp xai-voice-agent-linux my-vps:/tmp/xai-voice-agent
```

## Run on VPS

```bash
ssh my-vps

# Stop any existing worker (do not leave it running idle)
pkill -9 -f conversation-worker-stage1.5 || true

# Make sure XAI_API_KEY is in the spike .env
grep XAI_API_KEY /opt/ai-voice-receptionist/experimental/livekit/.env

# Start the harness
cd /opt/ai-voice-receptionist/experimental/livekit/xai-voice-agent
XAI_API_KEY=$XAI_API_KEY /tmp/xai-voice-agent
```

Or use a `.env` file:

```bash
# On the VPS, create xai-voice-agent.env with:
LIVEKIT_URL=wss://ai-voice-assistant-314hy5b3.livekit.cloud
LIVEKIT_API_KEY=APIVQCVnwDyXpAk
LIVEKIT_API_SECRET=...
XAI_API_KEY=xai-...
XAI_MODEL=grok-voice-latest
XAI_VOICE=eve
XAI_VAD_SILENCE_MS=1500
XAI_VAD_PREFIX_MS=300
XAI_VAD_THRESHOLD=0.7

# Then:
/tmp/xai-voice-agent --env xai-voice-agent.env
```

## Smoke test (no LiveKit)

Test the WSS client end-to-end without involving LiveKit. Useful as the first sanity check:

```bash
XAI_API_KEY=xai-... go run . --no-livekit

# In the terminal, type messages. Output audio is saved to xai-smoke-output.wav.
```

## Test in browser

1. Mint a 2h LiveKit token for the browser participant:

   ```bash
   cd C:\builds\AI-Phone-Answer-System\experimental\livekit\token-gen
   $env:LIVEKIT_API_KEY="APIVQCVnwDyXpAk"
   $env:LIVEKIT_API_SECRET="RKd65RHCDyXZMeam5CQ4wtEFeo7XrGGhw0W7ELl8eNdB"
   .\token-gen.exe --room voxlane-conv-spike --identity voxlane-browser-mic --ttl 2h
   ```

2. Open `file:///C:/builds/AI-Phone-Answer-System/experimental/livekit/web-client/two-way.html` in Chrome.

3. Paste the token. Set LiveKit URL to `wss://ai-voice-assistant-314hy5b3.livekit.cloud`.

4. Click Connect. Speak the 9-utterance test suite (see §12.2 of the manager report).

## Stop the harness

```bash
ssh my-vps "pkill -9 -f xai-voice-agent"
```

The harness is the spike. It is NOT production. Do not leave it running idle.
