// src/providers/real/depos-adapter.js — SKELETON for the real Depos
// adapter.
//
// SKELETON STATUS: HTTP plumbing in place; field mappings are
// placeholder TODOs. The user has not yet provided the Depos API
// access. When the API key arrives, fill in the field names and
// test against the Depos sandbox.
//
// Env vars (required when USE_REAL_PROVIDERS=1):
//   DEPOS_API_KEY             (required)
//   DEPOS_BASE_URL            (default: https://api.deposits.com/v1)
//   DEPOSIT_PENCE_PER_COVER   (default: 2000 = £20)

const DEPOS_BASE = process.env.DEPOS_BASE_URL || 'https://api.deposits.com/v1';
const DEPOS_KEY = process.env.DEPOS_API_KEY;
const PENCE_PER_COVER = parseInt(process.env.DEPOSIT_PENCE_PER_COVER || '2000', 10);

function requireEnv() {
  if (!DEPOS_KEY) {
    throw new Error(
      'Depos adapter: missing env var: DEPOS_API_KEY. ' +
      'Set USE_REAL_PROVIDERS=1 only after configuring it.',
    );
  }
}

function err(code, message, retryable = false, detail) {
  return { ok: false, error: { code, message, retryable, detail } };
}
function ok(value) { return { ok: true, value }; }

async function deposFetch(path, init = {}) {
  requireEnv();
  const url = new URL(DEPOS_BASE + path);
  const res = await fetch(url, {
    ...init,
    headers: {
      'Authorization': `Bearer ${DEPOS_KEY}`,
      'Accept': 'application/json',
      ...(init.body ? { 'Content-Type': 'application/json' } : {}),
      ...(init.headers || {}),
    },
    body: init.body ? JSON.stringify(init.body) : undefined,
  });
  return res;
}

function mapHttpStatus(res, path) {
  if (res.status === 402) return err('DECLINED', `Depos ${path} -> 402: card declined`, false);
  if (res.status === 404) return err('NOT_FOUND', `Depos ${path} -> 404`);
  if (res.status === 408 || res.status === 504) return err('PROVIDER_TIMEOUT', `Depos ${path} -> ${res.status}`, true);
  if (res.status === 422) return err('INVALID_INPUT', `Depos ${path} -> 422`);
  if (res.status >= 500) return err('PROVIDER_ERROR', `Depos ${path} -> ${res.status}`, true);
  if (!res.ok) return err('PROVIDER_ERROR', `Depos ${path} -> ${res.status}`, true, res.statusText);
  return null;
}

/**
 * Map our DepositHoldRequest to Depos's POST /holds body.
 *
 * TODO: confirm the actual Depos field names. The fake returned:
 *   { hold_id, status, amount_cents, currency, booking_id, expires_at }
 */
function toDeposHoldBody(req) {
  return {
    booking_id: req.booking_id,
    amount_cents: req.amount_cents,
    currency: req.currency,
    customer_email: req.customer_email,
    customer_name: req.customer_name,
    customer_phone: req.customer_phone,
    expires_in_seconds: req.expires_in_seconds || 24 * 60 * 60,
  };
}

/**
 * Map Depos's POST /holds response to our DepositHoldResponse.
 */
function mapHoldResponse(data) {
  return {
    hold_id: data.hold_id,
    status: data.status || 'held',
    expires_at: data.expires_at,
    confirmation_url: data.confirmation_url,
  };
}

async function hold(req) {
  const res = await deposFetch('/holds', {
    method: 'POST',
    body: toDeposHoldBody(req),
    headers: { 'Idempotency-Key': req.idempotency_key },
  });
  const statusErr = mapHttpStatus(res, 'POST /holds');
  if (statusErr) return statusErr;
  return ok(mapHoldResponse(await res.json()));
}

async function compensate(req, idempotencyKey) {
  // TODO: confirm the actual release/refund endpoint. Possibilities:
  //   POST /holds/{hold_id}/release  (immediate release)
  //   POST /holds/{hold_id}/refund   (refund a captured hold)
  // The fake treated them the same.
  const res = await deposFetch(`/holds/${encodeURIComponent(req.hold_id)}/release`, {
    method: 'POST',
    headers: { 'Idempotency-Key': idempotencyKey },
    body: { reason: req.reason },
  });
  const statusErr = mapHttpStatus(res, 'POST /holds/:id/release');
  if (statusErr) return statusErr;
  const data = await res.json().catch(() => ({}));
  return ok({ released: data.released !== false });
}

export function makeDeposAdapter() {
  return {
    deposit: { hold, compensate },
    /** Test-only: expose the per-cover amount. */
    _pencePerCover: PENCE_PER_COVER,
  };
}
