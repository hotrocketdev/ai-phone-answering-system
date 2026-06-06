# 30-MIN PRODUCTION-STYLE xAI VOICE AGENT VALIDATION

## Goal
Run a 30-minute realistic mixed conversation against the xAI Voice Agent (Plan D)
via the existing LiveKit browser harness, log all metrics, and produce a pass/fail
report that gates production promotion.

## How it works
1. The harness (`xai-voice-agent`) joins a LiveKit room as a participant, with
   `xai-voice-agent-harness` identity.
2. The user opens `experimental/livekit/web-client/two-way.html` in a browser
   (Chrome/Edge), mics in as `voxlane-browser-mic`, and talks for 30 minutes.
3. Audio flows: browser mic → LiveKit room → harness OGG demuxer → xAI WSS Voice
   Agent → xAI Eve voice → harness OGG muxer → LiveKit room → browser speaker.
4. Every event the harness receives from xAI is logged with a `METRIC` prefix
   (`turn_start`, `turn_end`, `transcript`, `function_call`, `error`,
   `session_connect`, `session_end`).
5. After 30 min, Ctrl+C the harness and copy `/tmp/xai-voice-agent.log` to the
   `xai-voice-agent/` dir for post-processing.

## STEP 0 — Clean up old artifacts
```bash
ssh root@100.97.196.4 '
  pkill -9 -f xai-voice-agent || true
  pkill -9 -f "ffmpeg.*pipe:" || true
  rm -f /tmp/xai-voice-agent.log
  ls -la /tmp/xai-voice-agent /tmp/xai-voice-agent.env
'
```
Expected:
- "xai-voice-agent: No matching processes" (idempotent)
- `xai-voice-agent` exists, ~19 MB
- `xai-voice-agent.env` exists, 207 bytes (XAI key)

## STEP 1 — Push new binary
This Windows env cannot reach VPS via Tailscale. **User must copy the binary**.

The new Linux binary is built from current `xai_client.go` with structured
METRIC logging. Local path:

```
experimental/livekit/xai-voice-agent/xai-voice-agent-linux  (19.2 MB)
```

To push:
```bash
# From the user's local Windows box (after I build it):
scp experimental/livekit/xai-voice-agent/xai-voice-agent-linux \
    root@100.97.196.4:/tmp/xai-voice-agent.new

# On VPS:
ssh root@100.97.196.4 '
  mv /tmp/xai-voice-agent.new /tmp/xai-voice-agent
  chmod +x /tmp/xai-voice-agent
  /tmp/xai-voice-agent --version || /tmp/xai-voice-agent -h 2>&1 | head -5
'
```

Expected:
- Binary moved + chmod +x
- Either `--version` (if implemented) or usage banner shows our flags

## STEP 2 — Mint a 2h LiveKit token
On the user's local box:
```bash
cd experimental/livekit/token-gen
go run . --room voxlane-conv-spike --identity voxlane-browser-mic --ttl 2h
```
Expected: prints a JWT, ~280 chars, starts with `eyJhbGciOiJIUzI1NiIs...`

## STEP 3 — Open the browser
Browser: Chrome or Edge (WebRTC audio required).

URL (file:// — NOT Tailscale):
```
file:///C:/builds/AI-Phone-Answer-System/experimental/livekit/web-client/two-way.html
```

In the page:
- URL field is pre-filled: `wss://ai-voice-assistant-314hy5b3.livekit.cloud`
- Paste the fresh 2h token from STEP 2
- Click **Connect** → **Enable mic**

## STEP 4 — Start the harness (on VPS)
```bash
ssh root@100.97.196.4 '
  cd /tmp
  nohup /tmp/xai-voice-agent \
    --tools /opt/ai-voice-receptionist/experimental/livekit/xai-voice-agent/tools-booking.json \
    --env /tmp/xai-voice-agent.env \
    --room voxlane-conv-spike \
    --identity xai-voice-agent-harness \
    --voice eve \
    --model grok-voice-latest \
    --vad-silence-ms 1500 \
    --vad-prefix-ms 300 \
    --vad-threshold 0.7 \
    > /tmp/xai-voice-agent.log 2>&1 &
  echo "started pid $!"
  sleep 3
  tail -f /tmp/xai-voice-agent.log
'
```

Expected: first 10 log lines show:
```
xai-voice-agent starting:
  model=grok-voice-latest voice=eve
  VAD: silence=1500ms prefix=300ms threshold=0.70
  LiveKit: url=wss://... room=voxlane-conv-spike identity=xai-voice-agent-harness
METRIC session_connect ...
```

If you don't see `LiveKit:` or `METRIC session_connect` within 5s, Ctrl+C and check
network / token.

## STEP 5 — Run the 30-min scenario
The user talks for 30 minutes. The harness logs all events to
`/tmp/xai-voice-agent.log`. The user follows the script in
`THIRTY_MIN_SCENARIOS.md` (companion doc).

## STEP 6 — Stop the harness
```bash
ssh root@100.97.196.4 '
  pkill -INT -f xai-voice-agent
  sleep 2
  pkill -9 -f xai-voice-agent || true
  pkill -9 -f "ffmpeg.*pipe:" || true
  tail -50 /tmp/xai-voice-agent.log
'
```

Expected: `METRIC session_end turns=N function_calls=N transcripts=N errors=N duration_ms=N`
as the last meaningful line.

## STEP 7 — Pull the log
```bash
scp root@100.97.196.4:/tmp/xai-voice-agent.log \
    experimental/livekit/xai-voice-agent/logs/30min-2026-06-07.log
```

(Don't commit the log; it goes into a `logs/` dir that is .gitignore'd.)

## STEP 8 — Run the metrics analyzer
```bash
cd experimental/livekit/xai-voice-agent
go run ./cmd/analyze logs/30min-2026-06-07.log > report-30min-2026-06-07.md
```

(Analyzer is a small tool that grep/awk the METRIC lines and produce a
markdown report. To be built as part of this commit.)

## STEP 9 — Decision gate
- **PASS** if avg latency < 2.0s, no hallucinations, 100% function-call success on
  booking scenarios, no audio drops, accent consistent, all phone numbers captured.
- **FAIL** is classified A-G (see `XAI_FULL_STACK_VALIDATION.md`).

## What this does NOT touch
- Production PCMU worker (still on `realtime-cartesia`, killed earlier)
- Production .env
- Production systemd
- Telnyx production webhook
- Production gateway (pid 1796461)
- Tailscale
