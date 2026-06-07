#!/usr/bin/env node
/**
 * gen-caller-fixture.mjs
 * ----------------------
 * Generates a deterministic synthetic caller fixture for testing the xAI
 * phone-harness function-call bridge with REAL speech (vs the 250Hz tone
 * used in r1-r6).
 *
 * Pipeline:
 *   Cartesia Sonic (British voice "Gemma") -> WAV (PCM16)
 *   -> ffmpeg -> PCMU 8kHz mono (.pcmu) the harness ingests
 *
 * Why Cartesia and not Windows SAPI: SAPI voices are robotic enough that a
 * failed STT could be blamed on voice quality rather than the bridge. Using
 * the British voice already in the stack gives a realistic caller AND
 * exercises British-accent STT at the same time.
 *
 * The phrase is chosen to FORCE both tool calls: it contains a date/time/
 * party-size (availability.check) and an explicit booking intent with name +
 * number (booking.create). If the bridge is wired correctly, the model has a
 * concrete reason to fire both.
 *
 * Usage:
 *   export CARTESIA_API_KEY=...        # required
 *   node gen-caller-fixture.mjs
 *
 * Output (in ./fixtures/):
 *   caller-booking-test.wav    <- listen to sanity-check the phrase
 *   caller-booking-test.pcmu   <- feed THIS to the harness
 *
 * Requirements: Node 18+ (global fetch), ffmpeg on PATH.
 *
 * NOTE: This is a TEST HARNESS artifact. It does not touch production,
 * .env, systemd, the Telnyx webhook, or the gateway. Keep it under
 * experimental/.
 */

import { writeFile, mkdir } from 'node:fs/promises';
import { spawn } from 'node:child_process';
import { existsSync } from 'node:fs';
import path from 'node:path';

// --- Config -----------------------------------------------------------------

const CARTESIA_API_KEY = process.env.CARTESIA_API_KEY;
const CARTESIA_VERSION = process.env.CARTESIA_VERSION ?? '2025-04-16';

// British voice "Gemma" — the production renderer voice. Override via env
// if production rotates voices.
const VOICE_ID =
  process.env.CARTESIA_VOICE_ID ?? '2f251ac3-89a9-4a77-a452-704b474ccd01';

// Sonic 3.5 (per the production .env CARTESIA_MODEL). Override via env if
// you bump the model.
const MODEL_ID = process.env.CARTESIA_MODEL ?? 'sonic-3.5';

// The caller line. Crafted to FORCE BOTH tool calls:
//   - date + time + party size  -> availability.check
//   - explicit "book" + name + phone  -> booking.create
// The phone number is spaced out digit-by-digit because that's how TTS
// engines reliably pronounce a UK mobile, and it doubles as a test of
// whether STT captures digits cleanly over 8kHz mu-law.
const PHRASE =
  process.env.CALLER_PHRASE ??
  "Hi, can I book a table for tomorrow at 7 for 4 people? " +
  "My name is George, and my number is 0 7 9 1 7, 7 1 5 7 3 4.";

const OUT_DIR = path.resolve('./fixtures');
const WAV_PATH = path.join(OUT_DIR, 'caller-booking-test.wav');
const PCMU_PATH = path.join(OUT_DIR, 'caller-booking-test.pcmu');

// Cartesia can emit WAV directly. We request PCM16 @ 44.1k (clean source),
// then let ffmpeg downsample to 8k mulaw — matching the r6 conversion path.
const TTS_SAMPLE_RATE = 44100;

// --- Helpers ----------------------------------------------------------------

function die(msg) {
  console.error(`\n[gen-caller-fixture] ERROR: ${msg}\n`);
  process.exit(1);
}

function run(cmd, args) {
  return new Promise((resolve, reject) => {
    const p = spawn(cmd, args, { stdio: ['ignore', 'inherit', 'inherit'] });
    p.on('error', reject);
    p.on('close', (code) =>
      code === 0 ? resolve() : reject(new Error(`${cmd} exited ${code}`)),
    );
  });
}

// --- Main -------------------------------------------------------------------

async function main() {
  if (!CARTESIA_API_KEY) die('CARTESIA_API_KEY is not set.');

  await mkdir(OUT_DIR, { recursive: true });

  console.log('[1/3] Requesting TTS from Cartesia (voice: Gemma, model:', MODEL_ID + ')...');
  console.log(`      Phrase: "${PHRASE}"`);

  // Cartesia REST: POST /tts/bytes returns raw audio in the requested format.
  const res = await fetch('https://api.cartesia.ai/tts/bytes', {
    method: 'POST',
    headers: {
      'Cartesia-Version': CARTESIA_VERSION,
      'Authorization': `Bearer ${CARTESIA_API_KEY}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      model_id: MODEL_ID,
      transcript: PHRASE,
      voice: { mode: 'id', id: VOICE_ID },
      output_format: {
        container: 'wav',
        encoding: 'pcm_s16le',
        sample_rate: TTS_SAMPLE_RATE,
      },
      language: 'en',
    }),
  });

  if (!res.ok) {
    const body = await res.text().catch(() => '');
    die(`Cartesia returned ${res.status}: ${body.slice(0, 500)}`);
  }

  const wavBuf = Buffer.from(await res.arrayBuffer());
  await writeFile(WAV_PATH, wavBuf);
  console.log(`      Wrote ${WAV_PATH} (${wavBuf.length} bytes)`);

  console.log('[2/3] Converting WAV -> PCMU 8kHz mono (matches r6 path)...');
  // Identical to the conversion validated in r6:
  //   ffmpeg -i input.wav -ac 1 -ar 8000 -f mulaw output.pcmu
  await run('ffmpeg', [
    '-y',
    '-i', WAV_PATH,
    '-ac', '1',
    '-ar', '8000',
    '-f', 'mulaw',
    PCMU_PATH,
  ]);

  if (!existsSync(PCMU_PATH)) die('ffmpeg did not produce the .pcmu file.');

  console.log('[3/3] Done.');
  console.log(`
Fixtures ready:
  ${WAV_PATH}   <- listen to sanity-check the phrase
  ${PCMU_PATH}  <- feed THIS to the phone harness

Next:
  1. Run the harness against caller-booking-test.pcmu (your client-side
     PCMU->PCM16 24k upsample, then xAI WSS).
  2. Confirm in the log:
       - response.function_call_arguments.done fires for availability.check
       - then for booking.create
       - each with a real call_id (not empty)
       - assistant resumes after function_call_output + response.create
  3. If both fire with real call_ids -> bridge is validated on the phone path.
  4. Then listen to the output WAV to confirm Eve's voice survived round-trip.
  5. Realism pass: re-record the same phrase in your own voice and re-run.
`);
}

main().catch((e) => die(e.message ?? String(e)));
