# Browser Audio Test Runbook — LiveKit HD Spike

**Date:** 2026-06-03 (PCMU), 2026-06-04 (Opus)
**Branch:** `feat/livekit-hd-spike`
**Scope:** End-to-end browser subscription verification for the LiveKit room. Covers both PCMU and Opus paths.

---

## TL;DR

You (the user) need to:
1. Open a browser to `experimental/livekit/web-client/index.html` (or `http://localhost:8765/index.html`).
2. Generate a listener token with the `token-gen` CLI.
3. Paste the URL + token into the browser, click Connect.
4. Start the publisher with `SPIKE_AUDIO_CODEC=pcmu` (original) **or** `SPIKE_AUDIO_CODEC=opus` (HD).
5. Listen for the 5-second 440 Hz test tone.

**PCMU** proves the LiveKit pipeline. **Opus** is the HD follow-up that proves the architecture can carry near-human voice quality.

---

## Prerequisites

- `experimental/livekit/.env` must exist with:
  - `LIVEKIT_URL=wss://ai-voice-assistant-314hy5b3.livekit.cloud`
  - `LIVEKIT_API_KEY=…`
  - `LIVEKIT_API_SECRET=…`
  (Set up by the spike earlier. Confirm with `cat experimental/livekit/.env` and check no values are `<set>`.)
- Go 1.22+ available (`go version`).
- A modern browser with WebRTC support: Chrome 90+, Edge 90+, Firefox 88+, Safari 14+.
- The browser machine must be on a network that allows outbound WebSocket (port 443) and UDP (port 50000-50100) to LiveKit Cloud.

---

## Step-by-step

### Step 0 — Choose a codec

The publisher supports two codecs via `SPIKE_AUDIO_CODEC`:

| Codec | Quality | Frequency response | Pipeline | When to use |
|---|---|---|---|---|
| `pcmu` (default) | Narrowband | ~3.4 kHz | Pure Go, no deps | Original spike baseline |
| `opus` (HD) | Wideband / fullband | up to 20 kHz | ffmpeg child process | HD voice test |

**For HD verification (Opus), use `SPIKE_AUDIO_CODEC=opus`.** The browser's `livekit-client@2.5.7` library supports Opus natively, so the web client does not change.

### Step 1 — Start the web client

Open `experimental/livekit/web-client/index.html` directly in a browser (e.g. drag-and-drop into Chrome, or right-click → Open With).

**Note on `file://` URLs:** The page imports `livekit-client@2.5.7` from a CDN. Browsers may block ESM imports from `file://`. If you see a CORS or import error, serve the file from a local HTTP server:

```bash
cd experimental/livekit/web-client
python -m http.server 8765
# Then open http://localhost:8765/index.html
```

The page will load and show "Not connected" status. The log will show:
```
[03:55:00.000] LiveKit client loaded (livekit-client@2.5.7)
[03:55:00.000] prefill example token length=0 (paste your own)
```

### Step 2 — Generate a listener token

Open a terminal and run:

```bash
cd experimental/livekit/token-gen
go run . --room voxlane-hd-spike --identity voxlane-listener --subscribe
```

This produces a 385-character JWT on stdout. Copy the entire output (no trailing newline).

**Sanity check:** the token starts with `eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9`. If it doesn't, your `.env` is missing the API key/secret.

### Step 3 — Paste the URL and token into the browser

In the web client form:
- **URL:** `wss://ai-voice-assistant-314hy5b3.livekit.cloud`
- **Token:** paste the JWT from step 2

Click **Connect**.

The status should change to "Connecting…" → "Connected to room: voxlane-hd-spike". The log should show:
```
[…] connecting url=wss://… token=eyJhbGci...(len=385)
[…] connected to room "voxlane-hd-spike" as "voxlane-listener"
[…] waiting for audio track from publisher...
```

**Autoplay note:** Most browsers require a user gesture before audio can start. The button click counts as a gesture. If audio.play() fails with "Autoplay policy blocked", click anywhere on the page (even empty space) to satisfy the gesture requirement, then re-click Connect.

### Step 4 — Start the publisher

**PCMU spike (default, original test):**
```bash
cd experimental/livekit/publisher
go run .              # or: SPIKE_WAIT_FOR_SUBSCRIBER=true go run .
```

**Opus HD spike (HD follow-up):**
```bash
cd experimental/livekit/publisher
SPIKE_AUDIO_CODEC=opus SPIKE_WAIT_FOR_SUBSCRIBER=true go run .
```

**Expected output (PCMU spike):**
```
token generated (room=voxlane-hd-spike identity=voxlane-publisher ttl=1h)
connected to room=RM_… as voxlane-publisher
spike_audio_codec=pcmu
falling back to 5s 440 Hz test tone at 8 kHz
track published: id=TR_… name= mime=
spike complete in 5.19s
```

**Expected output (Opus spike):**
```
token generated (room=voxlane-hd-spike identity=voxlane-publisher ttl=1h)
connected to room=RM_… as voxlane-publisher
spike_audio_codec=opus
ffmpeg_started=true pid=…
opus_header version=1 channels=1 input_rate=48000 pre_skip=312 gain=0 mapping_family=0
ogg_demuxer_ready (OpusHead + OpusTags consumed)
track published: id=TR_… name= mime=
spike complete in 5.23s
```

**On the VPS, use the prebuilt binary** instead of `go run`:
```bash
cd /opt/ai-voice-receptionist/experimental/livekit/publisher
SPIKE_AUDIO_CODEC=opus SPIKE_WAIT_FOR_SUBSCRIBER=true ./publisher-codec.bin
```

**Default behavior (one-shot):** the publisher immediately starts publishing the 5-second test tone. The browser should subscribe and start playing.

**Wait-for-subscriber mode:** if you want the publisher to wait until the browser has connected, set `SPIKE_WAIT_FOR_SUBSCRIBER=true` in `experimental/livekit/.env` and re-run. The publisher will:
1. Connect to the room
2. Wait for any remote participant to join
3. THEN start publishing and streaming

This is useful if you need a few seconds to set up the browser.

Expected publisher log:
```
[…] token generated (room=voxlane-hd-spike identity=voxlane-publisher ttl=1h)
[…] participant connected: voxlane-publisher (sid=PA_…)
[…] "level"=0 "msg"="ICE connected" …
[…] connected to room=RM_… as voxlane-publisher
[…] participant connected: voxlane-listener (sid=PA_…)         ← browser joined
[…] no CARTESIA_API_KEY — falling back to 5s 440 Hz test tone at 8 kHz
[…] "level"=0 "msg"="published track" …
[…] track published: id=TR_… mime=
[…] audio playback complete
[…] spike complete in 5.21s (audio: 5.00s)
```

### Step 5 — Confirm the browser subscribed

In the browser log, look for:
```
[…] participant connected: voxlane-publisher
[…] track subscribed: audio TR_…
[…] audio attached and play() invoked
[…] audio meter started
```

The audio meter (32 green bars below the audio element) should pulse during playback. If the bars are flat at the bottom, the audio is silent or the meter is broken.

### Step 6 — Confirm you heard the tone

You should hear a 5-second 440 Hz sine wave (musical note A4, in the middle of a piano). The tone is generated locally in the publisher (`generateTone()` in `main.go`) — no Cartesia is involved.

If you hear the tone, **the LiveKit pipeline is verified end-to-end.** Record:
- The room SID (in the publisher log, e.g. `RM_…`).
- The track SID (e.g. `TR_…`).
- The participant SIDs of both the publisher and listener.
- The browser log output.
- The publisher log output.
- Whether you heard the tone clearly.
- Any latency, choppiness, or distortion.

### Step 7 — Tear down

The publisher exits after the 5-second tone completes. The browser can be closed. The LiveKit room becomes empty and is closed by LiveKit Cloud automatically (no manual cleanup needed).

---

## Troubleshooting

### Browser: "Both URL and token are required"
Both fields must be filled. The token field clears if you reload the page; you need to re-paste it.

### Browser: `room.connect()` hangs or errors with "could not establish pc connection"
- The browser cannot reach LiveKit Cloud over UDP. Check firewall, corporate proxy, or VPN.
- LiveKit Cloud uses UDP port 50000-50100 for media. TURN fallback (TCP 443) is automatic but slower.
- Try a different network (e.g. mobile hotspot) to rule out corporate network restrictions.

### Browser: `audio.play() rejected: Autoplay policy blocked`
- Click anywhere on the page first, then re-click Connect.
- Or: open the browser's DevTools (F12), go to Console, and look for the exact error. Some browsers require a fresh user gesture on every `play()` call.

### Browser: connected but no audio track event fires
- The publisher is not in the room. Start it (step 4).
- The publisher joined a different room. Check the `--room` flag matches in both `token-gen` and `publisher`.
- The token is for a different room. Re-generate the token with the correct `--room` flag.

### Publisher: "no CARTESIA_API_KEY — falling back to 5s 440 Hz test tone"
This is expected. The test tone is sufficient to verify the pipeline. To use Cartesia, set `CARTESIA_API_KEY` in `experimental/livekit/.env`.

### Publisher: connects but no participant connected event
- Wait a few seconds. The OnParticipantConnected callback is called from the signaling goroutine and may be delayed.
- Check the publisher log for "connected to room" — once you see that, the room is up.

### Publisher: ICE fails
- UDP blocked. Try `webrtc.SettingEngine.SetNetworkTypes(["udp4", "tcp4"])` to force TCP. (Not yet implemented in this spike.)

### Audio heard but choppy
- Network jitter. LiveKit's jitter buffer absorbs this. If persistent, check your network latency to `ai-voice-assistant-314hy5b3.livekit.cloud`.

### Audio heard but distorted / static
- Possible codec mismatch. The publisher is using PCMU. The browser should auto-negotiate. Check the browser log for the actual codec used.

### Nothing heard, no errors
- Check the browser's system volume and the tab's volume (Chrome's per-tab volume control is in the tab's audio icon).
- Check the browser's audio output device (system preferences).
- Open the browser's `chrome://media-internals` and verify a stream is active.

---

## What this test does NOT prove

- ❌ **HD quality.** This is PCMU. The whole point of the spike is to eventually swap to Opus. Until the publisher uses Opus, the browser will hear narrowband audio (8 kHz, ~3.4 kHz frequency response).
- ❌ **Cartesia integration.** The test tone is generated locally. The Cartesia HTTP client is implemented but unverified (no API key in this spike).
- ❌ **Production voice.** This is a 440 Hz sine wave, not "Porto Douro Restaurants, Alex speaking. How can I help?".

## What this test DOES prove

- ✅ **End-to-end LiveKit pipeline.** Cartesia (or local) PCM → PCMU encoder → LiveKit room → browser.
- ✅ **Token auth.** Browser can join the room with a JWT.
- ✅ **Track publish/subscribe.** Server SDK publishes, browser SDK subscribes.
- ✅ **Codec compatibility.** PCMU is the lowest-common-denominator; if it works, Opus will too.
- ✅ **Network path.** Browser can reach LiveKit Cloud over the public Internet.

---

## After successful browser test

The next iteration should:
1. Replace PCMU with Opus in the publisher (HD).
2. Test with Cartesia synthesis (real voice, not test tone).
3. Compare subjective quality with PCMU phone path.
4. Decide whether to proceed to Phase 2 (two-way conversation).

See `docs/experimental/livekit-hd-spike/RESULTS_README.md` section "Opus/HD Follow-Up Plan" for details.
