# VoxLane Architecture — Critical Review

**Reviewer**: Principal SaaS Architect / Technical Cofounder  
**Date**: May 2026  
**Verdict**: V1 has solid bones. OpenAI Realtime pricing makes the unit economics unworkable. Fix the cost structure or the business dies.

---

## Executive Summary

The architecture is well-thought-out in most dimensions. State machine, tool call isolation, anti-hallucination guardrails, graceful degradation, GDPR handling, and multi-tenancy are all production-grade. The engineering decisions around Go for the voice engine, Redis for session state, BullMQ for async work, and deferring Kubernetes to Phase 3 are correct.

**The fatal problem is the unit economics.** At £0.47/call (actual, from Section 16), the Starter plan at £149/month for 300 calls yields £8 gross margin per customer. The Growth plan at £299 for 800 calls is underwater at full utilisation. You cannot build a SaaS business on single-digit-percent margins per account when customer acquisition cost for restaurant B2B sales is £500–2,000 and pre-Series-A CAC payback must be under 12 months.

The £0.04/call prominently displayed on the cover page is deeply misleading. It appears to be the Twilio-only component. The real per-call cost is **£0.47–0.52 (11–13× higher)**. This needs fixing before any investor, cofounder, or customer sees this document.

---

## 1. Unit Economics — The Kill Shot

### Actual per-call cost breakdown (3-minute average call)

| Component | Unit Cost | Per Call | % of Total |
|---|---|---|---|
| OpenAI Realtime input audio | $0.06/min | $0.18 | 30% |
| OpenAI Realtime output audio | $0.24/min | $0.36 | 60% |
| Twilio inbound | $0.0085/min | $0.025 | 4% |
| Twilio Media Streams | $0.004/min | $0.012 | 2% |
| Twilio SMS | $0.0079/SMS | $0.008 | 1% |
| Supabase + Compute | ~$0.003 | $0.003 | <1% |
| **Total** | | **~$0.59 = ~£0.47** | 100% |

### Plan profitability at 100% utilisation

| Plan | Price | Calls | Cost @ £0.47 | Profit | Margin |
|---|---|---|---|---|---|
| Starter | £149 | 300 | £141 | **£8** | **5.4%** |
| Growth | £299 | 800 | £376 | **−£77** | **−25.7%** |
| Scale | £549 | 2,000 | £940 | **−£391** | **−71.2%** |

The Growth and Scale plans lose money at full utilisation. The overage pricing (£0.55–0.65/call) at least covers costs, but the base plans are structurally broken. Growth plan users who exceed 800 calls at £0.55 overage push blended margin positive — but the business model cannot depend on customers going over their limits.

**You are effectively selling £141 of AI compute for £149 on Starter. That's a commodity reseller margin — not a SaaS business.**

### What the document claims

Section 16 states:
> "Growth — ~27% net plan" and "Scale — ~40% blended"

These numbers are incompatible with the stated per-call costs unless the per-call cost drops dramatically at scale. Nothing in the architecture suggests that it does. OpenAI does not offer volume discounts on the Realtime API.

---

## 2. The OpenAI Realtime API Problem

### Cost dominance is structural

Output audio at $0.24/minute is **91% of total AI cost** and **60% of total call cost**. Every second the AI speaks costs 0.4 cents. The architecture correctly identifies this but underestimates its severity.

**The core product value — "warm, natural, premium conversation" — is directly at war with the cost structure.** Natural conversation requires the AI to speak. Every word costs money. Truncating responses to control costs makes the assistant sound rushed, robotic, or impatient — destroying the premium experience.

### Vendor lock-in is existential

There is no alternative to OpenAI Realtime API. If OpenAI:
- Deprecates it (it's still in preview)
- Raises prices 5–10×
- Changes the API format
- Goes down for extended periods

...the entire voice product dies. There is no migration path. The architecture document does not address this.

The "fallback TTS" mentioned in failure handling is not a real fallback — it cannot handle conversations, cannot use tools, cannot manage state. It is a dead-end error message.

### Latency budget assumes best-case

The 450–720ms target (Section 2.4) assumes OpenAI processes in 250–400ms. Real-world Realtime API latency under load is 400–800ms. Add Twilio jitter, network variability, and resampling overhead, and the p95 first-response latency could exceed 1.2 seconds easily. That's noticeable silence on a phone call.

---

## 3. Audio Transcoding — A Hidden Risk

Section 5 describes the audio pipeline: µ-law 8kHz ↔ PCM 24kHz with "linear interpolation" and "zero-copy buffers."

### Linear interpolation will damage voice quality

Upsampling 8kHz → 24kHz by 3× using linear interpolation introduces aliasing artifacts and high-frequency distortion. Professional audio resampling uses polyphase filters or Lanczos interpolation. Linear interpolation may be fast, but it will make voices sound slightly metallic/tinny — exactly the "robotic" quality you're trying to avoid.

This needs either:
- A proper resampling library (libsamplerate / Secret Rabbit Code via CGo, or a pure Go equivalent)
- Or skip the resampling entirely if OpenAI exposes a way to specify 8kHz input/output audio format

### The double transcode

Audio goes through: **PSTN → µ-law → PCM(8kHz) → PCM(24kHz) → OpenAI → PCM(24kHz) → PCM(8kHz) → µ-law → PSTN**

That's two lossy conversions (µ-law encodes are lossy) and two resampling steps. Each step degrades quality. The net effect will be noticeable on good phone connections.

---

## 4. Pricing Model — Redesign Required

### What must change

The per-call cost (£0.47) and per-call margins are the existential issue. Three approaches, in order of recommendation:

#### Option A: Hybrid Architecture (Recommended for MVP)

Use OpenAI Realtime API ONLY for Greeting and Closing states. For the mid-call conversation (Booking, ModifyBooking, FAQ), use a cheaper approach:

1. **OpenAI Realtime** for initial greeting and intent detection (first 20–30 seconds)
2. **DeepSeek V4 text + TTS** for booking collection and data-gathering conversation (2–3 turns, 1–2 min)
3. **OpenAI Realtime** for booking confirmation and closing (final 30 seconds)

This cuts OpenAI Realtime usage from ~3 min to ~1 min per call, reducing AI cost from $0.54 to ~$0.18. Total per-call cost drops from £0.47 to ~£0.20–0.25.

The latency hit from the STT→LLM→TTS chain (200–500ms extra) is acceptable during the mid-call data-collection phase where the caller has already stated their intent and the conversation follows a predictable pattern.

**Preserves**: Natural greeting and closing (where warmth matters most).  
**Compromises**: Slightly more latency during booking data collection (acceptable tradeoff).

#### Option B: Raise Prices

If you must stay fully realtime:
- Starter: £249/month for 300 calls
- Growth: £549/month for 800 calls
- Scale: £999/month for 2,000 calls

These prices produce 40%+ margins at full utilisation. The question is whether restaurants will pay £249/month. A single missed booking can be worth £30–120; 300 successfully answered calls pay for the service many times over. The value prop supports these prices — test willingness to pay.

#### Option C: The "Concierge Speech Model"

Train the AI to guide callers toward efficient booking. Instead of open-ended conversation:
- "I can check availability for you. What date and time works best, and for how many people?"
- Collect all fields upfront before any tool calls
- One availability check, one booking creation, confirmed in 90 seconds

This doesn't change the per-minute cost but reduces average call duration from 3 min to ~2 min, reducing cost to ~£0.31/call. Coupled with a £249 Starter price, margin hits ~38%.

### The real numbers for a SaaS P&L

Assume 100 Starter customers at £249/month:

| Line Item | Monthly |
|---|---|
| Revenue (100 × £249) | £24,900 |
| AI + Telephony (100 × 200 calls × £0.25) | −£5,000 |
| Infrastructure (VPS + Supabase + monitoring) | −£500 |
| **Gross Profit** | **£19,400 (78%)** |
| Support (1 FTE) | −£3,500 |
| Sales (1 FTE + commissions) | −£5,000 |
| Engineering (founders) | −£0 (pre-revenue) |
| **Operating Profit** | **£10,900 (44%)** |

This is a real SaaS business. The current model at £149/£0.47 is not.

---

## 5. Architecture: What's Solid

### State Machine & Anti-Hallucination
The 8-state design with Go-enforced transitions is excellent. The tool-call-as-truth-source pattern prevents hallucination. The rule "AI cannot confirm booking without create_booking tool returning success" is the single most important architectural constraint in the system. Do not change this.

### Tool Call Architecture
HMAC-signed tool calls from Go → NestJS, state-scoped tool availability, max 10 tools/call limit, structured error responses with alternatives — all correct.

### Session Management
Redis-backed session state with TTL, sticky sessions correctly identified as critical scaling constraint, 30-minute hard TTL. Well-designed.

### Queue Architecture
BullMQ + Redis is appropriate. SMS with 5s delay post-call is thoughtful. Dead letter handling with escalation to dashboard tasks is practical.

### GDPR / Privacy
Comprehensive. Encryption at rest for PII, lawful basis documented, SAR and erasure endpoints, call recording opt-out, data residency in EU/UK, retention policies defined. Good.

### Graceful Degradation
Circuit breakers on ResDiary, fallback behaviour for OpenAI outage, synthetic call tests every 15 minutes — all solid.

### Infrastructure Pragmatism
Deferring Kubernetes to Phase 3 (month 18+) is the right call. Docker Compose on a single VPS for MVP is sufficient. The scaling triggers are reasonable.

---

## 6. Architecture: What's Missing or Weak

### 1. OpenAI Session Costs Not Tracked Per-Call

The architecture tracks `ai_tokens_used` in call_sessions but doesn't specify how OpenAI Realtime API costs are retrieved. The Realtime API session pricing is based on audio minutes, not tokens. The cost tracking needs to record:
- `ai_input_audio_seconds`
- `ai_output_audio_seconds`
- `ai_text_tokens`

Without this, per-tenant profitability cannot be accurately measured, and the pricing model has no cost feedback loop.

### 2. No Mention of OpenAI Realtime API Rate Limits

OpenAI imposes rate limits on the Realtime API. Concurrent sessions are typically capped (often 50–100 for beta). The architecture claims "10k concurrent calls (K8s scale)" but this requires OpenAI to support that many concurrent Realtime API sessions. They don't currently. This is a hard ceiling you can't scale past.

### 3. WebSocket Reconnection Logic Is Absent

Section 10 mentions TTL-based cleanup and Twilio disconnect handling, but there's no reconnection strategy. If the OpenAI WebSocket drops mid-call (it happens), the architecture has no path to reconnect and resume. The "OpenAI session reconnect" retry strategy in Section 21 mentions 3 attempts, but doesn't describe HOW state is recovered after reconnection — does the conversation history get replayed? Does the caller hear silence while this happens?

### 4. Sticky Sessions Are Under-Explored

Section 10 correctly identifies that Twilio Media Stream WebSockets must pin to a specific Go instance. In production at scale, this is a significant operational burden. Twilio sends to one IP — if that Go instance is overloaded, crashes, or is being drained for deployment, calls drop. The document mentions "consistent hashing on CallSid" for L4 load balancing, but Twilio connects to a specific URL, not through your load balancer's decision. The real architecture must handle:
- Node failure mid-call (call is lost — can this be recovered?)
- Graceful drain during deployments (30s drain may not be enough for active calls)
- Capacity-based routing (send new calls to least-loaded Go instance)

### 5. No External Monitoring of OpenAI Realtime API Health

The synthetic call test (every 15 min) is good, but it tests the entire pipeline. There's no dedicated health check that specifically verifies: "Can we open a new OpenAI Realtime WebSocket right now?" This is different from "is the OpenAI API up?" — the Realtime API has independent availability characteristics.

### 6. Prompt Architecture Underestimates Token Usage

Section 25 estimates 1,800–2,500 tokens per 4-turn call. For the Realtime API, audio tokens are priced differently from text tokens. But the tool schemas (60–80 tokens each) and tool results (150–300 tokens each) add up fast. In a booking call with check_availability → create_booking → send_sms_confirmation, that's 3 tool calls × 250 tokens avg = 750 tokens just in tool result context. Plus 4 conversation turns. Real token usage for a booking call is likely 3,000–4,500 tokens, not 1,800–2,500.

### 7. The Engineering Team Requirement Is Unstated

Building this requires:
- 1 Go engineer (WebSocket, audio, realtime systems)
- 1 NestJS/TypeScript backend engineer
- 1 Next.js frontend engineer
- 1 DevOps/infra engineer
- 1 person doing Twilio integration, ResDiary partnership, telecom testing

That's a 4–5 person engineering team minimum, plus a product/design person, plus sales for restaurant B2B. The document implies this is buildable by a small team but the technology breadth (Go + TypeScript + Next.js + Docker + Redis + K8s + Twilio + OpenAI + ResDiary) requires senior generalists or a larger team — both expensive.

### 8. ResDiary as Sole Integration = Fragile

Building MVP around a single booking platform is fine for go-to-market, but the architecture should explicitly plan for the second integration (OpenTable, Resy, or SevenRooms) and budget for it. If ResDiary changes their API, restricts access, or gets acquired, your entire booking flow breaks for all customers.

---

## 7. Specific Technical Critiques

### Go Audio Pipeline

```go
// Upsample 8kHz → 24kHz (3:1 linear interpolation)
pcm24k := upsample8to24(pcm8k)
```

**Problem**: Linear interpolation is not adequate for audio upsampling. Use a polyphase FIR filter. Implement via `github.com/mjibson/go-dsp` or CGo bindings to libsamplerate. This will add ~2–5ms latency per chunk — negligible at 20ms chunk sizes.

### Twilio Media Stream <> OpenAI Format Mismatch

OpenAI Realtime API expects `pcm16` at 24kHz. Twilio sends µ-law at 8kHz. The architecture describes transcoding in Go. An alternative: check if Twilio can be configured to send PCM16 directly (some Twilio configurations support this, avoiding the µ-law decode step and one lossy conversion). If not, accept the overhead but use a proper resampler.

### Silence Detection Dual-Path Risk

The architecture uses OpenAI's server-side VAD for turn detection AND custom silence timeouts in Go (8s, 15s). These can conflict. If OpenAI VAD fires (caller speaks) but Go's custom timer hasn't expired, the system must cancel the timer. The document doesn't describe the synchronization between these two silence-detection systems.

### FAQ Short-Circuit (Section 25.4)

The Go engine detecting simple FAQ patterns via keyword matching and bypassing AI is clever but risky. What if the caller's question contains the keyword "parking" but the actual question is "Is there parking for an event tonight?" vs "I'm having trouble parking"? The FAQ answer might be wrong. At minimum, the FAQ short-circuit should log every bypass for review, and fall back to AI if the answer would be ambiguous.

---

## 8. Recommendations — Ranked by Impact

### Tier 1: Must Fix Before MVP

1. **Fix unit economics.** Adopt the hybrid architecture (Option A) or raise prices significantly (Option B). The £149 Starter plan with £0.47/call cost is not commercially viable.

2. **Fix the cover page.** Remove "£0.04/call" — replace with the actual £0.47/call (or the new cost after architecture changes). Misleading cost claims damage credibility with investors and technical evaluators.

3. **Add OpenAI rate limit mitigation.** Document the OpenAI Realtime API concurrent session limit. Plan for what happens when you hit it. Consider a waitlist/capacity reservation system per tenant.

4. **Implement proper audio resampling.** Linear interpolation → polyphase filter. Test with real phone calls to verify voice quality doesn't degrade.

### Tier 2: Must Fix Before 10 Paying Customers

5. **Per-call cost tracking.** Add `ai_input_audio_seconds` and `ai_output_audio_seconds` to the call_sessions table. Without this, you can't measure per-tenant profitability.

6. **WebSocket reconnection strategy.** Design and document the recovery flow when the OpenAI WebSocket drops mid-call. Test it.

7. **Synthetic call test for availability validation.** Add a health check that specifically opens an OpenAI Realtime session, sends a test audio frame, and verifies response.

8. **FAQ short-circuit audit trail.** Log every keyword-matched FAQ bypass to an audit table for manual review.

### Tier 3: Important Before Scaling Past 50 Tenants

9. **Second booking platform integration.** Budget for and design the second adapter (OpenTable or Resy). Prove the adapter pattern works before you have 50 ResDiary-dependent tenants.

10. **Engineering team plan.** Budget for 4–5 engineers. This is not a 2-person startup project given the technology breadth and production reliability requirements.

11. **Sticky session robustness.** Design and test the node failure mid-call scenario. Accept that some calls will drop — define the acceptable loss rate.

---

## 9. Final Verdict

**The architecture is technically sound.** If OpenAI Realtime API pricing were 5× lower or your prices were 2× higher, this would be a strong plan. The engineering decisions, from Go for the voice engine to state-scoped tool injection to deferred Kubernetes, are correct.

**The business model is broken at current pricing.** You cannot resell OpenAI's Realtime API at a 5% markup and call it a SaaS business. You need either lower costs (hybrid architecture) or higher prices (value-based pricing).

**The single biggest architectural risk is OpenAI vendor lock-in.** If the Realtime API goes away, your product goes away. There is no plan B in this document. At minimum, design the system so the OpenAI session handler is behind an interface that could be swapped for a different realtime AI provider (if one ever emerges).

**What to do this week:**

1. Delete £0.04/call from the cover page. Replace with realistic numbers.
2. Model the hybrid architecture costs and build a P&L that works.
3. Get a real OpenAI Realtime API key. Run 50 test calls. Measure actual latency, actual costs, and actual voice quality. Do not trust the numbers in this document until validated against production API behaviour.
4. Check OpenAI Realtime API concurrent session limits and rate limit policies. These are the real scaling ceiling, not your VPS capacity.
