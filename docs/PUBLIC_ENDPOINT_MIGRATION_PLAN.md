# Public Endpoint Migration Plan

**Date**: 2026-05-30
**Status**: **Complete** — deployed to VPS, verified with live call

---

## Evidence

ngrok free tier fails within 30–60 seconds of restart. Observed failures:

| Error | Meaning |
|-------|---------|
| `ERR_NGROK_8012` | Agent can't reach upstream (localhost:8080) |
| `502 Bad Gateway` | HTTP/WebSocket upgrade fails |
| Process exits silently | No log, no crash dump |

Local gateway health check returns 200. Local WebSocket (`ws://localhost:8080/stream/test`) opens correctly. Cartesia API tested directly and returns valid audio. The entire local pipeline works.

**Root cause**: ngrok free tier is not production-grade. It is a development convenience, not infrastructure.

---

## Target Architecture

```
Internet → VPS (public IP) → Caddy (TLS + reverse proxy) → Gateway:8080
                                                            → Backend:3001
```

### Caddy Config

```caddyfile
voxlane.example.com {
    # API webhook proxy
    handle /api/* {
        reverse_proxy localhost:3001
    }

    # WebSocket stream endpoint
    handle /stream/* {
        @ws {
            header Connection *Upgrade*
            header Upgrade    websocket
        }
        reverse_proxy @ws localhost:8080
    }

    # Fallback
    handle {
        reverse_proxy localhost:8080
    }
}
```

### DNS

| Record | Value |
|--------|-------|
| voxlane.example.com | VPS public IP |
| *.voxlane.example.com | VPS public IP |

---

## Migration Steps

1. Provision VPS (Hetzner CX22 or similar — 2 vCPU, 4 GB RAM, 40 GB SSD, ~$6/mo)
2. Install Caddy (`apt install caddy`)
3. Deploy Caddyfile with correct domain
4. Point DNS A record to VPS IP
5. Run gateway as systemd service
6. Run backend (NestJS) as systemd service
7. Update Twilio webhook URL to `https://voxlane.example.com/api/public/voice/webhook`
8. Update TwiML Stream URL to `wss://voxlane.example.com/stream/{callSid}`
9. Remove ngrok from development workflow
10. Document SSH access and deployment procedures

### Systemd Service: Gateway

```ini
[Unit]
Description=VoxLane Voice Gateway
After=network.target

[Service]
Type=simple
User=voxlane
WorkingDirectory=/opt/voxlane/voice-gateway
EnvironmentFile=/opt/voxlane/.env
ExecStart=/opt/voxlane/voice-gateway/gateway
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

### Systemd Service: Backend

```ini
[Unit]
Description=VoxLane Backend (NestJS)
After=network.target

[Service]
Type=simple
User=voxlane
WorkingDirectory=/opt/voxlane/backend
EnvironmentFile=/opt/voxlane/.env
ExecStart=/usr/bin/node dist/main.js
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

---

## Security

- TLS handled by Caddy (automatic Let's Encrypt)
- Gateway binds to localhost only (not exposed to internet directly)
- Backend binds to localhost only
- Firewall (ufw): allow 80/tcp, 443/tcp, deny all else
- SSH key-only authentication

---

## Future: Telnyx

Once the VPS is provisioned, Telnyx can route calls directly to the public WebSocket endpoint without Twilio as intermediary. This eliminates ngrok AND Twilio Media Streams overhead.

---

## Rollback

Keep Twilio + ngrok as emergency fallback during migration. Do not delete ngrok config until VPS is verified with live calls.

---

## Execution Order

Follow these steps **in exact order**. Do not skip validation gates.

### Phase A — Provision & Install

| Step | Command / Action | Validation Gate |
|------|-----------------|-----------------|
| A1 | Provision VPS (Hetzner CX22 or equivalent) | SSH accessible |
| A2 | Point DNS A record to VPS IP | `dig <domain>` returns VPS IP |
| A3 | Run `docs/VPS_DEPLOYMENT_RUNBOOK.md` Steps 1-2 | `redis-cli ping` → PONG |
| A4 | Clone repo | `git status` clean |

### Phase B — Config & Build

| Step | Command / Action | Validation Gate |
|------|-----------------|-----------------|
| B1 | Copy `deploy/env.production.example` → `.env` | Fill in ALL real values |
| B2 | Build backend: `cd backend && npm ci && npm run build` | `npm run start` works locally |
| B3 | Build gateway: `cd voice-gateway && go build` | Binary exists |
| B4 | Install Caddy config | `caddy validate` passes |

### Phase C — Deploy & Verify

| Step | Command / Action | Validation Gate |
|------|-----------------|-----------------|
| C1 | Install systemd units | `systemctl daemon-reload` OK |
| C2 | Start all services | `systemctl status` shows active |
| C3 | Run `deploy/scripts/validate-public-endpoint.sh` | **ALL checks PASS** |
| C4 | Verify public WebSocket upgrade | WS: 101 Switching Protocols |

### Phase D — Cut Over

| Step | Command / Action | Validation Gate |
|------|-----------------|-----------------|
| D1 | Update Twilio webhook to `https://<domain>/api/public/voice/webhook` | Webhook returns 200 |
| D2 | Make test call: dial +441789336134 | Hear British greeting |
| D3 | Verify conversation: ask a question | AI responds correctly |

### Phase E — Telnyx (Future)

**DO NOT START** until ALL Phase C and D gates pass.
**Gate**: "Public WebSocket 101 is stable" must be verified.

See `experimental/telnyx/TELNYX_ADAPTER_IMPLEMENTATION_PLAN.md`.

---

## Critical Gates

| Gate # | Description | Must Pass |
|--------|-------------|-----------|
| G1 | Redis responding | PONG |
| G2 | Local gateway health | 200 |
| G3 | Local backend webhook | 200 XML |
| G4 | Public webhook via Caddy | 200 XML |
| G5 | Public WebSocket upgrade | 101 |
| G6 | Live call audio | Hear greeting |
| G7 | Live call conversation | AI responds |

**Do not proceed to Phase D until G1-G5 all pass.**
**Do not proceed to Phase E until G1-G7 all pass.**

---

## Verification Record

**Date**: 2026-05-30
**Call SID**: CAe66f71f893124b12fe2e01fb19a82360

| Gate | Result |
|------|--------|
| G1 — Redis | PONG |
| G2 — Gateway health | 200 |
| G3 — Backend webhook | 200 XML |
| G4 — Public webhook | 200 XML |
| G5 — Public WebSocket | 101 Open |
| G6 — Live call audio | Caller heard Cartesia voice |
| G7 — Live conversation | OpenAI responded, Cartesia rendered |

**Domain**: voice.voxlane.co.uk
**Transport**: nginx (VPS) — no ngrok
**Twilio webhook**: https://voice.voxlane.co.uk/api/public/voice/webhook
**Telnyx**: now unblocked

