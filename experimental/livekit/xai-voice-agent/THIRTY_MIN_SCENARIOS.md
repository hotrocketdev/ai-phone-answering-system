# 30-Minute Production-Style xAI Voice Agent Test Scenarios

Total target: **30 minutes** of mixed realistic restaurant phone-call conversation.
The user talks to Eve (xAI Voice Agent) for the full duration, with the script
below as a guide (not a strict order — feel free to improvise).

The harness (`xai-voice-agent`) on the VPS logs all `METRIC` events to
`/tmp/xai-voice-agent.log`. The metrics analyzer parses the log to produce the
post-test report.

---

## Goals per scenario
- **Stress**: 13 categories of call patterns in 30 min
- **Realism**: real British accent, real numbers, real names
- **Coverage**: function-calling, VAD, interruption, long pauses, edge cases

---

## SCENARIO TIMELINE (approx 30 min)

### T+0:00 – 2:00  — Normal booking flow
Goal: confirm baseline booking works.
1. "Hi, I'd like to book a table for 4 people at 7pm tomorrow please."
2. (Eve asks for details. Provide name "George" and phone "07917 715734".)
3. (Eve should call `availability.check` then `booking.create`.)
4. Confirm booking by reading it back: "Great, so 4 at 7pm tomorrow for George, 07917 715734. Thanks!"

### T+2:00 – 4:00  — Booking change
Goal: change a reservation mid-call.
5. "Actually, can we change that to 8pm instead? And make it 6 people."
6. (Eve should acknowledge the change and update.)
7. Provide new phone if asked: "Same number, 07917 715734."

### T+4:00 – 6:00  — UK phone number capture
Goal: confirm Eve understands UK phone formats.
8. "My mobile's 07700 900123. Can you text me the confirmation?"
9. (Eve should repeat back the digits: "zero seven seven zero zero, nine zero zero, one two three".)
10. Confirm OK.

### T+6:00 – 8:00  — Outdoor seating
Goal: weather-dependent seating question.
11. "Do you have outdoor seating? We're hoping to sit outside if the weather's nice."
12. (Eve should call `availability.check` with outdoor preference.)
13. Respond based on Eve's answer.

### T+8:00 – 10:00  — Manager callback
Goal: confirm escalation path works.
14. "Actually, can you have the manager call me back in 10 minutes? My name's Sarah, number 0117 555 0123."
15. (Eve should call `manager.escalate`.)
16. Confirm and say thanks.

### T+10:00 – 12:00  — Unknown opening hours
Goal: Eve should not invent; should escalate.
17. "What time do you close on Sundays?"
18. (Eve should NOT guess. Should offer to check or call back.)
19. If Eve gives a specific time, push back: "Are you sure? I thought you closed earlier."

### T+12:00 – 14:00  — Dietary requirement
Goal: dietary requests handled gracefully.
20. "One of us is coeliac — really strict gluten-free. Is the kitchen OK with that?"
21. (Eve should NOT promise specifics. Should offer to check with chef via callback.)
22. If Eve says "yes, absolutely", push back: "Are you certain? Cross-contamination is a big deal for us."

### T+14:00 – 16:00  — Interruption (overlap)
Goal: confirm VAD handles barge-in.
23. Start saying "I'd like to book a table for 2 people at—" then mid-sentence change: "—actually 3, sorry, at 8pm."
24. (Eve should hear the corrected version, not the partial.)
25. Confirm: "Yes, 3 people at 8pm."

### T+16:00 – 18:00  — Change of mind
Goal: confirm Eve can recover from cancellations.
26. "Wait — on second thought, can you just hold off for now? I'll call back later."
27. (Eve should acknowledge and offer a friendly closing.)
28. "OK, thanks anyway. Bye."

### T+18:00 – 20:00  — Repeat / clarify
Goal: confirm Eve handles "sorry, can you repeat that?".
29. Ask a question (e.g., "What's the cancellation policy?")
30. If Eve answers, immediately say: "Sorry, could you repeat that? I missed the bit about the fee."
31. (Eve should re-state the policy.)

### T+20:00 – 22:00  — Multi-intent
Goal: confirm Eve handles multiple questions in one turn.
32. "Do you have a kids menu? And is there parking nearby? Also, are you dog-friendly?"
33. (Eve should answer each in turn, NOT invent specifics.)
34. Push back if Eve gives a confident answer about parking: "Where exactly? Is it free?"

### T+22:00 – 25:00  — Long pause
Goal: confirm VAD doesn't cut the call after 5-10s silence.
35. "Hold on a sec, my other line's ringing…"
36. (Count to 10 slowly in your head. Stay silent.)
37. "Sorry about that. Yes, tomorrow at 7 is fine."

### T+25:00 – 27:00  — Background noise injection
Goal: confirm ASR handles noisy input.
38. (Cough or make a brief noise.) "Sorry about that."
39. "Can I also bring a birthday cake? Is that OK?"
40. (Eve should answer about the cake.)

### T+27:00 – 30:00  — Walk-in / edge cases
Goal: confirm Eve handles real-world quirks.
41. "I just walked in — any tables free right now for 2?"
42. (Eve should call `availability.check` for "now".)
43. "What about 11pm tonight for 2 people? I know it's late."
44. "Thanks, that's all I needed. Bye."

---

## POST-TEST CHECKLIST
- [ ] Harness stopped cleanly (`pkill -INT`)
- [ ] Last log line is `METRIC session_end ...`
- [ ] Log file pulled from VPS to local `logs/` dir
- [ ] No `xai-voice-agent.log` or `*.wav` committed
- [ ] Metrics analyzer run on the log
- [ ] Report produced: `report-30min-YYYY-MM-DD.md`
- [ ] VPS harness + ffmpegs killed
- [ ] Production gateway (pid 1796461) still running
- [ ] Production .env untouched
