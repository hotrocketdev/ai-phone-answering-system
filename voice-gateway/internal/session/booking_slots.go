package session

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/voxlane/voice-gateway/internal/session/sm"
)

type bookingSlotUpdate struct {
	Date      string
	Time      string
	PartySize int
	Name      string
	Phone     string
}

func (u bookingSlotUpdate) hasAny() bool {
	return u.Date != "" || u.Time != "" || u.PartySize > 0 || u.Name != "" || u.Phone != ""
}

func parseBookingSlots(text string, current sm.BookingData) bookingSlotUpdate {
	lower := strings.ToLower(strings.TrimSpace(text))
	update := bookingSlotUpdate{}
	if lower == "" {
		return update
	}

	update.Phone = parsePhone(text)
	update.Date = parseDate(lower)
	update.Time = parseTime(lower, current)
	update.PartySize = parsePartySize(lower)
	update.Name = parseName(text, lower, current, update)

	return update
}

func mergeBookingSlots(current sm.BookingData, update bookingSlotUpdate, allowOverwrite bool) sm.BookingData {
	if update.Date != "" && (allowOverwrite || current.Date == "") {
		current.Date = update.Date
	}
	if update.Time != "" && (allowOverwrite || current.Time == "") {
		current.Time = update.Time
	}
	if update.PartySize > 0 && (allowOverwrite || current.PartySize == 0) {
		current.PartySize = update.PartySize
	}
	if update.Name != "" && (allowOverwrite || current.Name == "") {
		current.Name = update.Name
	}
	if update.Phone != "" && (allowOverwrite || current.Phone == "") {
		current.Phone = update.Phone
	}
	return current
}

func bookingIntent(text string) bool {
	lower := strings.ToLower(text)
	for _, token := range []string{"book", "booking", "reserve", "reservation", "table"} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func correctionIntent(text string) bool {
	lower := strings.ToLower(text)
	for _, token := range []string{"actually", "make that", "sorry", "instead", "change it"} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func firstMissingBookingField(b sm.BookingData) string {
	switch {
	case b.Date == "":
		return "date"
	case b.Time == "":
		return "time"
	case b.PartySize == 0:
		return "guest_count"
	case b.Name == "":
		return "name"
	case b.Phone == "":
		return "phone"
	default:
		return ""
	}
}

func nextBookingQuestion(field string) string {
	switch field {
	case "date":
		return "What date would you like to book for?"
	case "time":
		return "What time would you like?"
	case "guest_count":
		return "How many guests?"
	case "name":
		return "Can I take your name please?"
	case "phone":
		return "And a contact number?"
	default:
		return "One moment, I'll check that."
	}
}

func bookingSummary(b sm.BookingData) string {
	return fmt.Sprintf("date=%s time=%s guest_count=%d name=%s phone_present=%t missing=%s",
		blankIfEmpty(b.Date), blankIfEmpty(b.Time), b.PartySize, blankIfEmpty(b.Name), b.Phone != "", firstMissingBookingField(b))
}

func blankIfEmpty(v string) string {
	if strings.TrimSpace(v) == "" {
		return "-"
	}
	return v
}

func parseDate(lower string) string {
	if strings.Contains(lower, "tomorrow") {
		return "tomorrow"
	}
	if strings.Contains(lower, "today") {
		return "today"
	}
	weekdays := []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}
	for _, day := range weekdays {
		if regexp.MustCompile(`\b` + day + `\b`).MatchString(lower) {
			return day
		}
	}
	return ""
}

func parseTime(lower string, current sm.BookingData) string {
	re := regexp.MustCompile(`\b([01]?\d|2[0-3])(?::([0-5]\d))?\s*(a\.?m\.?|p\.?m\.?)\b`)
	if m := re.FindStringSubmatch(lower); len(m) > 0 {
		hour, _ := strconv.Atoi(m[1])
		minute := 0
		if m[2] != "" {
			minute, _ = strconv.Atoi(m[2])
		}
		if strings.HasPrefix(strings.ReplaceAll(m[3], ".", ""), "p") && hour < 12 {
			hour += 12
		}
		if strings.HasPrefix(strings.ReplaceAll(m[3], ".", ""), "a") && hour == 12 {
			hour = 0
		}
		return fmt.Sprintf("%02d:%02d", hour, minute)
	}

	re24 := regexp.MustCompile(`\b([01]?\d|2[0-3]):([0-5]\d)\b`)
	if m := re24.FindStringSubmatch(lower); len(m) > 0 {
		hour, _ := strconv.Atoi(m[1])
		minute, _ := strconv.Atoi(m[2])
		return fmt.Sprintf("%02d:%02d", hour, minute)
	}

	if current.Time == "" && current.Date != "" {
		reBare := regexp.MustCompile(`\b([1-9]|1[0-2])\b`)
		if m := reBare.FindStringSubmatch(lower); len(m) > 0 && !looksLikePartySize(lower) {
			hour, _ := strconv.Atoi(m[1])
			if hour >= 1 && hour <= 11 {
				hour += 12
			}
			return fmt.Sprintf("%02d:00", hour)
		}
	}

	return ""
}

func parsePartySize(lower string) int {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\b(?:for|table for)\s+(\d{1,2})\b`),
		regexp.MustCompile(`\b(\d{1,2})\s+(?:people|guests|persons)\b`),
	}
	for _, re := range patterns {
		if m := re.FindStringSubmatch(lower); len(m) > 0 {
			n, _ := strconv.Atoi(m[1])
			if n > 0 {
				return n
			}
		}
	}

	numberWords := map[string]int{
		"one": 1, "two": 2, "three": 3, "four": 4, "five": 5,
		"six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10,
		"eleven": 11, "twelve": 12,
	}
	for word, n := range numberWords {
		if regexp.MustCompile(`\bfor\s+`+word+`\b`).MatchString(lower) ||
			regexp.MustCompile(`\b`+word+`\s+(people|guests|persons)\b`).MatchString(lower) ||
			strings.Trim(lower, " .!?") == word {
			return n
		}
	}
	return 0
}

func parseName(original, lower string, current sm.BookingData, update bookingSlotUpdate) string {
	if current.Name != "" || update.Phone != "" || update.PartySize > 0 || update.Time != "" || update.Date != "" {
		return ""
	}
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bmy name is\s+([a-z][a-z'\-]+)\b`),
		regexp.MustCompile(`(?i)\bit'?s\s+([a-z][a-z'\-]+)\b`),
	}
	for _, re := range patterns {
		if m := re.FindStringSubmatch(original); len(m) > 0 {
			return titleName(m[1])
		}
	}
	trimmed := strings.Trim(lower, " .!?")
	if current.Date != "" && current.Time != "" && current.PartySize > 0 && regexp.MustCompile(`^[a-z][a-z'\-]+$`).MatchString(trimmed) {
		return titleName(trimmed)
	}
	return ""
}

func parsePhone(original string) string {
	digits := regexp.MustCompile(`\D`).ReplaceAllString(original, "")
	if len(digits) >= 10 && len(digits) <= 13 {
		return digits
	}
	return ""
}

func looksLikePartySize(lower string) bool {
	return strings.Contains(lower, "people") || strings.Contains(lower, "guest") || strings.Contains(lower, "for ")
}

func titleName(name string) string {
	if name == "" {
		return ""
	}
	name = strings.ToLower(name)
	return strings.ToUpper(name[:1]) + name[1:]
}
