// dispatcher.js — function-call dispatcher (production worker version).
//
// REPLACES the stub dispatcher in the spike. The interface is the
// same — input (name, args), output (result object) — but the
// implementations are real product integrations:
//
//   - availability.check   -> ResDiary API: GET /availability?date&time&party_size
//   - booking.create       -> ResDiary API: POST /bookings  +  Depos: hold deposit
//   - manager.escalate     -> Porto Douro callback queue: POST /messages
//
// Each dispatcher function is async and may return a Promise. The
// caller (xai-client.js event loop) is responsible for awaiting
// the result before calling xai.sendFunctionResult().
//
// The dispatcher is the only file that needs to change when the
// ResDiary / Depos / manager-queue credentials become available.

const RESDIARY_BASE = process.env.RESDIARY_BASE_URL || 'https://api.resdiary.com/v1';
const RESDIARY_KEY = process.env.RESDIARY_API_KEY;
const DEPOS_BASE = process.env.DEPOS_BASE_URL || 'https://api.deposits.com/v1';
const DEPOS_KEY = process.env.DEPOS_API_KEY;
const MANAGER_QUEUE_URL = process.env.MANAGER_QUEUE_URL || '';
const MANAGER_QUEUE_KEY = process.env.MANAGER_QUEUE_KEY;

async function resdiaryGet(path, params) {
  const url = new URL(RESDIARY_BASE + path);
  url.search = new URLSearchParams(params).toString();
  const res = await fetch(url, {
    headers: {
      'Authorization': `Bearer ${RESDIARY_KEY}`,
      'Accept': 'application/json',
    },
  });
  if (!res.ok) {
    const body = await res.text().catch(() => '');
    throw new Error(`ResDiary ${path} ${res.status}: ${body.slice(0, 200)}`);
  }
  return res.json();
}

async function resdiaryPost(path, body) {
  const res = await fetch(RESDIARY_BASE + path, {
    method: 'POST',
    headers: {
      'Authorization': `Bearer ${RESDIARY_KEY}`,
      'Content-Type': 'application/json',
      'Accept': 'application/json',
    },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    const errBody = await res.text().catch(() => '');
    throw new Error(`ResDiary ${path} ${res.status}: ${errBody.slice(0, 200)}`);
  }
  return res.json();
}

async function deposHold({ bookingId, amountCents, currency, customerEmail }) {
  // Depos holds a deposit on the customer's card. The hold is
  // captured on booking confirmation and released on cancel.
  const res = await fetch(DEPOS_BASE + '/holds', {
    method: 'POST',
    headers: {
      'Authorization': `Bearer ${DEPOS_KEY}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      booking_id: bookingId,
      amount_cents: amountCents,
      currency,
      customer_email: customerEmail,
    }),
  });
  if (!res.ok) {
    const body = await res.text().catch(() => '');
    throw new Error(`Depos hold ${res.status}: ${body.slice(0, 200)}`);
  }
  return res.json();
}

async function managerMessage({ topic, message, callerName, callerPhone }) {
  const res = await fetch(MANAGER_QUEUE_URL + '/messages', {
    method: 'POST',
    headers: {
      'Authorization': `Bearer ${MANAGER_QUEUE_KEY}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      topic,
      message,
      caller_name: callerName,
      caller_phone: callerPhone,
      received_at: new Date().toISOString(),
    }),
  });
  if (!res.ok) {
    const body = await res.text().catch(() => '');
    throw new Error(`Manager queue ${res.status}: ${body.slice(0, 200)}`);
  }
  return res.json();
}

export async function dispatchToolCall(name, args) {
  switch (name) {
    case 'availability.check': {
      const { date, time, party_size } = args;
      const data = await resdiaryGet('/availability', { date, time, party_size });
      // ResDiary returns { available, next_slot, message }. If
      // unavailable, the model should offer the next available slot.
      return {
        available: !!data.available,
        next_slot: data.next_slot || null,
        message: data.message || (data.available ? 'A table is available.' : 'No tables available at that time.'),
      };
    }
    case 'booking.create': {
      // 1. Create the booking in ResDiary.
      const { date, time, party_size, name, phone, notes } = args;
      const booking = await resdiaryPost('/bookings', {
        date, time, party_size, customer: { name, phone }, notes,
      });
      // 2. Hold the deposit on the customer's card via Depos.
      //    TODO: confirm amount/currency with the manager.
      const hold = await deposHold({
        bookingId: booking.id,
        amountCents: 2000, // £20 deposit per cover, TBD
        currency: 'GBP',
        customerEmail: booking.customer?.email,
      });
      return {
        status: 'created',
        confirmation_id: booking.confirmation_code,
        booking_id: booking.id,
        deposit_hold_id: hold.hold_id,
      };
    }
    case 'manager.escalate': {
      // Record a callback message. The manager will see it in
      // the queue UI and call back the caller.
      const { topic, message, caller_name, caller_phone } = args;
      const m = await managerMessage({
        topic,
        message,
        callerName: caller_name,
        callerPhone: caller_phone,
      });
      return {
        status: 'message_taken',
        callback_required: true,
        message_id: m.id,
      };
    }
    default:
      return {
        error: 'unknown_tool',
        detail: `dispatcher has no handler for ${JSON.stringify(name)}`,
      };
  }
}
