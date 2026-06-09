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
//
// Provider contracts live in src/providers.ts (frozen TypeScript-style
// interfaces). The JSDoc typedefs below mirror those for the Node
// runtime. Provider implementations (src/providers/*.js) are swappable
// via the factory in src/providers/index.js.
//
// @typedef {import('./providers.ts').AvailabilityRequest} AvailabilityRequest
// @typedef {import('./providers.ts').BookingCreateRequest} BookingCreateRequest
// @typedef {import('./providers.ts').DepositHoldRequest} DepositHoldRequest
// @typedef {import('./providers.ts').ManagerEscalationRequest} ManagerEscalationRequest
// @typedef {import('./providers.ts').Result} Result
// @typedef {import('./providers.ts').ProviderError} ProviderError
// @typedef {import('./providers.ts').BookingProviderSet} BookingProviderSet

import { getProviders } from './providers/index.js';

const DEPOSIT_PENCE_PER_COVER = parseInt(process.env.DEPOSIT_PENCE_PER_COVER || '2000', 10);

/**
 * Orchestrate the full booking flow:
 *   1. ResDiary check availability
 *   2. (if available) Depos hold deposit
 *   3. ResDiary create booking
 *   4. (if booking fails) Depos compensate the hold
 *
 * The dispatcher is the ONLY place that knows the flow. The
 * individual providers don't call each other.
 *
 * @param {string} name - tool name (availability.check, booking.create, manager.escalate, etc.)
 * @param {Record<string, any>} args - tool args from the xAI function_call
 * @param {string} idempotencyKey - per-call idempotency key, supplied by the xai-client event loop
 * @returns {Promise<object>} xAI function_call_output payload
 */
export async function dispatchToolCall(name, args, idempotencyKey) {
  if (!idempotencyKey) {
    throw new Error('dispatchToolCall: idempotencyKey is required');
  }
  const { availability, deposit, booking, managerQueue } = getProviders();

  switch (name) {
    case 'availability.check': {
      const req = { date: args.date, time: args.time, party_size: args.party_size };
      const r = await availability.check(req, idempotencyKey);
      if (!r.ok) return { error: r.error.code, detail: r.error.message };
      return {
        available: r.value.available,
        next_slot: r.value.next_slot,
        message: r.value.message,
      };
    }

    case 'booking.create': {
      const { date, time, party_size, name: customerName, phone, notes } = args;
      const amountCents = party_size * DEPOSIT_PENCE_PER_COVER;

      // 1. Hold the deposit FIRST. If the hold fails, we never
      //    touch the booking system.
      const holdReq = {
        booking_id: 'pending-' + idempotencyKey,
        amount_cents: amountCents,
        currency: 'GBP',
        customer_email: args.email || 'unknown@example.com',
        customer_phone: phone,
        customer_name: customerName,
        idempotency_key: idempotencyKey + ':hold',
        expires_in_seconds: 24 * 60 * 60,
      };
      const holdR = await deposit.hold(holdReq);
      if (!holdR.ok) {
        return { error: 'deposit_hold_failed', detail: holdR.error.message, code: holdR.error.code };
      }
      const holdId = holdR.value.hold_id;

      // 2. Create the booking in ResDiary.
      const bookReq = {
        date, time, party_size,
        customer: { name: customerName, phone, email: args.email },
        notes,
        deposit_hold_id: holdId,
        idempotency_key: idempotencyKey + ':book',
      };
      const bookR = await booking.create(bookReq);
      if (!bookR.ok) {
        // 3. Compensation: release the hold so the customer isn't
        //    charged for a booking that never went through.
        const compR = await deposit.compensate(
          { hold_id: holdId, reason: 'booking_failed' },
          idempotencyKey + ':compensate',
        );
        if (!compR.ok) {
          return {
            error: 'booking_create_failed_and_compensation_failed',
            detail: `booking: ${bookR.error.message}; compensate: ${compR.error.message}`,
            hold_id: holdId,
            needs_manual_refund: true,
          };
        }
        return {
          error: 'booking_create_failed',
          detail: bookR.error.message,
          hold_id: holdId,
          refunded: true,
        };
      }

      return {
        status: bookR.value.status,
        confirmation_id: bookR.value.confirmation_code,
        booking_id: bookR.value.id,
        deposit_hold_id: holdId,
      };
    }

    case 'manager.escalate': {
      const { topic, message, caller_name, caller_phone } = args;
      const r = await managerQueue.send(
        {
          topic,
          message,
          caller_name,
          caller_phone,
          booking_context: {
            date: args.date,
            time: args.time,
            party_size: args.party_size,
          },
        },
        idempotencyKey + ':escalate',
      );
      if (!r.ok) return { error: r.error.code, detail: r.error.message };
      return {
        status: r.value.status,
        callback_required: r.value.callback_required,
        message_id: r.value.id,
        missing_phone: r.value.missing_phone,
      };
    }

    default:
      return {
        error: 'unknown_tool',
        detail: `dispatcher has no handler for ${JSON.stringify(name)}`,
      };
  }
}
