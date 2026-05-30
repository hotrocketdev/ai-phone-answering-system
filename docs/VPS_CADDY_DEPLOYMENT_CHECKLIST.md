# VPS + Caddy Deployment Checklist

**Date**: 2026-05-30
**Status**: Pre-deployment planning

---

## 1. VPS Requirements

| Item | Spec |
|------|------|
| OS | Ubuntu 24.04 LTS |
| vCPU | 2 (minimum) |
| RAM | 4 GB |
| Disk | 40 GB SSD |
| IP | Static public IPv4 |
| Provider | Hetzner CX22 (~$6/mo) or DigitalOcean ($12/mo) |
| Domain | `voxlane.example.com` (replace with real domain) |

### Firewall (ufw)

```bash
ufw allow 80/tcp
ufw allow 443/tcp
ufw allow 22/tcp
ufw deny 6379      # Redis — local only
ufw deny 3001      # Backend — local only
ufw deny 8080      # Gateway — local only
ufw enable
```

## 2. Services

| Service | Bind | Purpose |
|---------|------|---------|
| Caddy | :80, :443 | TLS termination, reverse proxy |
| Gateway (Go) | localhost:8080 | WebSocket stream handler |
| Backend (NestJS) | localhost:3001 | Webhook handler, business logic |
| Redis | localhost:6379 | Session state cache |

## 3. Caddy Routes

```
/health                    → gateway:8080 (health check)
/api/public/voice/webhook  → backend:3001 (Twilio/Telnyx webhook)
/api/public/voice/status-callback → backend:3001
/stream/*                  → gateway:8080 (WebSocket upgrade)
/metrics                   → gateway:8080 (Prometheus)
```

## 4. Environment Variables

```bash
# /opt/voxlane/.env

# Gateway
GATEWAY_PORT=8080
GATEWAY_WS_URL=wss://voxlane.example.com/stream
GATEWAY_MAX_CONCURRENT_CALLS=100
GATEWAY_MAX_CALL_DURATION_SECONDS=1800
GATEWAY_SILENCE_TIMEOUT_PROMPT_SECONDS=10
GATEWAY_SILENCE_TIMEOUT_HANGUP_SECONDS=20

# OpenAI
OPENAI_API_KEY=sk-...
OPENAI_REALTIME_MODEL=gpt-realtime-1.5

# Cartesia
VOICE_RENDERER=cartesia
CARTESIA_API_KEY=sk_car_...
CARTESIA_VOICE_ID=2f251ac3-89a9-4a77-a452-704b474ccd01
CARTESIA_MODEL=sonic-3.5
CARTESIA_LANGUAGE=en
CARTESIA_SPEED=0.95
CARTESIA_VOLUME=0.90
CARTESIA_EMOTION=content

# Runtime
VOICE_RUNTIME=custom
VOICE_PROVIDER=twilio

# Telephony
TWILIO_ACCOUNT_SID=...
TWILIO_AUTH_TOKEN=...

# Telnyx (future)
TELEPHONY_PROVIDER=twilio
TELNYX_API_KEY=
TELNYX_CONNECTION_ID=
TELNYX_PHONE_NUMBER=
TELNYX_WEBHOOK_SECRET=
TELNYX_STREAM_CODEC=PCMU
TELNYX_BIDIRECTIONAL_CODEC=PCMU

# Backend
NESTJS_URL=http://localhost:3001
HMAC_SECRET=...
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=

# Business
BUSINESS_NAME=Porto Douro Restaurants
LOG_LEVEL=info
```

## 5. Health Checks

| Check | Command | Expected |
|-------|---------|----------|
| Gateway local | `curl http://localhost:8080/health` | `{"status":"ok"}` |
| Backend local | `curl -X POST http://localhost:3001/api/public/voice/webhook -d "CallSid=test&From=%2B1&To=%2B2"` | 200 XML |
| Public webhook | `curl -X POST https://voxlane.example.com/api/public/voice/webhook -d "CallSid=test&From=%2B1&To=%2B2"` | 200 XML |
| Public WebSocket | `curl -i -N -H "Connection: Upgrade" -H "Upgrade: websocket" https://voxlane.example.com/stream/test` | 101 Switching Protocols |
| Redis | `redis-cli ping` | PONG |

## 6. Process Management

All services run via systemd. See `deploy/systemd/` for unit files.

```bash
systemctl enable --now redis-server
systemctl enable --now voxlane-gateway
systemctl enable --now voxlane-backend
systemctl enable --now caddy
```

### Logs

```bash
journalctl -u voxlane-gateway -f
journalctl -u voxlane-backend -f
journalctl -u caddy -f
```

## 7. Deployment Steps

1. Provision VPS, SSH in as root
2. Create `voxlane` user: `useradd -m -s /bin/bash voxlane`
3. Install dependencies: `apt update && apt install -y redis-server caddy unzip curl`
4. Copy gateway binary to `/opt/voxlane/voice-gateway/gateway`
5. Copy backend dist to `/opt/voxlane/backend/dist/`
6. Copy `.env` to `/opt/voxlane/.env` (chmod 600)
7. Copy systemd units to `/etc/systemd/system/`
8. Copy Caddyfile to `/etc/caddy/Caddyfile`
9. Point DNS A record to VPS IP
10. `systemctl daemon-reload && systemctl enable --now voxlane-gateway voxlane-backend caddy`
11. Run health checks (section 5)
12. Update Twilio webhook URL to `https://voxlane.example.com/api/public/voice/webhook`
13. Make test call

## 8. Rollback

- Keep local dev environment + ngrok config intact
- Keep Twilio webhook pointing to ngrok until VPS verified
- After VPS verified, switch Twilio webhook → VPS domain
- Keep Twilio fallback `<Say>` in TwiML for graceful degradation
