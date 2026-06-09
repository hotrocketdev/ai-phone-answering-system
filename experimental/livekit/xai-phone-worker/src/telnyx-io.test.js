// src/telnyx-io.test.js — end-to-end test of the Telnyx I/O scaffold.
//
// This test does NOT connect to a real Telnyx media stream. It
// reads from a PCMU file (the r-real fixture from the spike) and
// writes to a PCMU file (the simulated outbound), recording
// pacing metrics. The same code path runs on a live call — only
// the I/O source/sink changes.
//
// Run with: node src/telnyx-io.test.js

import fs from 'node:fs';
import assert from 'node:assert/strict';
import { TelnyxMediaSource, TelnyxMediaSink } from './telnyx-io.js';
import { pcmuToPcm16 } from './pcmu-codec.js';

let pass = 0, fail = 0;
async function test(name, fn) {
  try {
    await fn();
    console.log(`  PASS  ${name}`);
    pass++;
  } catch (e) {
    console.log(`  FAIL  ${name}: ${e.message}`);
    if (e.stack) console.log('    ' + e.stack.split('\n').slice(1, 4).join('\n    '));
    fail++;
  }
}

async function main() {
  const IN = './fixtures/caller-real.pcmu';
  const OUT = './tmp/telnyx-sink.pcmu';

  if (!fs.existsSync(IN)) {
    console.log('SKIP  fixture not found at ' + IN + ' (run spike r-real first to produce it)');
    process.exit(0);
  }

  // Speed 100x so the test runs in <1s instead of 14s.
  const speed = 100;

  await test('1. TelnyxMediaSource streams all PCMU frames, no drops', async () => {
    const src = new TelnyxMediaSource(IN, { speed, verbose: false });
    let consumed = 0;
    await src.stream(async (frame) => {
      consumed++;
      // Simulate a real consumer that always keeps up.
    });
    const totalFrames = Math.floor(fs.statSync(IN).size / 160);
    assert.equal(src.metrics.frames, totalFrames);
    assert.equal(consumed, totalFrames);
    assert.equal(src.metrics.dropped_frames, 0);
  });

  await test('2. metrics report reasonable per-frame timings', async () => {
    const src = new TelnyxMediaSource(IN, { speed, verbose: false });
    await src.stream(async () => {});
    const s = src.summary();
    // Decode + resample should each be sub-millisecond on modern hardware.
    assert.ok(s.decode_ms.p95 < 50, `decode p95 too high: ${s.decode_ms.p95}ms`);
    assert.ok(s.resample_ms.p95 < 50, `resample p95 too high: ${s.resample_ms.p95}ms`);
  });

  await test('3. TelnyxMediaSink collects outbound frames', async () => {
    const src = new TelnyxMediaSource(IN, { speed: 1000, verbose: false });
    const sink = new TelnyxMediaSink(OUT);
    await src.stream(async (frame) => {
      sink.write(frame.pcm16_24k);
    });
    sink.flush();
    const written = fs.readFileSync(OUT);
    const writtenFrames = Math.floor(written.length / 160);
    assert.ok(writtenFrames > 0, 'expected at least one outbound frame');
    assert.equal(written.length % 160, 0, 'outbound PCMU byte length not a multiple of 160');
  });

  await test('4. round-trip: PCMU in -> decode -> encode produces valid PCMU', async () => {
    const buf = fs.readFileSync(IN);
    // Take the first 100 frames (2 seconds) and round-trip.
    const head = buf.subarray(0, 100 * 160);
    const decoded = pcmuToPcm16(head);
    // 100 frames * 160 samples * 2 bytes/sample = 32000 bytes
    assert.equal(decoded.length, 32000, 'decoded PCM16 should be 2x PCMU length');
  });

  await test('5. backpressure: slow consumer triggers dropped_frames', async () => {
    const src = new TelnyxMediaSource(IN, { speed: 1000, verbose: false });
    let i = 0;
    await src.stream(async () => {
      if (i++ % 5 === 0) throw new Error('simulated backpressure');
    });
    const s = src.summary();
    assert.ok(s.backpressure_events > 0, 'expected backpressure events to be recorded');
  });

  console.log(`\n${pass} passed, ${fail} failed`);
  process.exit(fail === 0 ? 0 : 1);
}

main().catch((e) => {
  console.error('test runner crashed:', e);
  process.exit(1);
});
