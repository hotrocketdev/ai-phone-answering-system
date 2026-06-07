// dispatcher.test.js — unit tests for the dispatcher, using a
// local fake ResDiary + Depos + manager-queue server.
//
// Run with: node src/dispatcher.test.js
//
// The test sets up an in-process HTTP server on a random port that
// responds to /availability, /bookings, /holds, /messages, then
// imports the dispatcher with the env pointing at it. Each
// function is exercised and the responses are validated.
//
// This is the first thing to run after the user provides the
// production credentials: the tests will fail until the API
// contracts are confirmed.

import http from 'node:http';
import assert from 'node:assert/strict';

// NOTE: dispatcher.js captures RESDIARY_BASE_URL etc at module load.
// Set env BEFORE the dynamic import so the dispatcher sees the
// fake-server URL instead of the production default.
async function loadDispatcherWithEnv(env) {
  for (const [k, v] of Object.entries(env)) {
    process.env[k] = v;
  }
  // Force module re-evaluation by appending a cache-busting query.
  // (Node ESM doesn't support delete require.cache; we use dynamic
  // import with a varying URL.)
  return import(`./dispatcher.js?t=${Date.now()}-${Math.random()}`);
}

// --- Fake servers ----------------------------------------------------------

function startFakeApis() {
  const calls = { availability: 0, bookings: 0, holds: 0, messages: 0 };
  const server = http.createServer((req, res) => {
    let body = '';
    req.on('data', (c) => (body += c));
    req.on('end', () => {
      if (req.url.startsWith('/v1/availability') && req.method === 'GET') {
        calls.availability++;
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ available: true, next_slot: '19:00', message: 'A table is available.' }));
        return;
      }
      if (req.url.startsWith('/v1/bookings') && req.method === 'POST') {
        calls.bookings++;
        const parsed = JSON.parse(body || '{}');
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({
          id: 'b-12345',
          confirmation_code: 'PD-2026-06-08-001',
          customer: { name: parsed.customer?.name || 'Test', email: 'test@example.com' },
        }));
        return;
      }
      if (req.url.startsWith('/v1/holds') && req.method === 'POST') {
        calls.holds++;
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ hold_id: 'h-67890' }));
        return;
      }
      if (req.url.startsWith('/messages') && req.method === 'POST') {
        calls.messages++;
        const parsed = JSON.parse(body || '{}');
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ id: 'm-11111', received_at: new Date().toISOString(), topic: parsed.topic }));
        return;
      }
      res.writeHead(404);
      res.end('not found');
    });
  });
  return new Promise((resolve) => {
    server.listen(0, '127.0.0.1', () => {
      const { port } = server.address();
      resolve({ server, port, calls });
    });
  });
}

// --- Tests -----------------------------------------------------------------

async function main() {
  const { server, port, calls } = await startFakeApis();
  const { dispatchToolCall } = await loadDispatcherWithEnv({
    RESDIARY_BASE_URL: `http://127.0.0.1:${port}/v1`,
    RESDIARY_API_KEY: 'fake-resdiary-key',
    DEPOS_BASE_URL: `http://127.0.0.1:${port}/v1`,
    DEPOS_API_KEY: 'fake-depos-key',
    MANAGER_QUEUE_URL: `http://127.0.0.1:${port}`,
    MANAGER_QUEUE_KEY: 'fake-manager-key',
  });

  let pass = 0, fail = 0;
  async function test(name, fn) {
    try {
      await fn();
      console.log(`  PASS  ${name}`);
      pass++;
    } catch (e) {
      console.log(`  FAIL  ${name}: ${e.message}`);
      fail++;
    }
  }

  await test('availability.check returns available + next_slot', async () => {
    const r = await dispatchToolCall('availability.check', { date: '2026-06-08', time: '19:00', party_size: 4 });
    assert.equal(r.available, true);
    assert.equal(r.next_slot, '19:00');
    assert.equal(calls.availability, 1);
  });

  await test('booking.create hits ResDiary + Depos and returns confirmation', async () => {
    const r = await dispatchToolCall('booking.create', {
      date: '2026-06-08', time: '19:00', party_size: 4,
      name: 'George', phone: '07917715734', notes: 'Outdoor if possible',
    });
    assert.equal(r.status, 'created');
    assert.equal(r.confirmation_id, 'PD-2026-06-08-001');
    assert.equal(calls.bookings, 1);
    assert.equal(calls.holds, 1);
  });

  await test('manager.escalate posts to manager queue', async () => {
    const r = await dispatchToolCall('manager.escalate', {
      topic: 'special_request', message: 'Wants the chef\'s table',
      caller_name: 'George', caller_phone: '07917715734',
    });
    assert.equal(r.status, 'message_taken');
    assert.equal(r.callback_required, true);
    assert.equal(calls.messages, 1);
  });

  await test('unknown tool returns error', async () => {
    const r = await dispatchToolCall('bogus.tool', {});
    assert.equal(r.error, 'unknown_tool');
  });

  console.log(`\n${pass} passed, ${fail} failed`);
  // Defer exit so the server close completes on Windows.
  server.closeAllConnections?.();
  setTimeout(() => server.close(() => process.exit(fail === 0 ? 0 : 1)), 50);
}

main().catch((e) => {
  console.error('test runner crashed:', e);
  process.exit(1);
});
