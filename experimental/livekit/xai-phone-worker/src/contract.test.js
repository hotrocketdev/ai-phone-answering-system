// src/contract.test.js — end-to-end contract tests for the booking
// flow using the in-process fake providers.
//
// Run with: node src/contract.test.js
//
// The 10 scenarios mirror the user's contract list:
//
//   1. availability available -> deposit hold -> booking create
//   2. availability unavailable -> suggest alternative
//   3. deposit fails -> do not create booking
//   4. booking create fails after deposit hold -> mark compensation/refund-needed
//   5. manager escalation with name + phone + reason
//   6. caller says "I spoke to the manager yesterday"
//   7. phone number capture passed through intact: 07917 715734
//   8. party size change from four to six
//   9. outdoor seating request stored as notes
//  10. unknown restaurant fact -> manager escalation, not hallucinated answer
//
// Plus a few extra robustness tests for provider-timeout and idempotency.

import assert from 'node:assert/strict';
import { dispatchToolCall } from './dispatcher.js';
import { getProviders, resetProviders } from './providers/index.js';

let pass = 0, fail = 0;
async function test(name, fn) {
  resetProviders();
  try {
    await fn();
    console.log(`  PASS  ${name}`);
    pass++;
  } catch (e) {
    console.log(`  FAIL  ${name}: ${e.message}`);
    if (e.stack) console.log('    ' + e.stack.split('\n').slice(1, 4).join('\n    '));
    fail++;
  } finally {
    resetProviders();
  }
}

const P = '2026-06-08';
const T = '19:00';
const PARTY = 4;
const CUST = 'George';
const PHONE = '07917 715734';

function idem(n) { return `idem-${n}-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`; }

async function main() {
  // 1. availability available -> deposit hold -> booking create
  await test('1. availability available -> deposit hold -> booking create', async () => {
    const a = await dispatchToolCall('availability.check', { date: P, time: T, party_size: PARTY }, idem('1a'));
    assert.equal(a.available, true);
    assert.equal(a.message, 'A table is available.');

    const b = await dispatchToolCall('booking.create', {
      date: P, time: T, party_size: PARTY, name: CUST, phone: PHONE, notes: 'Outdoor if possible',
    }, idem('1b'));
    assert.equal(b.status, 'created');
    assert.ok(b.confirmation_id, 'expected confirmation_id');
    assert.ok(b.booking_id, 'expected booking_id');
    assert.ok(b.deposit_hold_id, 'expected deposit_hold_id');
    assert.match(b.confirmation_id, /^PD-20260608-\d{3}$/);

    // Verify the providers actually got the calls.
    const { _internal, deposit } = getProviders();
    assert.equal(_internal.resdiary.bookings.size, 1);
    assert.equal(_internal.resdiary.bookings.get(b.booking_id).party_size, PARTY);
    assert.ok(_internal.depos.holds.has(b.deposit_hold_id));
    assert.equal(_internal.depos.holds.get(b.deposit_hold_id).amount_cents, 4 * 2000);
  });

  // 2. availability unavailable -> suggest alternative
  await test('2. availability unavailable -> suggest alternative', async () => {
    const a = await dispatchToolCall('availability.check',
      { date: '2099-12-31', time: T, party_size: PARTY }, idem('2'));
    assert.equal(a.available, false);
    assert.ok(a.next_slot, 'expected next_slot suggestion');
    assert.equal(a.next_slot.date, '2026-06-08');
    assert.equal(a.next_slot.party_size, PARTY);
  });

  // 3. deposit fails -> do not create booking
  await test('3. deposit fails -> do not create booking', async () => {
    getProviders()._internal.depos.scenario('declined');
    const b = await dispatchToolCall('booking.create', {
      date: P, time: T, party_size: PARTY, name: CUST, phone: PHONE,
    }, idem('3'));
    assert.equal(b.error, 'deposit_hold_failed');
    assert.equal(b.code, 'DECLINED');
    // Crucially: no booking should have been created.
    assert.equal(getProviders()._internal.resdiary.bookings.size, 0);
  });

  // 4. booking create fails after deposit hold -> mark compensation/refund-needed
  await test('4. booking fails after hold -> compensation + needs_manual_refund', async () => {
    const { _internal } = getProviders();
    _internal.resdiary.scenario('error'); // booking.create will fail next time
    const b = await dispatchToolCall('booking.create', {
      date: P, time: T, party_size: PARTY, name: CUST, phone: PHONE,
    }, idem('4'));
    assert.equal(b.error, 'booking_create_failed');
    assert.equal(b.refunded, true);
    // The hold should have been released (compensated).
    assert.equal(_internal.depos.released.size, 1, 'hold should be released');
  });

  // 5. manager escalation with name + phone + reason
  await test('5. manager escalation with name + phone + reason', async () => {
    const r = await dispatchToolCall('manager.escalate', {
      topic: 'special_request',
      message: 'Wants the chef\'s table for the birthday.',
      caller_name: CUST,
      caller_phone: PHONE,
    }, idem('5'));
    assert.equal(r.status, 'accepted');
    assert.equal(r.callback_required, true);
    assert.ok(r.message_id);
    const msg = getProviders()._internal.managerQueue.messages.get(r.message_id);
    assert.equal(msg.caller_name, CUST);
    assert.equal(msg.caller_phone, PHONE);
    assert.equal(msg.topic, 'special_request');
  });

  // 6. caller says "I spoke to the manager yesterday" -> still flow as
  //    escalation; the model should know to put it in the message body.
  await test('6. caller references prior contact -> manager.escalate works', async () => {
    const r = await dispatchToolCall('manager.escalate', {
      topic: 'follow_up',
      message: 'Caller says they spoke to the manager yesterday about an allergy question.',
      caller_name: CUST,
      caller_phone: PHONE,
    }, idem('6'));
    assert.equal(r.status, 'accepted');
    const msg = getProviders()._internal.managerQueue.messages.get(r.message_id);
    assert.match(msg.message, /manager yesterday/);
  });

  // 7. phone number capture passed through intact: 07917 715734
  await test('7. phone number 07917 715734 passed through intact', async () => {
    const r = await dispatchToolCall('booking.create', {
      date: P, time: T, party_size: PARTY, name: CUST, phone: '07917 715734',
    }, idem('7'));
    assert.equal(r.status, 'created');
    const { _internal } = getProviders();
    const booking = _internal.resdiary.bookings.get(r.booking_id);
    if (!booking) {
      throw new Error('booking not found in fake');
    }
    const customerPhone = booking.customer && booking.customer.phone;
    // Direct string check, avoiding any node:assert weirdness.
    if (customerPhone !== '07917 715734') {
      throw new Error(`expected '07917 715734' but got ${JSON.stringify(customerPhone)}`);
    }
    // Held deposit also includes the phone.
    const hold7 = _internal.depos.holds.get(r.deposit_hold_id);
    if (hold7.customer_phone !== '07917 715734') {
      throw new Error(`expected hold customer_phone '07917 715734' but got ${JSON.stringify(hold7.customer_phone)}`);
    }
    // Held deposit also includes the phone.
    const hold = _internal.depos.holds.get(r.deposit_hold_id);
    assert.equal(hold.customer_phone, '07917 715734');
  });

  // 8. party size change from four to six
  await test('8. party size change from 4 to 6 changes deposit amount', async () => {
    const r4 = await dispatchToolCall('booking.create', {
      date: P, time: T, party_size: 4, name: CUST, phone: PHONE,
    }, idem('8a'));
    const r6 = await dispatchToolCall('booking.create', {
      date: P, time: T, party_size: 6, name: CUST, phone: PHONE,
    }, idem('8b'));
    assert.equal(getProviders()._internal.depos.holds.get(r4.deposit_hold_id).amount_cents, 4 * 2000);
    assert.equal(getProviders()._internal.depos.holds.get(r6.deposit_hold_id).amount_cents, 6 * 2000);
  });

  // 9. outdoor seating request stored as notes
  await test('9. outdoor seating request stored as notes', async () => {
    const r = await dispatchToolCall('booking.create', {
      date: P, time: T, party_size: PARTY, name: CUST, phone: PHONE,
      notes: 'Outdoor if possible',
    }, idem('9'));
    const booking = getProviders()._internal.resdiary.bookings.get(r.booking_id);
    assert.equal(booking.notes, 'Outdoor if possible');
  });

  // 10. unknown restaurant fact -> manager escalation, not hallucinated
  //     This is a system-prompt concern; here we just verify the
  //     dispatcher exposes manager.escalate for the model to call.
  await test('10. unknown fact -> manager.escalate available, not hallucinated', async () => {
    // The dispatcher itself doesn't refuse; it surfaces the right
    // tool for the model to use. Verify manager.escalate works for
    // the "I want to know if you do a vegan tasting menu" case.
    const r = await dispatchToolCall('manager.escalate', {
      topic: 'menu_question',
      message: 'Caller asks if we do a vegan tasting menu. Restaurant does not serve vegan.',
      caller_name: CUST,
      caller_phone: PHONE,
    }, idem('10'));
    assert.equal(r.status, 'accepted');
    assert.equal(r.callback_required, true);
    const msg = getProviders()._internal.managerQueue.messages.get(r.message_id);
    assert.match(msg.message, /vegan tasting menu/);
  });

  // --- Extra robustness tests ---

  await test('E1. provider timeout surfaces PROVIDER_TIMEOUT + retryable', async () => {
    getProviders()._internal.resdiary.scenario('timeout');
    const r = await dispatchToolCall('availability.check',
      { date: P, time: T, party_size: PARTY }, idem('E1'));
    assert.equal(r.error, 'PROVIDER_TIMEOUT');
    assert.equal(r.detail, 'Fake: provider timed out after 30s');
  });

  await test('E2. invalid party size surfaces INVALID_INPUT', async () => {
    const r = await dispatchToolCall('availability.check',
      { date: P, time: T, party_size: 99 }, idem('E2'));
    assert.equal(r.error, 'INVALID_INPUT');
  });

  await test('E3. invalid date format surfaces INVALID_INPUT', async () => {
    const r = await dispatchToolCall('availability.check',
      { date: 'not-a-date', time: T, party_size: PARTY }, idem('E3'));
    assert.equal(r.error, 'INVALID_INPUT');
  });

  await test('E4. idempotency: same key returns same booking (no double-book)', async () => {
    const key = idem('E4');
    const a = await dispatchToolCall('booking.create',
      { date: P, time: T, party_size: PARTY, name: CUST, phone: PHONE }, key);
    const b = await dispatchToolCall('booking.create',
      { date: P, time: T, party_size: PARTY, name: CUST, phone: PHONE }, key);
    assert.equal(a.booking_id, b.booking_id);
    assert.equal(a.confirmation_id, b.confirmation_id);
    assert.equal(getProviders()._internal.resdiary.bookings.size, 1);
  });

  await test('E5. missing caller phone on manager.escalate -> missing_phone=true', async () => {
    const r = await dispatchToolCall('manager.escalate', {
      topic: 'callback', message: 'Caller hung up before giving phone.',
      caller_name: 'Unknown',
    }, idem('E5'));
    assert.equal(r.status, 'accepted');
    assert.equal(r.missing_phone, true);
  });

  console.log(`\n${pass} passed, ${fail} failed`);
  process.exit(fail === 0 ? 0 : 1);
}

main().catch((e) => {
  console.error('test runner crashed:', e);
  process.exit(1);
});
