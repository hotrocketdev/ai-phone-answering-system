// src/providers/fake/depos-fake.js — in-process fake for Depos.
//
// Implements DepositProvider.
//
// Behaviour matrix:
//   - successful deposit hold    -> ok, status=held, hold_id generated
//   - declined card              -> DECLINED (no hold created)
//   - provider timeout           -> PROVIDER_TIMEOUT (retryable)
//   - idempotency key handling   -> same key returns same result (replay)
//   - amount calculation         -> DEPOSIT_PENCE_PER_COVER (default 2000 = £20) * party_size
//                                  but each test can override per-tenant

import crypto from 'node:crypto';

/**
 * @typedef {import('../providers.ts').DepositProvider} DepositProvider
 * @typedef {import('../providers.ts').DepositHoldRequest} DepositHoldRequest
 * @typedef {import('../providers.ts').DepositCompensationRequest} DepositCompensationRequest
 */

const DEFAULT_PENCE_PER_COVER = 2000; // £20

function err(code, message, retryable = false, detail) {
  return { ok: false, error: { code, message, retryable, detail } };
}
function ok(value) { return { ok: true, value }; }

export class FakeDepos {
  constructor(pencePerCover = DEFAULT_PENCE_PER_COVER) {
    this.pencePerCover = pencePerCover;
    this.holds = new Map();        // hold_id -> hold
    this.released = new Set();     // released hold_ids
    /** @type {Map<string, object>} idempotency_key -> result for replay */
    this.replay = new Map();
    this._nextError = null;
  }

  scenario(name, count = 1) {
    this._nextError = { name, remaining: count };
  }
  setLatency(ms) { this._latencyMs = ms; }

  async _maybeFail() {
    if (this._latencyMs) await new Promise((r) => setTimeout(r, this._latencyMs));
    if (this._nextError && this._nextError.remaining > 0) {
      this._nextError.remaining--;
      const name = this._nextError.name;
      if (this._nextError.remaining === 0) this._nextError = null;
      if (name === 'timeout') return err('PROVIDER_TIMEOUT', 'Fake: Depos timed out', true);
      if (name === 'declined') return err('DECLINED', 'Fake: card was declined', false);
      if (name === 'error') return err('PROVIDER_ERROR', 'Fake: Depos 5xx', true);
    }
    return null;
  }

  async hold(req) {
    const fail = await this._maybeFail();
    if (fail) return fail;

    const cached = this.replay.get(req.idempotency_key);
    if (cached) return cached;

    if (!Number.isInteger(req.amount_cents) || req.amount_cents <= 0) {
      return err('INVALID_INPUT', `amount_cents must be a positive integer, got ${req.amount_cents}`);
    }
    if (!req.customer_email) {
      return err('INVALID_INPUT', 'customer_email is required for hold');
    }
    if (!req.booking_id) {
      return err('INVALID_INPUT', 'booking_id is required for hold');
    }

    const holdId = 'h-' + crypto.randomBytes(6).toString('hex');
    const expiresAt = new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString();
    const hold = {
      hold_id: holdId,
      status: 'held',
      amount_cents: req.amount_cents,
      currency: req.currency,
      booking_id: req.booking_id,
      customer_email: req.customer_email,
      customer_name: req.customer_name,
      customer_phone: req.customer_phone,
      expires_at: expiresAt,
    };
    this.holds.set(holdId, hold);
    const result = ok(hold);
    this.replay.set(req.idempotency_key, result);
    return result;
  }

  async compensate(req, idempotencyKey) {
    const cached = this.replay.get(idempotencyKey);
    if (cached) return cached;
    if (!this.holds.has(req.hold_id) && !this.released.has(req.hold_id)) {
      return err('NOT_FOUND', `No hold with hold_id ${req.hold_id}`);
    }
    this.released.add(req.hold_id);
    const result = ok({ released: true });
    this.replay.set(idempotencyKey, result);
    return result;
  }

  /** Calculate the amount for a given party size. Used by the dispatcher. */
  amountFor(partySize) {
    return partySize * this.pencePerCover;
  }
}

export function makeFakeDepos(pencePerCover) {
  const f = new FakeDepos(pencePerCover);
  return {
    deposit: {
      hold: (req) => f.hold(req),
      compensate: (req, key) => f.compensate(req, key),
    },
    _internal: f,
  };
}
