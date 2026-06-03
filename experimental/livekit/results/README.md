# LiveKit Spike Results

**Status:** Empty — results will be documented here after the spike runs.

---

## Planned Measurements

- **Audio quality:** Subjective comparison of Cartesia HD via LiveKit vs PCMU phone path
- **Latency:** Time from Cartesia first byte to browser playback (target: < 3s for greeting)
- **Frequency response:** Measured bandwidth of Opus audio vs PCMU
- **Stability:** No errors during spike run

---

## Planned Artifacts

- Audio capture of LiveKit HD spike (browser-side)
- Audio capture of PCMU phone path (for comparison)
- Latency log
- Subjective notes

---

## Planned Outcome

Based on the spike results, the recommendation will be one of:

1. **Proceed to Phase 2** (two-way conversation via LiveKit)
2. **Stop** (PSTN ceiling is acceptable, no further HD work needed)
3. **Evaluate alternatives** (e.g., different WebRTC platform, different TTS provider)

The spike itself does NOT commit to any production changes. Production integration is a separate, gated decision.
