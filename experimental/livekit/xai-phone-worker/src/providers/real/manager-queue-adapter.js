// src/providers/real/manager-queue-adapter.js — SKELETON for the real
// manager-callback-queue adapter.
//
// SKELETON STATUS: HTTP plumbing in place; field mappings are
// placeholder TODOs. The user has not yet provided the manager
// queue endpoint. When the endpoint arrives, fill in the field
// names and test against the manager queue's sandbox.
//
// Env vars (required when USE_REAL_PROVIDERS=1):
//   MANAGER_QUEUE_URL   (required)
//   MANAGER_QUEUE_KEY   (required)

const QUEUE_URL = process.env.MANAGER_QUEUE_URL;
const QUEUE_KEY = process.env.MANAGER_QUEUE_KEY;

function requireEnv() {
  const missing = [];
  if (!QUEUE_URL) missing.push('MANAGER_QUEUE_URL');
  if (!QUEUE_KEY) missing.push('MANAGER_QUEUE_KEY');
  if (missing.length) {
    throw new Error(
      `Manager queue adapter: missing env vars: ${missing.join(', ')}. ` +
      `Set USE_REAL_PROVIDERS=1 only after configuring them.`,
    );
  }
}

function err(code, message, retryable = false, detail) {
  return { ok: false, error: { code, message, retryable, detail } };
}
function ok(value) { return { ok: true, value }; }

async function queueFetch(path, init = {}) {
  requireEnv();
  const url = new URL(QUEUE_URL + path);
  const res = await fetch(url, {
    ...init,
    headers: {
      'Authorization': `Bearer ${QUEUE_KEY}`,
      'Accept': 'application/json',
      ...(init.body ? { 'Content-Type': 'application/json' } : {}),
      ...(init.headers || {}),
    },
    body: init.body ? JSON.stringify(init.body) : undefined,
  });
  return res;
}

function mapHttpStatus(res, path) {
  if (res.status === 404) return err('NOT_FOUND', `Manager queue ${path} -> 404`);
  if (res.status === 408 || res.status === 504) return err('PROVIDER_TIMEOUT', `Manager queue ${path} -> ${res.status}`, true);
  if (res.status >= 500) return err('PROVIDER_ERROR', `Manager queue ${path} -> ${res.status}`, true);
  if (!res.ok) return err('PROVIDER_ERROR', `Manager queue ${path} -> ${res.status}`, true, res.statusText);
  return null;
}

/**
 * Map our ManagerEscalationRequest to the manager-queue POST body.
 *
 * TODO: confirm field names with the manager queue's docs.
 */
function toQueueMessageBody(req) {
  return {
    topic: req.topic,
    message: req.message,
    caller_name: req.caller_name,
    caller_phone: req.caller_phone,
    booking_context: req.booking_context,
    received_at: new Date().toISOString(),
  };
}

async function send(req, idempotencyKey) {
  const res = await queueFetch('/messages', {
    method: 'POST',
    body: toQueueMessageBody(req),
    headers: { 'Idempotency-Key': idempotencyKey },
  });
  const statusErr = mapHttpStatus(res, 'POST /messages');
  if (statusErr) return statusErr;
  const data = await res.json();
  return ok({
    id: data.id,
    status: data.status || 'accepted',
    callback_required: data.callback_required !== false,
    received_at: data.received_at || new Date().toISOString(),
    missing_phone: !req.caller_phone,
  });
}

export function makeManagerQueueAdapter() {
  return {
    managerQueue: { send },
  };
}
