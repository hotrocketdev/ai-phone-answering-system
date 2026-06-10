// Time-based silence detection on inbound Opus frames.
//
// The LiveKit browser client (and LiveKit server SDK) emit Opus
// frames at a consistent cadence (20ms at 48kHz) when the peer is
// sending audio. Silence is the absence of frames, not zero-energy
// frames — LiveKit DTX may emit tiny 0-byte frames, so we
// also gate on Duration.
//
// VAD state machine:
//   idle       -> first frame -> speaking,  snap start time
//   speaking   -> frame        -> speaking,  extend
//   speaking   -> no frame for silenceTimeout -> emit utterance, idle
//
// The class owns the per-utterance frame buffer; the worker pulls
// it on emit.
package main

import "time"

const (
	// defaultSilenceTimeout is how long we wait after the last
	// inbound Opus frame before ending the current utterance.
	// 500ms covers natural pauses in speech without feeling laggy.
	defaultSilenceTimeout = 500 * time.Millisecond

	// defaultMaxUtteranceDuration is a hard cap to avoid unbounded
	// buffering if the user speaks for a long time.
	defaultMaxUtteranceDuration = 30 * time.Second
)

// vad is a per-track silence detector. Not safe for concurrent use;
// one instance is owned by the inbound reader goroutine.
type vad struct {
	silenceTimeout     time.Duration
	maxUtteranceDur    time.Duration
	speaking           bool
	startTime          time.Time
	lastFrameTime      time.Time
	frames             [][]byte // raw Opus frame bytes
}

// newVAD returns a VAD with default thresholds.
func newVAD() *vad {
	return &vad{
		silenceTimeout:  defaultSilenceTimeout,
		maxUtteranceDur: defaultMaxUtteranceDuration,
	}
}

// push records one inbound Opus frame and updates state. Returns
// true if the frame ended an utterance (caller should then call
// takeUtterance to retrieve and clear the buffer).
func (v *vad) push(frame []byte) (ended bool) {
	now := time.Now()
	if !v.speaking {
		v.speaking = true
		v.startTime = now
		v.lastFrameTime = now
		v.frames = v.frames[:0]
	} else {
		v.lastFrameTime = now
	}
	v.frames = append(v.frames, append([]byte(nil), frame...))

	// Force-end on max duration.
	if v.speaking && now.Sub(v.startTime) >= v.maxUtteranceDur {
		return true
	}
	return false
}

// tick checks whether the silence timeout has elapsed since the
// last frame. The worker calls this on a timer (~10-20Hz) when no
// new frame has arrived. Returns true if an utterance ended.
func (v *vad) tick() (ended bool) {
	if !v.speaking {
		return false
	}
	if time.Since(v.lastFrameTime) >= v.silenceTimeout {
		return true
	}
	return false
}

// takeUtterance returns the buffered Opus frames and resets state.
// Returns nil if no utterance is active.
func (v *vad) takeUtterance() [][]byte {
	if !v.speaking {
		return nil
	}
	out := v.frames
	v.speaking = false
	v.frames = nil
	return out
}
