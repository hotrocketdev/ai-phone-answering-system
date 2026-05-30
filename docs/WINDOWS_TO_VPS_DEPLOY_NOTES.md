# Windows → VPS Deployment Notes

**Date**: 2026-05-30
**Dev environment**: Windows 11 + PowerShell 7
**Target**: Ubuntu VPS

---

## SSH to VPS

```powershell
# From PowerShell on Windows
ssh root@<vps-ip>

# Or with key
ssh -i ~/.ssh/id_rsa root@<vps-ip>
```

## Test Gateway from Windows

```powershell
# Health check through VPS (public)
Invoke-WebRequest -Uri "https://<your-domain>/health"

# Test webhook
Invoke-WebRequest -Uri "https://<your-domain>/api/public/voice/webhook" `
  -Method POST `
  -ContentType "application/x-www-form-urlencoded" `
  -Body "CallSid=test&From=%2B1234567890&To=%2B0987654321"

# Test WebSocket (returns status code)
$ws = New-Object System.Net.WebSockets.ClientWebSocket
$ct = New-Object System.Threading.CancellationTokenSource
$ct.CancelAfter(5000)
try {
    $ws.ConnectAsync([Uri]"wss://<your-domain>/stream/test", $ct.Token).Wait(5000)
    Write-Host "WS: $($ws.State)"
} catch {
    Write-Host "WS FAIL: $_"
}
$ws.Dispose()
```

## Copy Files to VPS

```powershell
# Copy .env (never commit this file!)
scp .env root@<vps-ip>:/opt/ai-voice-receptionist/.env

# Copy binary (built on Windows via cross-compile)
cd voice-gateway
$env:GOOS='linux'; $env:GOARCH='amd64'; $env:CGO_ENABLED='0'
go build -o gateway-linux ./cmd/gateway
scp gateway-linux root@<vps-ip>:/opt/ai-voice-receptionist/voice-gateway/gateway
```

## Pull Latest Code on VPS

```bash
# SSH into VPS
ssh root@<vps-ip>
cd /opt/ai-voice-receptionist
git pull origin main

# Rebuild if Go code changed
cd voice-gateway
go build -o gateway ./cmd/gateway

# Rebuild if backend changed
cd ../backend
npm ci --production
npm run build

# Restart services
systemctl restart voxlane-gateway voxlane-backend
```

## Restart Services

```bash
# SSH into VPS
ssh root@<vps-ip>
systemctl restart voxlane-gateway
systemctl restart voxlane-backend
systemctl restart caddy
```

## View Logs

```bash
# SSH into VPS
journalctl -u voxlane-gateway -f    # Gateway live
journalctl -u voxlane-backend -f    # Backend live
journalctl -u caddy -f              # Caddy access/error
tail -f /var/log/caddy/voxlane-access.log
```

## Safety Rules

- **Never commit `.env`** — it contains API keys and secrets
- **Never print secrets** in terminal output or logs
- Use `.env.example` as template, copy and fill in manually
- SSH keys should be in `~/.ssh/`, not in the repo
- Test from Windows PowerShell, not from WSL (matches dev environment)

## Quick VPS Deploy (One-Liner Reference)

```powershell
# From Windows, after VPS is provisioned:
ssh root@<vps-ip> "apt update && apt install -y git && git clone https://github.com/hotrocketdev/ai-phone-answering-system.git /opt/ai-voice-receptionist"
# Then follow docs/VPS_DEPLOYMENT_RUNBOOK.md
```
