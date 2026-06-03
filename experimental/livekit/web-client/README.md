# LiveKit Web Client — Spike Scaffold

**Status:** Scaffold only — actual working client not yet written

---

## Purpose

Simple HTML/JS web client that connects to a LiveKit room and plays the audio track published by the Go publisher. This is the browser-side component of the one-way audio proof.

---

## Planned Architecture

```
Browser loads index.html
  → livekit-client connects to LiveKit room (with token)
    → Subscribes to audio track
      → Attaches to <audio> element
        → User hears HD audio through speakers
```

---

## Planned Steps (when implemented)

1. Include `livekit-client` from CDN: `https://unpkg.com/livekit-client@latest`
2. Generate a short-lived token (via LiveKit Cloud dashboard or a small token server)
3. Connect to the room: `room.connect(livekitUrl, token)`
4. Listen for track subscribed events
5. Attach the audio track to an `<audio>` element

---

## Token Generation

For the spike, generate a token via the LiveKit Cloud dashboard or use a small Go script. Token must be:

- Scoped to the spike room only
- Short-lived (1 hour TTL)
- Has permission to subscribe to audio tracks

**Do not hardcode tokens in the HTML file.** Tokens should be generated at runtime or entered via a form.

---

## How to Run (when implemented)

1. Open `index.html` in a browser
2. Enter the LiveKit URL and token
3. Click "Connect"
4. Hear the Cartesia greeting in HD

---

## Safety

- Single HTML file, no backend integration.
- No production frontend integration.
- No production credentials used.
