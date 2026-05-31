# Telnyx Outbound Audio Debug

Date: 2026-05-31

## Official Contract Checked

Sources:

- Telnyx Programmable Voice media streaming docs: https://developers.telnyx.com/docs/voice/programmable-voice/media-streaming
- Telnyx Call Control streaming start API reference: https://developers.telnyx.com/api/call-control/start-call-streaming

## streaming_start Fields

Current backend sends:

```json
{
  "stream_url": "wss://.../stream/{call_control_id}",
  "stream_track": "both_tracks",
  "stream_bidirectional_mode": "rtp",
  "stream_bidirectional_codec": "PCMU",
  "stream_bidirectional_target_legs": "opposite"
}
```

Docs confirm these parameter names exist for Call Control streaming start.

Allowed values to verify against the API reference:

- `stream_track`: includes `inbound_track`, `outbound_track`, `both_tracks`
- `stream_bidirectional_mode`: includes `rtp`
- `stream_bidirectional_codec`: includes `PCMU`
- `stream_bidirectional_target_legs`: includes `self`, `opposite`, `both`

## Proven Mismatch

The gateway was sending outbound RTP packets as raw binary WebSocket messages.

Official media streaming docs describe bidirectional RTP playback as a WebSocket text JSON event. The API reference describes media as base64-encoded RTP payload/raw audio wrapped in JSON payloads; the media streaming docs also state inbound media payloads contain no RTP headers.

```json
{
  "event": "media",
  "media": {
    "payload": "base64-encoded RTP payload/raw audio"
  }
}
```

So the WebSocket frame format was wrong, and the 12-byte RTP packet header was not part of the documented `media.payload` contract. Telnyx was likely accepting the socket write while discarding the binary frame because it was not a valid media event.

## Fix Applied

`voice-gateway/internal/provider/telnyx/adapter.go` now:

- keeps raw PCMU bytes as the RTP payload for `stream_bidirectional_mode=rtp`
- wraps each payload chunk in a JSON `media` event
- base64 encodes the raw PCMU payload into `media.payload`
- sends the envelope as a WebSocket text message
- parses Telnyx JSON `media`, `start`, `stop`, `mark`, and `error` events

## Target Leg Matrix

Known PCMU test tone used for all rows:

- `stream_bidirectional_mode=rtp`
- `stream_bidirectional_codec=PCMU`
- WebSocket JSON text `media` events
- `media.payload` is base64 raw PCMU
- 160-byte frames with 20 ms pacing
- tone starts after stream readiness

| mode | target_legs | stream_track | payload format | caller heard audio | notes |
| ---- | ----------- | ------------ | -------------- | ------------------ | ----- |
| rtp | opposite | both_tracks | JSON media, base64 raw PCMU | no | Delayed test tone sent successfully, Telnyx accepted media events, caller heard silence. |
| rtp | self | both_tracks | JSON media, base64 raw PCMU | yes | Caller heard delayed beep tone after pickup. |
| rtp | both | both_tracks | JSON media, base64 raw PCMU | yes | Caller heard delayed beep tone after pickup. |

## Current Boundary

Telnyx outbound playback is proven with `stream_bidirectional_target_legs=self` and `both`.
The silent boundary was `stream_bidirectional_target_legs=opposite`.

Next validation should disable the debug tone and run the normal Cartesia path with a working target leg.
