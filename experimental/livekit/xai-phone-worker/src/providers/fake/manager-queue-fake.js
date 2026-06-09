// src/providers/fake/manager-queue-fake.js — in-process fake for the
// manager callback queue.
//
// Implements ManagerQueueProvider.
//
// Behaviour matrix:
//   - message accepted           -> ok, status=accepted, callback_required=true
//   - callback required          -> always true for escalation
//   - queue failure              -> PROVIDER_ERROR
//   - missing caller phone       -> still accepts but marks callback_required=true
//                                   and includes a warning in the response so the
//                                   model knows to ask for the phone

import crypto from 'node:crypto';

function err(code, message, retryable = false, detail) {
  return { ok: false, error: { code, message, retryable, detail } };
}
function ok(value) { return { ok: true, value }; }

export class FakeManagerQueue {
  constructor() {
    this.messages = new Map();
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
      if (name === 'failure') return err('PROVIDER_ERROR', 'Fake: manager queue 5xx', true);
    }
    return null;
  }

  async send(req, idempotencyKey) {
    const fail = await this._maybeFail();
    if (fail) return fail;

    const cached = this.replay.get(idempotencyKey);
    if (cached) return cached;

    if (!req.message) return err('INVALID_INPUT', 'message is required');

    const id = 'm-' + crypto.randomBytes(6).toString('hex');
    const msg = {
      id,
      status: 'accepted',
      callback_required: true,
      received_at: new Date().toISOString(),
      topic: req.topic,
      message: req.message,
      caller_name: req.caller_name || null,
      caller_phone: req.caller_phone || null,
      booking_context: req.booking_context || null,
      /** If caller_phone was missing, mark a soft warning for the model. */
      missing_phone: !req.caller_phone,
    };
    this.messages.set(id, msg);
    const result = ok(msg);
    this.replay.set(idempotencyKey, result);
    return result;
  }
}

export function makeFakeManagerQueue() {
  const f = new FakeManagerQueue();
  return {
    managerQueue: { send: (req, key) => f.send(req, key) },
    _internal: f,
  };
}
