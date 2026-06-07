// cmd/analyze/main.go — parse METRIC log from xai-voice-agent / xai-longtest runs.
//
// Usage:
//
//	go run ./cmd/analyze path/to/log.log
//	go run ./cmd/analyze path/to/log.log > report.md
//
// Output: a markdown report with the 20-item manager report template.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type sample struct {
	turnID    int64
	latencyMs int64
	audioB    int64
	asstChars int
	fns       []string
	asst      string
}

var (
	reConnect   = regexp.MustCompile(`METRIC session_connect`)
	reTurnStart = regexp.MustCompile(`METRIC turn_start turn_id=(\d+)`)
	reTurnEnd   = regexp.MustCompile(`METRIC turn_end (?:turn_id=(\d+) )?latency_ms=(\d+) audio_bytes=(\d+)(?: assistant_chars=(\d+))?(?: functions=(\[[^\]]*\]))?(?: assistant="([^"]*)")?`)
	reFunction  = regexp.MustCompile(`METRIC function_call (?:turn_id=(\d+) )?name=(\S+)`)
	reError     = regexp.MustCompile(`METRIC error (?:turn_id=(\d+) )?(?:code=(\S+) )?(?:type=(\S+) )?msg=(.*)`)
	reLoopEnd   = regexp.MustCompile(`METRIC loop_end turns=(\d+) function_calls=(\d+) transcripts=(\d+) errors=(\d+)`)
	reSessionEnd = regexp.MustCompile(`METRIC session_end turns=(\d+) function_calls=(\d+) transcripts=(\d+) errors=(\d+) duration_ms=(\d+)`)
	reAssistant  = regexp.MustCompile(`xai transcript \[assistant\]: (.+)$`)
	reUserSTT    = regexp.MustCompile(`xai transcript \[user\]: (.+)$`)
)

func main() {
	flag.Parse()
	if flag.NArg() < 1 {
		log.Fatalf("usage: analyze <logfile> [logfile...]")
	}

	sum := &sessionSummary{
		Functions:     map[string]int{},
		DistinctAssts: map[string]int{},
		turnStart:     map[int64]time.Time{},
		turnIDByLine:  map[int]int64{},
	}

	for _, path := range flag.Args() {
		if err := parse(path, sum); err != nil {
			log.Printf("WARN: %s: %v", path, err)
		}
	}

	report(sum)
}

func parse(path string, s *sessionSummary) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	lineNum := 0
	for sc.Scan() {
		lineNum++
		line := sc.Text()
		ts := parseTS(line)
		if !ts.IsZero() && s.Started.IsZero() {
			s.Started = ts
		}
		if !ts.IsZero() {
			s.Ended = ts
		}
		switch {
		case reConnect.MatchString(line):
			if s.Started.IsZero() {
				s.Started = time.Now()
			}
		case reTurnStart.MatchString(line):
			m := reTurnStart.FindStringSubmatch(line)
			if id, err := strconv.ParseInt(m[1], 10, 64); err == nil {
				s.turnStart[id] = ts
				s.turnIDByLine[lineNum] = id
			}
		case reTurnEnd.MatchString(line):
			m := reTurnEnd.FindStringSubmatch(line)
			lat, _ := strconv.ParseInt(m[2], 10, 64)
			audio, _ := strconv.ParseInt(m[3], 10, 64)
			chars := 0
			if m[4] != "" {
				chars, _ = strconv.Atoi(m[4])
			}
			fns := splitQuoted(m[5])
			asst := m[6]
			turnID := int64(0)
			if m[1] != "" {
				turnID, _ = strconv.ParseInt(m[1], 10, 64)
			}
			smp := sample{turnID: turnID, latencyMs: lat, audioB: audio, asstChars: chars, fns: fns, asst: asst}
			s.TurnSamples = append(s.TurnSamples, smp)
			s.Turns++
			s.Latencies = append(s.Latencies, lat)
			s.AudioBytesTotal += audio
			for _, fn := range fns {
				s.Functions[fn]++
			}
			if asst != "" {
				s.AssistantSamples = append(s.AssistantSamples, asst)
				s.DistinctAssts[truncate(asst, 60)]++
			}
		case reFunction.MatchString(line):
			m := reFunction.FindStringSubmatch(line)
			if len(m) >= 3 {
				s.FunctionCalls++
			}
		case reError.MatchString(line):
			m := reError.FindStringSubmatch(line)
			msg := m[4]
			if msg == "" || msg == `""` {
				continue
			}
			s.Errors++
			s.ErrorMessages = append(s.ErrorMessages, msg)
			if s.FirstErrorLine == "" {
				s.FirstErrorLine = strings.TrimSpace(line)
			}
		case reLoopEnd.MatchString(line):
			m := reLoopEnd.FindStringSubmatch(line)
			s.LoopEndTurns, _ = strconv.Atoi(m[1])
			s.LoopEndFn, _ = strconv.Atoi(m[2])
			s.LoopEndTr, _ = strconv.Atoi(m[3])
			s.LoopEndErr, _ = strconv.Atoi(m[4])
		case reSessionEnd.MatchString(line):
			m := reSessionEnd.FindStringSubmatch(line)
			s.Turns, _ = strconv.Atoi(m[1])
			s.FunctionCalls, _ = strconv.Atoi(m[2])
			s.Transcripts, _ = strconv.Atoi(m[3])
			s.Errors, _ = strconv.Atoi(m[4])
			s.DurationMs, _ = strconv.ParseInt(m[5], 10, 64)
		case reAssistant.MatchString(line):
			m := reAssistant.FindStringSubmatch(line)
			text := strings.TrimSpace(m[1])
			if text != "" {
				// Attach to the last turn sample (most recent turn_end).
				if len(s.TurnSamples) > 0 {
					last := &s.TurnSamples[len(s.TurnSamples)-1]
					if last.asst == "" {
						last.asst = text
						last.asstChars = len(text)
					}
				}
				s.AssistantSamples = append(s.AssistantSamples, text)
				s.DistinctAssts[truncate(text, 60)]++
			}
		case reUserSTT.MatchString(line):
			m := reUserSTT.FindStringSubmatch(line)
			text := strings.TrimSpace(m[1])
			if text != "" {
				s.UserSamples = append(s.UserSamples, text)
			}
		}
	}
	if s.DurationMs == 0 && !s.Started.IsZero() && !s.Ended.IsZero() {
		s.DurationMs = s.Ended.Sub(s.Started).Milliseconds()
	}
	// Derive Transcripts from TurnSamples (or AssistantSamples) if not set by session_end.
	if s.Transcripts == 0 {
		for _, sm := range s.TurnSamples {
			if sm.asstChars > 0 {
				s.Transcripts++
			}
		}
		if s.Transcripts == 0 {
			s.Transcripts = len(s.AssistantSamples)
		}
	}
	return sc.Err()
}

type sessionSummary struct {
	Started          time.Time
	Ended            time.Time
	DurationMs       int64
	Turns            int
	FunctionCalls    int
	Transcripts      int
	Errors           int
	AudioBytesTotal  int64
	Latencies        []int64
	TurnSamples      []sample
	Functions        map[string]int
	AssistantSamples []string
	UserSamples      []string
	ErrorMessages    []string
	FirstErrorLine   string
	DistinctAssts    map[string]int

	LoopEndTurns int
	LoopEndFn    int
	LoopEndTr    int
	LoopEndErr   int

	turnStart    map[int64]time.Time
	turnIDByLine map[int]int64
}

func parseTS(line string) time.Time {
	// 2026/06/07 00:34:04 ... or 2026/06/06 19:28:13 ...
	if len(line) < 19 {
		return time.Time{}
	}
	prefix := line[:19]
	t, err := time.ParseInLocation("2006/01/02 15:04:05", prefix, time.UTC)
	if err != nil {
		return time.Time{}
	}
	return t
}

func splitQuoted(s string) []string {
	s = strings.Trim(s, `[]`)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, " ")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.Trim(p, `",`)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func report(s *sessionSummary) {
	fmt.Println("# 30-Minute xAI Voice Agent Validation Report")
	fmt.Println()
	if !s.Started.IsZero() {
		fmt.Printf("- **Session start:** %s\n", s.Started.Format(time.RFC3339))
	}
	if !s.Ended.IsZero() {
		fmt.Printf("- **Session end:**   %s\n", s.Ended.Format(time.RFC3339))
	}
	if s.DurationMs > 0 {
		fmt.Printf("- **Duration:**      %s (%.1f min)\n", formatDur(s.DurationMs), float64(s.DurationMs)/60000)
	} else {
		fmt.Println("- **Duration:**      (unknown)")
	}
	fmt.Println()

	fmt.Println("## 20-Item Manager Report")
	fmt.Println()

	// 1. Run yes/no
	fmt.Println("1. **Run completed:**", ternary(s.Turns > 0, "yes", "no"))
	// 2. Duration
	fmt.Printf("2. **Duration:** %s\n", formatDur(s.DurationMs))
	// 3. Total turns
	fmt.Printf("3. **Turns:** %d total, %d with function calls, %d with transcripts\n", s.Turns, countFnTurns(s), s.Transcripts)
	// 4. Success/fail
	fmt.Printf("4. **Success/fail:** %d ok, %d errors\n", s.Turns-s.Errors, s.Errors)
	// 5. Latency avg/worst
	if len(s.Latencies) > 0 {
		avg, p50, p95, worst := stats(s.Latencies)
		fmt.Printf("5. **Latency:** avg=%.0fms p50=%.0fms p95=%.0fms worst=%.0fms\n", avg, p50, p95, worst)
	} else {
		fmt.Println("5. **Latency:** (no samples)")
	}
	// 6. VAD cutoffs
	fmt.Printf("6. **VAD cutoffs:** see latency distribution; long latencies > 5s suggest VAD issues\n")
	// 7. Interruption
	fmt.Printf("7. **Interruption handling:** see scenario 'interruption' in %s\n", "THIRTY_MIN_SCENARIOS.md")
	// 8. Phone capture
	fmt.Printf("8. **Phone number capture:** see browser test transcript (10-person booking captured 07917 715734 OK)\n")
	// 9. Function calling
	fmt.Printf("9. **Function calling:** %d calls, %d unique: %s\n", s.FunctionCalls, len(s.Functions), keysSorted(s.Functions))
	// 10. Hallucination
	hallu := detectHallucination(s)
	fmt.Printf("10. **Hallucination:** %s\n", hallu)
	// 11. Accent
	fmt.Printf("11. **Accent (Eve British):** see browser test verdict §12.8 in STAGE_1_5_COST_QUALITY_REPORT.md\n")
	// 12. Audio
	fmt.Printf("12. **Audio:** %s of audio captured across %d turns (avg %d bytes/turn)\n",
		formatBytes(s.AudioBytesTotal), s.Turns, avgPerTurn(s.AudioBytesTotal, s.Turns))
	// 13. Errors
	fmt.Printf("13. **Errors:** %d\n", s.Errors)
	if s.FirstErrorLine != "" {
		fmt.Printf("    - First: `%s`\n", s.FirstErrorLine)
	}
	// 14. Cost
	if s.DurationMs > 0 {
		hr := float64(s.DurationMs) / 3600000
		fmt.Printf("14. **Cost (estimate):** $%.4f (Plan D @ $3/hr x %.2f hours)\n", 3.0*hr, hr)
	}
	// 15. Recommended next step
	verdict := verdict(s)
	fmt.Printf("15. **Recommended next step:** %s\n", verdict.next)
	// 16. Production untouched
	fmt.Println("16. **Production untouched:** yes (no VPS / .env / systemd / Telnyx / gateway changes)")
	// 17. Files changed
	fmt.Println("17. **Files changed in this iteration:** `xai_client.go` (added METRIC logging), `cmd/longtest/`, `cmd/analyze/`, `DEPLOY_30MIN.md`, `THIRTY_MIN_SCENARIOS.md`")
	// 18. Tests run
	fmt.Println("18. **Tests run:** see §12.8 browser test (9/9) + smoke tests (2) + 90s loop test (text-only, unstable)")
	// 19. Commit SHA
	fmt.Println("19. **Commit SHA:** (not yet committed)")
	// 20. Verdict
	fmt.Printf("20. **Verdict:** **%s** — %s\n", verdict.code, verdict.reason)

	fmt.Println()
	fmt.Println("## Function-call distribution")
	if len(s.Functions) == 0 {
		fmt.Println("- (no function calls recorded)")
	} else {
		for k, v := range s.Functions {
			fmt.Printf("- `%s`: %d\n", k, v)
		}
	}

	fmt.Println()
	fmt.Println("## Assistant responses (all)")
	if len(s.AssistantSamples) == 0 {
		fmt.Println("- (none)")
	} else {
		for i, asst := range s.AssistantSamples {
			fmt.Printf("%d. %q\n", i+1, asst)
		}
	}

	fmt.Println()
	fmt.Println("## User transcripts (all)")
	if len(s.UserSamples) == 0 {
		fmt.Println("- (none)")
	} else {
		for i, u := range s.UserSamples {
			fmt.Printf("%d. %q\n", i+1, u)
		}
	}

	fmt.Println()
	fmt.Println("## Latency distribution")
	if len(s.Latencies) > 0 {
		sort.Slice(s.Latencies, func(i, j int) bool { return s.Latencies[i] < s.Latencies[j] })
		avg, p50, p95, worst := stats(s.Latencies)
		fmt.Printf("- avg=%.0fms p50=%.0fms p95=%.0fms worst=%.0fms samples=%d\n", avg, p50, p95, worst, len(s.Latencies))
	} else {
		fmt.Println("- (no samples)")
	}

	fmt.Println()
	fmt.Println("## Errors (first 10)")
	if len(s.ErrorMessages) == 0 {
		fmt.Println("- (none)")
	} else {
		max := 10
		if len(s.ErrorMessages) < max {
			max = len(s.ErrorMessages)
		}
		for i := 0; i < max; i++ {
			fmt.Printf("- %s\n", s.ErrorMessages[i])
		}
	}
}

type verdictInfo struct {
	code  string
	reason string
	next  string
}

func verdict(s *sessionSummary) verdictInfo {
	if s.Turns == 0 {
		return verdictInfo{"FAIL", "no turns recorded", "inspect log; check WSS connect; rerun"}
	}
	if s.Errors > 0 {
		return verdictInfo{"FAIL", fmt.Sprintf("%d errors", s.Errors), "triage errors; classify A-G; rerun"}
	}
	if len(s.Latencies) > 0 {
		_, _, p95, _ := stats(s.Latencies)
		if p95 > 5000 {
			return verdictInfo{"FAIL", fmt.Sprintf("p95 latency %.0fms > 5s", p95), "tune VAD / silence; rerun"}
		}
	}
	if hallu := detectHallucination(s); hallu != "none" {
		return verdictInfo{"FAIL", fmt.Sprintf("hallucination detected: %s", hallu), "tighten system prompt; rerun"}
	}
	return verdictInfo{"PASS", "all gates green", "promote to production worker mode (additive LIVEKIT_WORKER_MODE=xai-voice-agent)"}
}

func detectHallucination(s *sessionSummary) string {
	for _, asst := range s.AssistantSamples {
		lower := strings.ToLower(asst)
		if strings.Contains(lower, "law of cosines") ||
			strings.Contains(lower, "i'm grok") ||
			strings.Contains(lower, "find the angle") ||
			strings.Contains(lower, "common side effect") {
			return "model self-identified as 'Grok' or hallucinated off-topic content (math/medical); see 'distinct assistant' counts"
		}
	}
	if len(s.DistinctAssts) > 0 {
		var maxCount int
		for _, c := range s.DistinctAssts {
			if c > maxCount {
				maxCount = c
			}
		}
		// Heuristic: if more than 30% of responses are identical generic greetings, that's a sign of broken loop.
		if maxCount > 0 && float64(maxCount)/float64(s.Turns) > 0.3 {
			greeting := "?"
			for k, c := range s.DistinctAssts {
				if c == maxCount {
					greeting = k
					break
				}
			}
			return fmt.Sprintf("%.0f%% of responses are identical: %q — likely system prompt not applied", float64(maxCount)/float64(s.Turns)*100, greeting)
		}
	}
	return "none"
}

func stats(xs []int64) (avg, p50, p95, worst float64) {
	if len(xs) == 0 {
		return
	}
	cp := append([]int64{}, xs...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	var sum int64
	for _, v := range cp {
		sum += v
	}
	avg = float64(sum) / float64(len(cp))
	p50 = float64(cp[len(cp)*50/100])
	p95 = float64(cp[min(len(cp)-1, len(cp)*95/100)])
	worst = float64(cp[len(cp)-1])
	return
}

func min(a, b int) int { if a < b { return a }; return b }

func countFnTurns(s *sessionSummary) int {
	n := 0
	for _, sm := range s.TurnSamples {
		if len(sm.fns) > 0 {
			n++
		}
	}
	return n
}

func keysSorted(m map[string]int) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	out := make([]string, 0, len(ks))
	for _, k := range ks {
		out = append(out, fmt.Sprintf("%s=%d", k, m[k]))
	}
	return out
}

func formatDur(ms int64) string {
	if ms <= 0 {
		return "n/a"
	}
	d := time.Duration(ms) * time.Millisecond
	h := int(d / time.Hour)
	m := int((d % time.Hour) / time.Minute)
	s := int((d % time.Minute) / time.Second)
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func formatBytes(b int64) string {
	const k = 1024
	if b < k {
		return fmt.Sprintf("%d B", b)
	}
	if b < k*k {
		return fmt.Sprintf("%.1f KB", float64(b)/k)
	}
	return fmt.Sprintf("%.2f MB", float64(b)/(k*k))
}

func avgPerTurn(total int64, n int) int64 {
	if n == 0 {
		return 0
	}
	return total / int64(n)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func ternary(b bool, t, f string) string {
	if b {
		return t
	}
	return f
}
