// pcmu-codec.js — G.711 µ-law (PCMU) <-> PCM16 linear encoding.
//
// Pure-JS, no native deps. The µ-law algorithm is a well-known compander
// (G.711, ITU-T recommendation) used by every PSTN trunk. Telnyx's
// telephony media streams deliver PCMU as 8 kHz, 8-bit, mono.
//
// Reference: ITU-T G.711 (1988). Encoding: 14-bit linear -> 8-bit
// log-companded. Decoding: inverse.
//
// All little-endian. PCMU byte values are unsigned 8-bit; PCM16 values
// are signed 16-bit little-endian.

const BIAS = 0x84;
const CLIP = 32635;

const dec2bin = {};
for (let i = 0; i < 256; i++) dec2bin[i] = (i >>> 0).toString(2).padStart(8, '0');

function encodeSample(sample) {
  // 16-bit linear -> 8-bit µ-law
  let s = sample < 0 ? -sample : sample;
  if (s > CLIP) s = CLIP;
  s += BIAS;
  let exponent = 7;
  for (let expMask = 0x4000; (s & expMask) === 0 && exponent > 0; expMask >>= 1) exponent--;
  const mantissa = (s >> (exponent + 3)) & 0x0f;
  const sign = sample < 0 ? 0x80 : 0x00;
  return (~(sign | (exponent << 4) | mantissa)) & 0xff;
}

function decodeSample(uLaw) {
  // 8-bit µ-law -> 16-bit linear
  uLaw = ~uLaw & 0xff;
  const sign = uLaw & 0x80;
  const exponent = (uLaw >> 4) & 0x07;
  const mantissa = uLaw & 0x0f;
  let sample = ((mantissa << 3) + BIAS) << exponent;
  sample -= BIAS;
  return sign ? -sample : sample;
}

// pcm16ToPcmu(pcm16LE: Buffer) -> Buffer (PCMU bytes, same length as pcm16LE/2)
export function pcm16ToPcmu(pcm16LE) {
  const out = Buffer.allocUnsafe(pcm16LE.length / 2);
  for (let i = 0; i < out.length; i++) {
    const s = pcm16LE.readInt16LE(i * 2);
    out[i] = encodeSample(s);
  }
  return out;
}

// pcmuToPcm16(pcmu: Buffer) -> Buffer (PCM16 little-endian, 2x length)
export function pcmuToPcm16(pcmu) {
  const out = Buffer.allocUnsafe(pcmu.length * 2);
  for (let i = 0; i < pcmu.length; i++) {
    out.writeInt16LE(decodeSample(pcmu[i]), i * 2);
  }
  return out;
}

// pcm24kToPcm8k(pcm24k: Buffer) -> Buffer (simple nearest-neighbour downsample)
// xAI outputs PCM16 24kHz; PCMU is 8 kHz. 3:1 downsample by picking every
// 3rd sample. This is not the highest-quality resampler but is good enough
// for verifying the round-trip in the harness; production code would use
// a polyphase filter.
export function pcm24kToPcm8k(pcm24k) {
  const samplesIn = pcm24k.length / 2;
  const samplesOut = Math.floor(samplesIn / 3);
  const out = Buffer.allocUnsafe(samplesOut * 2);
  for (let i = 0; i < samplesOut; i++) {
    out.writeInt16LE(pcm24k.readInt16LE(i * 3 * 2), i * 2);
  }
  return out;
}

// pcm8kToPcm24k(pcm8k: Buffer) -> Buffer (3x nearest-neighbour upsample)
export function pcm8kToPcm24k(pcm8k) {
  const samplesIn = pcm8k.length / 2;
  const samplesOut = samplesIn * 3;
  const out = Buffer.allocUnsafe(samplesOut * 2);
  for (let i = 0; i < samplesIn; i++) {
    const s = pcm8k.readInt16LE(i * 2);
    out.writeInt16LE(s, i * 6);
    out.writeInt16LE(s, i * 6 + 2);
    out.writeInt16LE(s, i * 6 + 4);
  }
  return out;
}

// Convenience: a 1-second silence buffer at 8 kHz PCMU (8000 zero bytes).
export function silencePcmu(durationMs = 1000, sampleRate = 8000) {
  const n = Math.floor((durationMs * sampleRate) / 1000);
  return Buffer.alloc(n, 0xff); // µ-law "0" encodes to 0xFF
}

// Convenience: a 1-second low-amplitude tone (sine wave at 250 Hz) at
// 8 kHz, encoded as PCMU. Useful for triggering xAI VAD without real
// speech.
export function tonePcmu(freqHz = 250, durationMs = 1000, sampleRate = 8000, amplitude = 8000) {
  const n = Math.floor((durationMs * sampleRate) / 1000);
  const pcm = Buffer.alloc(n * 2);
  for (let i = 0; i < n; i++) {
    const t = i / sampleRate;
    const s = Math.round(amplitude * Math.sin(2 * Math.PI * freqHz * t));
    pcm.writeInt16LE(s, i * 2);
  }
  return pcm16ToPcmu(pcm);
}
