#!/bin/bash
# validate-public-endpoint.sh
# Run on VPS after deployment to verify all endpoints.
# Usage: DOMAIN=your-domain.com ./validate-public-endpoint.sh

set -e

DOMAIN="${DOMAIN:-voxlane.example.com}"
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m'

pass() { echo -e "${GREEN}PASS${NC} $1"; }
fail() { echo -e "${RED}FAIL${NC} $1"; exit 1; }

echo "=== VoxLane Public Endpoint Validation ==="
echo "Domain: $DOMAIN"
echo ""

# 1. Service status
echo "--- Services ---"
for svc in voxlane-gateway voxlane-backend caddy redis-server; do
    if systemctl is-active --quiet $svc 2>/dev/null; then
        pass "$svc running"
    else
        fail "$svc not running"
    fi
done
echo ""

# 2. Local gateway health
echo "--- Local Gateway ---"
if curl -sf http://localhost:8080/health > /dev/null 2>&1; then
    pass "gateway health (localhost:8080/health)"
else
    fail "gateway health"
fi
echo ""

# 3. Local backend webhook
echo "--- Local Backend ---"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:3001/api/public/voice/webhook \
    -H "Content-Type: application/x-www-form-urlencoded" \
    -d "CallSid=validate&From=%2B1&To=%2B1")
if [ "$STATUS" = "200" ]; then
    pass "backend webhook (localhost:3001) — HTTP $STATUS"
else
    fail "backend webhook — HTTP $STATUS"
fi
echo ""

# 4. Public webhook (through Caddy)
echo "--- Public Webhook ---"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST "https://$DOMAIN/api/public/voice/webhook" \
    -H "Content-Type: application/x-www-form-urlencoded" \
    -d "CallSid=validate&From=%2B1&To=%2B1")
if [ "$STATUS" = "200" ]; then
    pass "public webhook — HTTP $STATUS"
else
    fail "public webhook — HTTP $STATUS"
fi
echo ""

# 5. Public WebSocket upgrade
echo "--- Public WebSocket ---"
WS_CODE=$(curl -s -o /dev/null -w "%{http_code}" -i -N \
    -H "Connection: Upgrade" \
    -H "Upgrade: websocket" \
    -H "Sec-WebSocket-Version: 13" \
    -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
    "https://$DOMAIN/stream/validate" 2>/dev/null || echo "000")
if [ "$WS_CODE" = "101" ]; then
    pass "public WebSocket upgrade — HTTP 101"
else
    fail "public WebSocket upgrade — HTTP $WS_CODE (expected 101)"
fi
echo ""

# 6. Redis
echo "--- Redis ---"
if redis-cli ping | grep -q PONG; then
    pass "redis ping"
else
    fail "redis ping"
fi
echo ""

echo -e "${GREEN}All checks passed.${NC}"
echo "Next: configure Twilio webhook to https://$DOMAIN/api/public/voice/webhook"
