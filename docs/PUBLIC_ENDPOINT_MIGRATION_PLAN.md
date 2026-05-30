# Public Endpoint Migration Plan

**Date**: 2026-05-30
**Status**: Active — ngrok confirmed unreliable, migration required

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
