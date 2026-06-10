// src/contract.test.js — vendor contract smoke tests.
//
// STATUS: WORKING for the in-memory stub vendors (memory/redis-mem.js,
// transport/file-loopback.js, stt/xai.js). Throws for the SDK-backed
// vendors (deepgram, elevenlabs, cerebras, livekit) since those aren't
// installed yet.
//
// Run with: npm test (alias for test:contract)

import assert from 'node:assert/strict';
import { XaiBundledStt } from './vendors/stt/xai.js';
import { XaiEveTts } from './vendors/tts/xai-eve.js';
import { FileLoopbackTransport } from './vendors/transport/file-loopback.js';
import { RedisMemMemory } from './vendors/memory/redis-mem.js';
import { makeBundle } from './vendors/bundles.js';

let passed = 0;
let failed = 0;
function test(name: string, fn: () => void | Promise<void>) {
  return Promise.resolve()
    .then(fn)
    .then(() => { passed++; console.log(`  PASS  ${name}`); })
    .catch((e) => { failed++; console.error(`  FAIL  ${name}: ${e.message}`); });
}

async function main() {
  console.log('--- Vendor contract smoke tests ---\n');

  // 1. In-memory memory stub round-trip.
  await test('memory/redis-mem: set + get round-trip', async () => {
    const m = new RedisMemMemory();
    await m.set('+447917715734', { phone: '+447917715734', name: 'George' });
    const got = await m.get('+447917715734');
    assert.equal(got?.name, 'George');
  });

  await test('memory/redis-mem: missing phone returns null', async () => {
    const m = new RedisMemMemory();
    const got = await m.get('+440000000000');
    assert.equal(got, null);
  });

  await test('memory/redis-mem: append history', async () => {
    const m = new RedisMemMemory();
    await m.append('+447917715734', { ts_ms: 1, summary: 'booked table for 4' });
    // No getter for history in this stub; just ensure no throw.
  });

  // 2. File-loopback transport.
  await test('transport/file-loopback: connect reads file', async () => {
    const t = new FileLoopbackTransport({
      file_path: 'fixtures/rehearsal/t01-hello.pcmu',
      format: 'pcmu_8k',
    });
    await t.connect();
    const snap = t.snapshot();
    assert.equal(snap.frames_in, 0);  // nothing played yet
  });

  // 3. STT vendor names.
  await test('stt/xai: name is "xai"', () => {
    const s = new XaiBundledStt();
    assert.equal(s.name, 'xai');
  });

  await test('stt/deepgram: throws on missing API key', () => {
    // Lazy import to avoid loading @deepgram/sdk if not installed.
    assert.throws(() => {
      // @ts-ignore
      const { DeepgramStt } = require('./vendors/stt/deepgram.js');
      new DeepgramStt({ apiKey: '' });
    }, /apiKey is required/);
  });

  // 4. TTS vendor names.
  await test('tts/xai-eve: name is "xai-eve"', () => {
    const t = new XaiEveTts();
    assert.equal(t.name, 'xai-eve');
  });

  await test('tts/elevenlabs: throws on missing API key', () => {
    assert.throws(() => {
      // @ts-ignore
      const { ElevenLabsTts } = require('./vendors/tts/elevenlabs.js');
      new ElevenLabsTts({ apiKey: '' });
    }, /apiKey is required/);
  });

  // 5. Bundle factory.
  await test('bundles: make xai-bundle with no env succeeds', async () => {
    const b = await makeBundle('xai-bundle');
    assert.equal(b.name, 'xai-bundle');
  });

  await test('bundles: make hybrid-deepgram without DEEPGRAM_API_KEY throws', async () => {
    const prev = process.env.DEEPGRAM_API_KEY;
    delete process.env.DEEPGRAM_API_KEY;
    try {
      await assert.rejects(() => makeBundle('hybrid-deepgram'), /deepgram_api_key/);
    } finally {
      if (prev) process.env.DEEPGRAM_API_KEY = prev;
    }
  });

  await test('bundles: make hybrid-elevenlabs without ELEVENLABS_API_KEY throws', async () => {
    const prev = process.env.ELEVENLABS_API_KEY;
    delete process.env.ELEVENLABS_API_KEY;
    try {
      await assert.rejects(() => makeBundle('hybrid-elevenlabs'), /elevenlabs_api_key/);
    } finally {
      if (prev) process.env.ELEVENLABS_API_KEY = prev;
    }
  });

  console.log(`\n--- ${passed} passed, ${failed} failed ---`);
  process.exit(failed === 0 ? 0 : 1);
}

main().catch((e) => { console.error(e); process.exit(1); });
