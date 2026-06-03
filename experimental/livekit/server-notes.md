# LiveKit Server Setup Notes

**Date:** 2026-06-03
**Status:** Spike scaffold only — server not yet deployed

---

## Option A — LiveKit Cloud (free tier) — RECOMMENDED for spike

**Why:** Fastest to set up, no infrastructure overhead, sufficient for proving the audio path.

### Steps

1. Create a LiveKit Cloud account at https://cloud.livekit.io
2. Create a new project (e.g., "voxlane-hd-spike")
3. Note the project credentials:
   - `LIVEKIT_URL` (e.g., `wss://your-project.livekit.cloud`)
   - `LIVEKIT_API_KEY`
   - `LIVEKIT_API_SECRET`
4. Generate a test token for a room (via dashboard or Go SDK)

### Free tier limits

- 10,000 participant minutes / month
- Sufficient for spike testing
- Room-based, no per-room limits in free tier

### Security

- API keys are scoped to the spike project only.
- Generate short-lived tokens (1 hour TTL) for the web client.
- No production credentials used.

---

## Option B — Self-hosted LiveKit server (Docker)

**Why:** Full control, no external dependency, can run on the same VPS or a separate dev box.

### Steps

1. Install Docker on the host machine.
2. Run LiveKit server:
   ```bash
   docker run -d --name livekit-server \
     -p 7880:7880 \
     -p 7881:7881 \
     -p 7882:7882/udp \
     -e LIVEKIT_KEYS="devkey: devsecret0000000000000000000000000000" \
     livekit/livekit-server:latest \
     --dev
   ```
3. Expose ports 7880 (HTTP), 7881 (WebSocket), 7882/udp (RTP media).
4. Configure TURN server for browser connectivity outside LAN (optional for spike).

### Pros and cons

- **Pros:** Full control, no external dependency, can run on the same VPS or a separate dev box.
- **Cons:** Infrastructure to manage, need to expose WebSocket port, need TURN server for browser connectivity outside LAN.

---

## Option C — Local Docker (developer machine)

**Why:** Zero infrastructure, no network exposure, fast iteration.

### Steps

1. Install Docker locally.
2. Run the same Docker command as Option B.
3. Use `ws://localhost:7880` as the LiveKit URL.
4. Test from a browser on the same machine.

### Limitations

- Only the developer can test.
- No external browser access.

---

## Recommendation

**Use Option A (LiveKit Cloud free tier) for the spike.** Fastest to set up, no infrastructure overhead, sufficient for proving the audio path. Can switch to self-hosted later if needed.

---

## What is NOT needed for this spike

- LiveKit SIP service (separate Docker image: `livekit/sip`) — not needed for one-way audio proof.
- TURN/STUN server (for browser connectivity outside LAN) — can be deferred.
- Redis (used by LiveKit for multi-node deployments) — not needed for single-node spike.
- Production deployment, systemd services, nginx config — not needed for spike.
