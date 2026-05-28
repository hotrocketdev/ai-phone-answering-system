# Voice Stack Decision Matrix

Date: 2026-05-28  
Project: AI Voice Receptionist / VoxLane

## Current Options

| Stack | Audio Quality | Engineering Risk | UK Number Availability | Control | Recommendation |
|---|---:|---:|---:|---:|---|
| Twilio Media Streams + OpenAI + Cartesia | Medium/low | Low now | Good | High | MVP/fallback only |
| Cartesia managed number | Unknown/high potential | Medium | Currently likely limited | Lower | Test as spike |
| Twilio import into Cartesia | Unknown | Medium | Uses Twilio number path | Medium | Test when available |
| LiveKit + Telnyx + Cartesia | High potential | Medium/high | Telnyx approval friction | High | Best long-term candidate |
| LiveKit + Simwood + Cartesia | High potential | Medium/high | Good UK fit likely | High | Strong UK candidate |
| SignalWire + Cartesia | Medium/high potential | Medium | Unknown | Medium/high | Secondary evaluation |
| Deepgram Voice Agent | Works but mechanical | Low/medium | Depends telephony | Lower | Archive for now |
| OpenAI native voice | Too American | Low | N/A | Medium | Not production voice |

## Key Learning

The main issue is not the AI brain. The current issue is audio transport quality.

## Keep

- Go gateway
- NestJS business backend
- Next.js SaaS dashboard
- Cartesia voice renderer
- OpenAI conversation path for now
- provider abstraction

## Archive / Reduce

- Deepgram runtime
- old OpenAI voice playback path
- repeated audio conversion experiments
- test tone debug paths once diagnostics are documented
- stale prompt examples like Bella Roma

## Long-Term Preferred Stack

```text
LiveKit/SIP
+ Cartesia Sonic 3.5
+ OpenAI or Grok
+ NestJS tools
+ Next.js dashboard
```

## Future Grok/xAI Role

Grok/xAI should be evaluated after the transport issue is solved.

Do not evaluate Grok while Twilio Media Streams audio is still the dominant quality bottleneck.

Potential role:

```text
Grok = cheaper/faster conversation brain
Cartesia = voice
LiveKit/SIP = transport
```

## Production Readiness Principle

Do not go to production with a voice path that sounds noticeably mechanical or low-quality. The receptionist voice is the product.
