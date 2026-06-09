#!/usr/bin/env node
/**
 * gen-rehearsal-fixtures-opus24k.mjs
 * ---------------------------------
 * Generates the 7 caller-turn PCM16 24 kHz fixtures for the
 * "what does xAI sound like at full bandwidth" rehearsal.
 *
 * Same 7 phrases as gen-rehearsal-fixtures.mjs, but outputs
 * raw PCM16 24 kHz mono (no PCMU round-trip). The caller audio
 * is now full-bandwidth, so xAI's STT receives the actual
 * voice quality (not the telephony 4 kHz cap).
 *
 * Pipeline:
 *   Cartesia Sonic 3.5 (Gemma British) -> WAV (PCM16 44.1k)
 *   -> ffmpeg -> PCM16 24kHz mono (.pcm16_24k)
 *
 * Output (in ./fixtures/rehearsal/):
 *   t01-hello.pcm16_24k
 *   t02-book.pcm16_24k
 *   ...
 *
 * Run with: CARTESIA_API_KEY=... node scripts/gen-rehearsal-fixtures-opus24k.mjs
 */

import { writeFile, mkdir } from 'node:fs/promises';
import { spawn } from 'node:child_process';
import { existsSync } from 'node:fs';
import path from 'node:path';

const CARTESIA_API_KEY = process.env.CARTESIA_API_KEY;
const CARTESIA_VERSION = process.env.CARTESIA_VERSION ?? '2025-04-16';
const VOICE_ID = process.env.CARTESIA_VOICE_ID ?? '2f251ac3-89a9-4a77-a452-704b474ccd01';
const MODEL_ID = process.env.CARTESIA_MODEL ?? 'sonic-3.5';

const PHRASES = [
  { id: 't01-hello',        text: 'Hi.' },
  { id: 't02-book',         text: 'Can I book a table please?' },
  { id: 't03-tomorrow-7-4', text: 'Tomorrow at seven for four people.' },
  { id: 't07-george',       text: 'George.' },
  { id: 't09-phone',        text: 'Zero seven nine one seven, seven one five seven three four.' },
  { id: 't14-change-to-6',  text: 'Actually, make that six people, not four.' },
  { id: 't16-off-script',   text: 'Do you have a vegan tasting menu?' },
];

const OUT_DIR = path.resolve('./fixtures/rehearsal');

function die(msg) {
  console.error(`\n[gen-rehearsal-fixtures-opus24k] ERROR: ${msg}\n`);
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

async function synth(text) {
  const res = await fetch('https://api.cartesia.ai/tts/bytes', {
    method: 'POST',
    headers: {
      'Cartesia-Version': CARTESIA_VERSION,
      'Authorization': `Bearer ${CARTESIA_API_KEY}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      model_id: MODEL_ID,
      transcript: text,
      voice: { mode: 'id', id: VOICE_ID },
      output_format: {
        container: 'wav',
        encoding: 'pcm_s16le',
        sample_rate: 44100,
      },
      language: 'en',
    }),
  });
  if (!res.ok) {
    const body = await res.text().catch(() => '');
    die(`Cartesia ${res.status}: ${body.slice(0, 300)}`);
  }
  return Buffer.from(await res.arrayBuffer());
}

async function main() {
  if (!CARTESIA_API_KEY) die('CARTESIA_API_KEY not set.');

  await mkdir(OUT_DIR, { recursive: true });
  console.log(`[1/3] Synthesizing ${PHRASES.length} phrases with Cartesia Sonic 3.5 (Gemma)...`);

  for (let i = 0; i < PHRASES.length; i++) {
    const p = PHRASES[i];
    const wavBuf = await synth(p.text);
    const wavPath = path.join(OUT_DIR, `${p.id}.wav`);
    await writeFile(wavPath, wavBuf);
    console.log(`      [${i + 1}/${PHRASES.length}] ${p.id} (${p.text.length} chars)`);
  }

  console.log(`[2/3] Converting WAV -> PCM16 24 kHz mono (no PCMU round-trip)...`);
  for (const p of PHRASES) {
    const wavPath = path.join(OUT_DIR, `${p.id}.wav`);
    const pcm16Path = path.join(OUT_DIR, `${p.id}.pcm16_24k`);
    // ffmpeg decode WAV 44.1k -> resample to 24k -> mono -> s16le -> file
    await run('ffmpeg', [
      '-y', '-i', wavPath,
      '-ac', '1',
      '-ar', '24000',
      '-f', 's16le',
      pcm16Path,
    ]);
    if (!existsSync(pcm16Path)) die(`ffmpeg did not produce ${pcm16Path}`);
  }

  console.log(`[3/3] Done.`);
  console.log(`\nFixtures ready in ${OUT_DIR}:`);
  for (const p of PHRASES) {
    console.log(`  ${p.id}.pcm16_24k  <- "${p.text}"`);
  }
}

main().catch((e) => die(e.message ?? String(e)));
