# Multi-Tenancy Architecture Note

**Date**: 2026-05-30
**Status**: Architecture correction — enforcement begins now

---

## Core Principle

**VoxLane is the SaaS platform. Porto Douro is a tenant, not the platform.**

---

## Platform vs Tenant Responsibilities

| Concern | VoxLane Platform | Tenant (Porto Douro) |
|---------|-----------------|----------------------|
| Codebase | Owns all code | Configures via env/admin |
| Branding | Configurable from env | BUSINESS_NAME, voice ID, greeting |
| Phone number | Routes calls | Owns Twilio number, pays for usage |
| OpenAI | Platform API key | None |
| Cartesia | Platform API key | Voice ID selection only |
| Deployment | VPS, Caddy, systemd | None |
| Data | Redis session state | None |

---

## Tenant Configuration Boundary

Tenant-specific values MUST only exist as **environment variables**, never in source code:

```bash
BUSINESS_NAME=VoxLane         # NOT hardcoded
CARTESIA_VOICE_ID=<tenant-id>  # tenant-specific
```

Any greeting text derived from `BUSINESS_NAME` must use the env value, not a hardcoded string.

---

## Hardcoded Tenant References Found

| # | File | Hardcode | Severity |
|---|------|----------|----------|
| 1 | `voice-gateway/internal/config/config.go:115` | Default: `"Porto Douro Restaurants"` | Medium |
| 2 | `voice-gateway/cmd/gateway/main.go:339` | Greeting: `"Good evening, Porto Douro Restaurants..."` | High |
| 3 | `voice-gateway/internal/runtime/deepgram/agent.go:102` | Greeting: `"Good afternoon. Porto Douro Restaurants..."` | High |
| 4 | `deploy/env.production.example:62` | Default: `BUSINESS_NAME=Porto Douro Restaurants` | Medium |
| 5 | `docs/VPS_CADDY_DEPLOYMENT_CHECKLIST.md:102` | Doc example: `Porto Douro Restaurants` | Low |
| 6 | `ai_voice_receptionist_session_handoff_may_2026_UPDATED_FINAL.md:499` | Session doc example | Low |

---

## Recommended Fixes

### Fix 1 — Config Default (Medium Priority)
**File**: `voice-gateway/internal/config/config.go:115`

Change default from hardcoded tenant name to generic:
```go
BusinessName: getEnv("BUSINESS_NAME", "VoxLane"),
```

### Fix 2 — Debug Greeting (High Priority)
**File**: `voice-gateway/cmd/gateway/main.go:339`

This is the `runCartesiaDirectGreeting` debug function. Replace hardcoded string:
```go
text := fmt.Sprintf("Good afternoon, %s, how can I help?", cfg.BusinessName)
```

### Fix 3 — Deepgram Greeting (High Priority)
**File**: `voice-gateway/internal/runtime/deepgram/agent.go:102`

Same pattern:
```go
"greeting": fmt.Sprintf("Good afternoon. %s. How can I help?", cfg.BusinessName),
```

### Fix 4 — Docs/Deploy (Low Priority)
**Files**: `deploy/env.production.example`, `docs/VPS_CADDY_DEPLOYMENT_CHECKLIST.md`

Replace:
```bash
BUSINESS_NAME=Your Business Name
```

---

## Multi-Tenant Roadmap

Current: single-tenant (one BUSINESS_NAME, one voice, one phone number)

Future stages (not implemented):
- Tenant database table with voice config per tenant
- Phone number → tenant lookup at call time
- Per-tenant Cartesia voice ID selection
- Tenant onboarding form / admin dashboard

---

## Rules Going Forward

1. **Never hardcode a tenant name** in source code, docs, or examples
2. **Use `VoxLane`** as the default platform name in env templates
3. **Derive greetings** from `BUSINESS_NAME` env var, not from string literals
4. **No tenant-specific values** in deployment docs — use placeholders
5. **Tests and examples** use "Test Business" or "Acme Corp"
