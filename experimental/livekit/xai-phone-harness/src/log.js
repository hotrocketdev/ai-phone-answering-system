// log.js — minimal logger. Writes to stdout with a millisecond
// timestamp, mirroring the Go METRIC log format the analyzer expects.

const start = Date.now();
function ts() {
  const ms = Date.now() - start;
  const s = Math.floor(ms / 1000);
  const m = s % 60;
  const totalS = Math.floor(s / 60);
  const h = Math.floor(totalS / 60);
  return `[${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}:${String(ms % 1000).padStart(3, '0')}]`;
}

export function log(...args) {
  console.log(`${ts()}`, ...args);
}
