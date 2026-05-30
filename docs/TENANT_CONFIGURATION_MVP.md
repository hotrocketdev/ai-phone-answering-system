# Tenant Configuration — MVP

**Date**: 2026-05-30
**Status**: Implemented

---

## Overview

VoxLane is a SaaS platform. Tenant identity is configured via environment variables. No tenant data is hardcoded in source code.

---

## Configuration

### Platform Identity (env)

```bash
BUSINESS_NAME=VoxLane          # Platform name — used for logs, admin, dashboard
```

### Tenant Identity (env)

```bash
TENANT_BUSINESS_NAME=Porto Douro Restaurants   # Tenant name — used for caller-facing greetings and AI prompts
```

### How It Works

1. `config.CustomerName()` returns `TENANT_BUSINESS_NAME` if set, otherwise falls back to `BUSINESS_NAME`
2. `session.go` passes `cfg.CustomerName()` to the state machine
3. The AI system prompt uses the tenant name: "You are the receptionist at {tenant name}"
4. Debug/direct greeting functions also use `CustomerName()`

### Rules

| Context | Name Used |
|---------|-----------|
| AI system prompt | TENANT_BUSINESS_NAME (or BUSINESS_NAME fallback) |
| Caller greeting (debug) | TENANT_BUSINESS_NAME (or BUSINESS_NAME fallback) |
| Gateway logs | BUSINESS_NAME (platform) |
| Admin/dashboard | BUSINESS_NAME (platform) |
| Documentation examples | Generic placeholders |

---

## Future Multi-Tenancy

Current: single tenant via env vars.

Future stages:
- `config/tenants.json` — JSON file with phone number → tenant mapping
- Redis tenant config — runtime tenant lookup
- Database tenant table — full SaaS tenant management
- Admin dashboard — tenant onboarding UI

### Future Env Structure

```bash
# Platform
BUSINESS_NAME=VoxLane

# Tenant routing (future)
TENANT_CONFIG_SOURCE=env          # env | json | redis | db
TENANT_DEFAULT_BUSINESS_NAME=Porto Douro Restaurants
TENANT_DEFAULT_PHONE_NUMBER=+441789336134
```

---

## Phone-to-Tenant Mapping

Current implementation: single tenant. The `TENANT_BUSINESS_NAME` env var applies to ALL inbound calls.

For multi-tenant routing (future):
- Look up inbound `To` number in tenant config
- Load tenant-specific: voice ID, greeting, business name, prompt customizations
- If no match: use generic greeting "Thanks for calling. How can I help?" (never the platform name)

---

## Verification

**Call SID**: pending
**Expected greeting**: "Porto Douro Restaurants" (not "VoxLane")
**Verification method**: dial +441789336134, listen for tenant name in greeting
