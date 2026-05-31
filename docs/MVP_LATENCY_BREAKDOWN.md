# MVP Latency Breakdown

Rig: `VOICE_RUNTIME=custom`, `VOICE_RENDERER=cartesia`, Cartesia `sonic-3.5` → `pcm_mulaw 8000`, OpenAI `gpt-realtime-1.5`.

## First Greeting Latency

Measured from WebSocket open (gateway `Run()` entry) to first u-law frame reaching Twilio.

| Stage | Estimated ms | Notes |
| ----- | ------------ | ----- |
| OpenAI WS dial + TLS handshake | 200–400 | Depends on network; parallel with Cartesia greeting |
| OpenAI `session.created` | +50–100 | After WS connect |
| OpenAI `session.updated` | +20–50 | Config ACK |
| `response.create` (initial) | +0 | Sent immediately after session ready |
| OpenAI `text.delta` (first token) | +300–800 | Model inference latency |
| Text buffered (phrase boundary) | +100–300 | Cartesia waits for `.` or 100 chars |
| Cartesia WS connect + first chunk | +200–400 | TS to first audio |
| First u-law frame to Twilio | +0 | Immediately dequeued |

**Baseline total: 1200–2200 ms** (before fast static greeting)

### Optimisation: `FAST_STATIC_GREETING=true`

- Greeting text is known at compile time: `"Good evening, {BUSINESS_NAME}, how can I help?"`
- Cartesia greeting sent in a goroutine before OpenAI connects
- OpenAI session created in parallel (its latency is hidden)
- First OpenAI greeting is suppressed to avoid double-greeting

| Stage | Estimated ms | Notes |
| ----- | ------------ | ----- |
| Cartesia WS connect | 100–200 | Only Cartesia in path |
| First Cartesia chunk | +50–150 | TS → u-law |
| First u-law frame to Twilio | +0 | |

**Improved total: 150–350 ms** (~5x reduction)

## Follow-Up Reply Latency

Measured from `speech_stopped` to first u-law frame of reply.

| Stage | Estimated ms | Notes |
| ----- | ------------ | ----- |
| `speech_stopped` → `response.created` | 10–30 | OpenAI internal |
| `response.created` → first `text.delta` | 300–800 | Model inference |
| Phrase buffering (`.!?;:\n` at 20+ chars) | 100–400 | Cartesia waits for boundary |
| Cartesia render (first chunk) | 150–300 | TS to audio |
| First u-law frame to Twilio | +0 | |

**Baseline total: 600–1600 ms**

### Phrase Buffering Tuning

Current threshold: flush at `.` / `?` / `!` with ≥20 chars, or at ≥100 chars.

Tuning options:
- Reduce from 100 to 60 chars max buffer (faster first chunk, more chops)
- Add `,` and `;` as flush boundaries
- Stream immediately with 10-char min (risks choppy speech)

## Bottleneck Analysis

1. **OpenAI first-token latency** (300–800 ms) — biggest contributor, cannot be controlled
2. **Phrase buffering** (100–400 ms) — tunable but tradeoff speech quality vs speed
3. **Cartesia TS latency** (150–300 ms) — already optimised (`sonic-3.5`, `pcm_mulaw`)
4. **Network/ngrok** — adds <10 ms for local dev; negligible

## Frame Pacing

No pacer used. Frames sent as Cartesia produces them. Twilio accepts at wire speed.

## Verification

After instrumented call:
1. Check gateway logs for `LATENCY_STARTUP` and `LATENCY_TURN` lines
2. Greeting should log ~150-350ms `first_twilio_frame` from `ws_open`
3. Reply should log `first_twilio_frame` from `turn_start`
