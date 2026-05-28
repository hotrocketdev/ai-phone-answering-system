# HD Audio Transport Notes

**Date**: 2026-05-28  
**Branch**: `spike/livekit-hd-audio`

---

## Why HD Audio Matters

The current Twilio Media Streams path uses G.711 µ-law at 8kHz. This codec:
- Has a frequency range of 300-3400 Hz (telephone narrowband)
- Removes all frequencies above 3.4kHz
- Compresses dynamic range

Cartesia Sonic 3.5 voices rendered at 44.1kHz in the playground sound natural and expressive. Over PSTN µ-law, they sound compressed and "mechanical." The same voice, same model — different transport.

## Codec Quality Progression

```
µ-law 8kHz (current)    → G.722 16kHz        → Opus 48kHz
├── 300-3400 Hz          ├── 50-7000 Hz        ├── 20-20000 Hz
├── 64 kbps              ├── 64 kbps           ├── 6-510 kbps
├── Narrowband           ├── Wideband          ├── Fullband
└── "Phone quality"      └── "HD Voice"        └── "Studio quality"
```

## LiveKit Audio Pipeline

LiveKit uses WebRTC internally, which negotiates the best available codec between participants:
- Browser → Opus (up to 48kHz stereo)
- SIP → G.722 or Opus (if SIP provider supports it)
- Agent worker → uncompressed PCM

The LiveKit SFU handles transcoding between codecs as needed. Cartesia audio arrives as PCM and is encoded into Opus/G.722 for the SIP caller automatically.

## Expected Quality Gains

| Scenario | Codec | Perceived Quality |
|----------|-------|-------------------|
| Current Twilio | µ-law 8kHz | Compressed, mechanical |
| SIP + G.722 | G.722 16kHz | Noticeably clearer, natural |
| SIP + Opus | Opus 24kHz | Near-playground quality |

## Latency Budget

| Component | µ-law Path | HD Path |
|-----------|-----------|---------|
| Twilio WS setup | ~200ms | N/A |
| SIP INVITE handshake | N/A | ~300ms |
| LiveKit room join | N/A | ~100ms |
| Cartesia TTS first byte | ~90ms | ~90ms |
| Codec encode/decode | ~20ms | ~30ms |
| Network round-trip | ~50ms | ~50ms |
| **Total** | **~460ms** | **~570ms** |

HD path adds ~110ms for SIP/LiveKit setup. Acceptable for voice conversations.

## Telnyx SIP Configuration

Telnyx supports HD voice codecs on SIP trunks:
- Opus (preferred)
- G.722
- G.711 (fallback)

SIP trunk setup requires:
1. Create SIP Connection in Telnyx Portal
2. Configure codec preference (Opus first)
3. Point to LiveKit SIP service IP
4. Assign UK number to connection

## Simwood Alternative

Simwood is a UK-focused SIP provider:
- Free UK geographic numbers
- G.722 support
- Lower per-minute costs than Telnyx
- UK data residency
- Smaller company, potentially less reliable than Telnyx

## Production Considerations

- SIP requires a static public IP (or FQDN) for the SIP service
- Firewall must allow SIP (5060/5061) and RTP ports
- TLS/SRTP for production security
- Redis required for LiveKit SIP session state
- Monitoring: SIP registration status, call success rate, audio quality MOS scores
