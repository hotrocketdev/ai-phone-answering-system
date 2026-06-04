# LiveKit Web Client — Spike

**Status:** Working — connects to a LiveKit room and plays subscribed audio tracks.

---

## How to use

1. **Generate a token** using the `token-gen` CLI (see `../token-gen/README.md`):
   ```bash
   cd ../token-gen
   go run . --room voxlane-hd-spike --identity voxlane-listener --subscribe
   ```
   Copy the JWT (long string starting with `eyJ…`) to the clipboard.

2. **Open `index.html`** in a modern browser (Chrome/Edge/Firefox).

3. **Paste the LiveKit URL** (e.g. `wss://ai-voice-assistant-314hy5b3.livekit.cloud`) and the **token** into the form.

4. **Click Connect.** The status should change to "Connected to room…" once the WebSocket is up.

5. **Start the publisher** in another terminal:
   ```bash
   cd ../publisher
   go run .
   ```
   The publisher will join the room, publish an audio track, and stream 5 seconds of test tone (or a Cartesia greeting if `CARTESIA_API_KEY` is set).

6. **Listen.** The browser will subscribe to the audio track and play it through the speakers. The audio meter shows the live level.

---

## What this client does

- Connects to a LiveKit room via WebSocket.
- Auto-subscribes to the first audio track published by another participant.
- Attaches the audio to an `<audio>` element with `autoplay` enabled.
- Renders a 32-bar audio level meter from an `AnalyserNode`.
- Logs all state changes and track events to the on-page log.

---

## What this client does NOT do

- Publish any audio from the browser (no mic capture — the spike is one-way).
- Use any production VoxLane code.
- Connect to a backend, tenant, or booking system.
- Store tokens or credentials in localStorage.
- Use any framework — vanilla HTML/JS + `livekit-client@2.5.7` from CDN.

---

## Files

- `index.html` — single-file web client (no build step, no backend).

---

## Troubleshooting

- **"Both URL and token are required"** — fill in both fields.
- **`Autoplay policy blocked audio.play()`** — click anywhere on the page first; modern browsers require a user gesture before audio can start.
- **`room.connect()` hangs** — check that the URL starts with `wss://` and that the token is from the same LiveKit project.
- **No audio** — the publisher must be running at the same time. Check the publisher's log for `track published`.
- **Meter shows nothing** — audio may be silent. The meter is RMS-derived; a quiet track shows near-zero bars.
