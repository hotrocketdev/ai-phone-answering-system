# VPS Deployment Runbook

**Date**: 2026-05-30
**App path**: `/opt/ai-voice-receptionist`
**Target**: Ubuntu 24.04 LTS

---

## Step 1 — Server Preparation

```bash
# SSH into VPS as root
ssh root@<vps-ip>

# Update system
apt update && apt upgrade -y

# Create app user
useradd -m -s /bin/bash voxlane
usermod -aG sudo voxlane

# Create app directory
mkdir -p /opt/ai-voice-receptionist
chown voxlane:voxlane /opt/ai-voice-receptionist

# Configure firewall
ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw enable
```

## Step 2 — Install Dependencies

```bash
# System packages
apt install -y redis-server caddy unzip curl git build-essential

# Go (for building gateway)
wget https://go.dev/dl/go1.22.5.linux-amd64.tar.gz
tar -C /usr/local -xzf go1.22.5.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

# Node.js (for backend)
curl -fsSL https://deb.nodesource.com/setup_20.x | bash -
apt install -y nodejs
```

## Step 3 — Clone Repository

```bash
cd /opt/ai-voice-receptionist
git clone https://github.com/hotrocketdev/ai-phone-answering-system.git .
```

## Step 4 — Production Environment File

```bash
# Create from template
cp deploy/env.production.example /opt/ai-voice-receptionist/.env
chmod 600 /opt/ai-voice-receptionist/.env

# Edit with real values
nano /opt/ai-voice-receptionist/.env
```

Replace all placeholders with real API keys, secrets, and domain.

## Step 5 — Build Backend (NestJS)

```bash
cd /opt/ai-voice-receptionist/backend
npm ci --production
npm run build
```

## Step 6 — Build Gateway (Go)

```bash
cd /opt/ai-voice-receptionist/voice-gateway
CGO_ENABLED=0 go build -o gateway ./cmd/gateway
```

## Step 7 — Install Redis

```bash
# Already installed in Step 2
systemctl enable --now redis-server
redis-cli ping   # Should return PONG
```

## Step 8 — Install Caddy

```bash
# Already installed in Step 2
cp /opt/ai-voice-receptionist/deploy/Caddyfile.example /etc/caddy/Caddyfile
# Edit domain: replace voxlane.example.com with real domain
nano /etc/caddy/Caddyfile
caddy validate --config /etc/caddy/Caddyfile
systemctl enable --now caddy
```

## Step 9 — Install Systemd Services

```bash
cp /opt/ai-voice-receptionist/deploy/systemd/voice-gateway.service.example \
   /etc/systemd/system/voxlane-gateway.service
cp /opt/ai-voice-receptionist/deploy/systemd/backend.service.example \
   /etc/systemd/system/voxlane-backend.service

# Edit paths if different
nano /etc/systemd/system/voxlane-gateway.service
nano /etc/systemd/system/voxlane-backend.service

systemctl daemon-reload
```

## Step 10 — Start Services

```bash
systemctl enable --now voxlane-gateway
systemctl enable --now voxlane-backend
systemctl enable --now caddy
systemctl enable --now redis-server
```

## Step 11 — Verify Health

```bash
# Local gateway
curl http://localhost:8080/health

# Local backend
curl -X POST http://localhost:3001/api/public/voice/webhook \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "CallSid=test&From=%2B1&To=%2B2"

# All services
systemctl status voxlane-gateway voxlane-backend caddy redis-server
```

## Step 12 — Verify Public Webhook

```bash
curl -X POST https://<your-domain>/api/public/voice/webhook \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "CallSid=test&From=%2B1&To=%2B2"
```

Expected: 200 with valid TwiML XML response.

## Step 13 — Verify Public WebSocket

```bash
curl -i -N \
  -H "Connection: Upgrade" \
  -H "Upgrade: websocket" \
  -H "Sec-WebSocket-Version: 13" \
  -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
  https://<your-domain>/stream/test
```

Expected: HTTP/1.1 101 Switching Protocols.

## Step 14 — Configure Twilio

1. Go to Twilio Console → Phone Numbers → Active Numbers
2. Select your number (+441789336134)
3. Set Voice webhook to: `https://<your-domain>/api/public/voice/webhook`
4. Set method to HTTP POST
5. Save

## Step 15 — Test Live Call

1. Dial +441789336134
2. Should hear British greeting from Cartesia
3. Say "Can I book a table for two tonight?"
4. Should hear AI response

## Rollback

If VPS deployment fails:
1. Revert Twilio webhook to ngrok URL
2. Restore ngrok: `ngrok http 8080` on local machine
3. Troubleshoot VPS: `journalctl -u voxlane-gateway -f`
4. Check Caddy logs: `journalctl -u caddy -f`

---

## Troubleshooting

| Symptom | Check |
|---------|-------|
| 502 Bad Gateway | `systemctl status voxlane-gateway` — is it running? |
| WebSocket 404 | Caddy route missing — check Caddyfile `/stream/*` handler |
| No audio | Check Cartesia API key, voice ID, model in `.env` |
| Webhook 401 | HMAC validation failing — check `HMAC_SECRET` |
| Redis error | `systemctl status redis-server` |
