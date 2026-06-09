// src/providers/fake/resdiary-fake.js — in-process fake for ResDiary.
//
// Implements AvailabilityProvider + BookingProvider.
//
// Behaviour matrix (the user asked for all of these):
//   - available slot               -> ok, available=true
//   - unavailable slot             -> ok, available=false + next_slot suggestion
//   - invalid party size (<1 or >20) -> INVALID_INPUT
//   - invalid date/time format     -> INVALID_INPUT
//   - provider timeout (configurable per call) -> PROVIDER_TIMEOUT
//   - provider error (5xx simulation) -> PROVIDER_ERROR
//
// Also tracks created bookings so we can verify (id, confirmation_code)
// round-trip in tests.

import crypto from 'node:crypto';

/**
 * @typedef {import('../providers.ts').AvailabilityProvider} AvailabilityProvider
 * @typedef {import('../providers.ts').BookingProvider} BookingProvider
 * @typedef {import('../providers.ts').AvailabilityRequest} AvailabilityRequest
 * @typedef {import('../providers.ts').BookingCreateRequest} BookingCreateRequest
 */

const PARTY_SIZE_MIN = 1;
const PARTY_SIZE_MAX = 20;
const ISO_DATE_RE = /^\d{4}-\d{2}-\d{2}$/;
const TIME24_RE = /^([01]\d|2[0-3]):[0-5]\d$/;

function validateDate(d) {
  if (typeof d !== 'string') {
    return { code: 'INVALID_INPUT', message: `Invalid date: ${JSON.stringify(d)} (expected YYYY-MM-DD or natural language like "tomorrow")` };
  }
  // ISO YYYY-MM-DD — accepted as-is.
  if (ISO_DATE_RE.test(d)) return null;
  // Natural-language dates the model uses in the system prompt.
  const lower = d.trim().toLowerCase();
  if (lower === 'tomorrow' || lower === 'today' || lower === 'tonight' ||
      lower === 'this evening' || /^this (mon|tues|wednes|thurs|fri|satur|sun)day$/.test(lower) ||
      /^(mon|tues|wednes|thurs|fri|satur|sun)day$/.test(lower)) {
    return null;
  }
  return { code: 'INVALID_INPUT', message: `Invalid date format: ${JSON.stringify(d)} (expected YYYY-MM-DD or natural language like "tomorrow")` };
}
function validateTime(t) {
  if (typeof t !== 'string') {
    return { code: 'INVALID_INPUT', message: `Invalid time: ${JSON.stringify(t)} (expected HH:MM)` };
  }
  if (TIME24_RE.test(t)) return null;
  // Natural-language: "7", "7pm", "7 pm", "seven", "half past seven", "19:00", etc.
  const lower = t.trim().toLowerCase();
  if (/^(\d{1,2})(\s*(am|pm))?$/.test(lower)) return null;
  if (/^(\d{1,2}):\d{2}(\s*(am|pm))?$/.test(lower)) return null;
  const wordNums = ['one','two','three','four','five','six','seven','eight','nine','ten','eleven','twelve'];
  if (wordNums.some((w) => lower === w || lower === w + ' pm' || lower === w + ' am')) return null;
  return { code: 'INVALID_INPUT', message: `Invalid time format: ${JSON.stringify(t)} (expected HH:MM or natural language like "seven" / "7pm")` };
}
function validatePartySize(p) {
  if (!Number.isInteger(p) || p < PARTY_SIZE_MIN || p > PARTY_SIZE_MAX) {
    return { code: 'INVALID_INPUT', message: `Invalid party_size: ${p} (must be integer 1-20)` };
  }
  return null;
}

function err(code, message, retryable = false, detail) {
  return { ok: false, error: { code, message, retryable, detail } };
}
function ok(value) { return { ok: true, value }; }

/**
 * Configurable fake. Pre-seed a scenario per test:
 *
 *   const fake = new FakeResDiary();
 *   fake.scenario('unavailable');  // or 'timeout', 'error', 'invalid'
 *   const provider = fake.asProvider();
 */
export class FakeResDiary {
  constructor() {
    /** @type {Map<string, object>} idempotency_key -> result, for replay */
    this.replay = new Map();
    /** @type {Map<string, object>} booking_id -> booking */
    this.bookings = new Map();
    /** Force the next call to return a specific error. */
    this._nextError = null;
  }

  /** Pre-seed a scenario for the next N calls. */
  scenario(name, count = 1) {
    this._nextError = { name, remaining: count };
  }

  /** Inject an HTTP-style latency (ms) for every call. */
  setLatency(ms) { this._latencyMs = ms; }

  async _maybeFail() {
    if (this._latencyMs) await new Promise((r) => setTimeout(r, this._latencyMs));
    if (this._nextError && this._nextError.remaining > 0) {
      this._nextError.remaining--;
      const name = this._nextError.name;
      if (this._nextError.remaining === 0) this._nextError = null;
      if (name === 'timeout') return err('PROVIDER_TIMEOUT', 'Fake: provider timed out after 30s', true);
      if (name === 'error') return err('PROVIDER_ERROR', 'Fake: upstream 5xx', true);
      if (name === 'invalid') return err('INVALID_INPUT', 'Fake: invalid input', false);
    }
    return null;
  }

  async check(req, idempotencyKey) {
    const fail = await this._maybeFail();
    if (fail) return fail;

    const cached = this.replay.get(idempotencyKey);
    if (cached) return cached;

    const dateErr = validateDate(req.date);
    if (dateErr) return { ok: false, error: dateErr };
    const timeErr = validateTime(req.time);
    if (timeErr) return { ok: false, error: timeErr };
    const partyErr = validatePartySize(req.party_size);
    if (partyErr) return { ok: false, error: partyErr };

    // Simulate "no tables available" for the catch-all date 2099-12-31.
    if (req.date === '2099-12-31') {
      const result = ok({
        available: false,
        next_slot: { date: '2026-06-08', time: '19:30', party_size: req.party_size },
        message: 'No tables available at that time. The next available slot is 19:30 on 2026-06-08.',
      });
      this.replay.set(idempotencyKey, result);
      return result;
    }

    const result = ok({
      available: true,
      next_slot: null,
      message: 'A table is available.',
    });
    this.replay.set(idempotencyKey, result);
    return result;
  }

  async create(req) {
    const fail = await this._maybeFail();
    if (fail) return fail;

    const cached = this.replay.get(req.idempotency_key);
    if (cached) return cached;

    const dateErr = validateDate(req.date);
    if (dateErr) return { ok: false, error: dateErr };
    const timeErr = validateTime(req.time);
    if (timeErr) return { ok: false, error: timeErr };
    const partyErr = validatePartySize(req.party_size);
    if (partyErr) return { ok: false, error: partyErr };
    if (!req.customer?.name) return err('INVALID_INPUT', 'customer.name is required');
    if (!req.customer?.phone) return err('INVALID_INPUT', 'customer.phone is required');

    const id = 'b-' + crypto.randomBytes(6).toString('hex');
    const confirmation = 'PD-' + req.date.replace(/-/g, '') + '-' + String(this.bookings.size + 1).padStart(3, '0');
    const booking = {
      id,
      confirmation_code: confirmation,
      status: 'created',
      date: req.date,
      time: req.time,
      party_size: req.party_size,
      customer: { name: req.customer?.name, phone: req.customer?.phone, email: req.customer?.email },
      notes: req.notes || null,
      deposit_hold_id: req.deposit_hold_id || null,
    };
    this.bookings.set(id, booking);
    const result = ok(booking);
    this.replay.set(req.idempotency_key, result);
    return result;
  }

  async get(confirmationCode) {
    for (const b of this.bookings.values()) {
      if (b.confirmation_code === confirmationCode) return ok(b);
    }
    return err('NOT_FOUND', `No booking with confirmation_code ${confirmationCode}`);
  }
}

export function makeFakeResDiary() {
  const f = new FakeResDiary();
  return {
    availability: { check: (req, key) => f.check(req, key) },
    booking: {
      create: (req) => f.create(req),
      get: (code) => f.get(code),
    },
    /** Test-only accessor for verifying internal state. */
    _internal: f,
  };
}
