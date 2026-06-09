// src/providers/real/resdiary-adapter.js — SKELETON for the real
// ResDiary adapter.
//
// SKELETON STATUS: HTTP plumbing is in place but the field mappings
// are placeholder TODOs. The user has not yet provided the ResDiary
// API access. When the API key arrives, fill in the field names
// below and test against the ResDiary sandbox.
//
// Env vars (all required when USE_REAL_PROVIDERS=1):
//   RESDIARY_API_KEY      (required)
//   RESDIARY_BASE_URL     (default: https://api.resdiary.com/v1)
//   RESDIARY_VENUE_ID     (required; the Porto Douro restaurant id)
//
// The adapter implements the same AvailabilityProvider +
// BookingProvider contracts as the fake (src/providers/fake/
// resdiary-fake.js), so the dispatcher doesn't change when we
// swap fakes for real.

const RESDIARY_BASE = process.env.RESDIARY_BASE_URL || 'https://api.resdiary.com/v1';
const RESDIARY_KEY = process.env.RESDIARY_API_KEY;
const RESDIARY_VENUE_ID = process.env.RESDIARY_VENUE_ID;

function requireEnv() {
  const missing = [];
  if (!RESDIARY_KEY) missing.push('RESDIARY_API_KEY');
  if (!RESDIARY_VENUE_ID) missing.push('RESDIARY_VENUE_ID');
  if (missing.length) {
    throw new Error(
      `ResDiary adapter: missing env vars: ${missing.join(', ')}. ` +
      `Set USE_REAL_PROVIDERS=1 only after configuring them.`,
    );
  }
}

function err(code, message, retryable = false, detail) {
  return { ok: false, error: { code, message, retryable, detail } };
}
function ok(value) { return { ok: true, value }; }

async function resdiaryFetch(path, init = {}) {
  requireEnv();
  const url = new URL(RESDIARY_BASE + path);
  const res = await fetch(url, {
    ...init,
    headers: {
      'Authorization': `Bearer ${RESDIARY_KEY}`,
      'Accept': 'application/json',
      ...(init.body ? { 'Content-Type': 'application/json' } : {}),
      ...(init.headers || {}),
    },
    body: init.body ? JSON.stringify(init.body) : undefined,
  });
  return res;
}

function mapHttpStatus(res, path) {
  if (res.status === 404) return err('NOT_FOUND', `ResDiary ${path} -> 404`);
  if (res.status === 408 || res.status === 504) return err('PROVIDER_TIMEOUT', `ResDiary ${path} -> ${res.status}`, true);
  if (res.status === 422) return err('INVALID_INPUT', `ResDiary ${path} -> 422: ${res.statusText}`);
  if (res.status >= 500) return err('PROVIDER_ERROR', `ResDiary ${path} -> ${res.status}`, true);
  if (!res.ok) return err('PROVIDER_ERROR', `ResDiary ${path} -> ${res.status}`, true, res.statusText);
  return null;
}

/**
 * Map ResDiary's GET /availability response to our AvailabilityResponse.
 *
 * TODO: confirm the actual field names with the ResDiary API docs
 * (or the user's sandbox). The fake returns:
 *   { available, next_slot: { date, time, party_size }, message }
 */
function mapAvailabilityResponse(data) {
  // PLACEHOLDER — fill in when API access arrives.
  return {
    available: !!data.available,
    next_slot: data.next_slot || null,
    message: data.message || (data.available ? 'A table is available.' : 'No tables available at that time.'),
  };
}

/**
 * Map our BookingCreateRequest to ResDiary's POST /bookings body.
 *
 * TODO: confirm the actual field names.
 */
function toResDiaryBookingBody(req) {
  return {
    venue_id: RESDIARY_VENUE_ID,
    date: req.date,
    time: req.time,
    party_size: req.party_size,
    customer: {
      name: req.customer.name,
      phone: req.customer.phone,
      email: req.customer.email,
    },
    notes: req.notes,
    deposit_hold_id: req.deposit_hold_id,
    idempotency_key: req.idempotency_key,
  };
}

/**
 * Map ResDiary's POST /bookings response to our BookingResponse.
 *
 * TODO: confirm the actual field names.
 */
function mapBookingResponse(data) {
  return {
    id: data.id,
    confirmation_code: data.confirmation_code || data.confirmationCode || data.id,
    status: data.status || 'created',
    date: data.date,
    time: data.time,
    party_size: data.party_size || data.partySize,
  };
}

async function checkAvailability(req, idempotencyKey) {
  const params = new URLSearchParams({
    venue_id: RESDIARY_VENUE_ID,
    date: req.date,
    time: req.time,
    party_size: String(req.party_size),
  });
  const res = await resdiaryFetch('/availability?' + params.toString());
  const statusErr = mapHttpStatus(res, 'GET /availability');
  if (statusErr) return statusErr;
  return ok(mapAvailabilityResponse(await res.json()));
}

async function createBooking(req) {
  const res = await resdiaryFetch('/bookings', {
    method: 'POST',
    body: toResDiaryBookingBody(req),
    headers: { 'Idempotency-Key': req.idempotency_key },
  });
  const statusErr = mapHttpStatus(res, 'POST /bookings');
  if (statusErr) return statusErr;
  return ok(mapBookingResponse(await res.json()));
}

async function getBooking(confirmationCode) {
  const res = await resdiaryFetch('/bookings/' + encodeURIComponent(confirmationCode));
  const statusErr = mapHttpStatus(res, 'GET /bookings/:code');
  if (statusErr) return statusErr;
  return ok(mapBookingResponse(await res.json()));
}

export function makeResDiaryAdapter() {
  return {
    availability: { check: checkAvailability },
    booking: { create: createBooking, get: getBooking },
  };
}
