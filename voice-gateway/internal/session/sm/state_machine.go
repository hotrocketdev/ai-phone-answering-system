// Package sm provides the conversation state machine for the AI receptionist.
// Enforces valid state transitions, state-scoped tool availability,
// anti-hallucination guardrails, and state-specific prompt injection.
package sm

import (
	"fmt"
	"strings"
)

// ─── States ──────────────────────────────────────────────────────────────

type State string

const (
	StateGreeting        State = "GREETING"
	StateFAQ             State = "FAQ_ANSWER"
	StateCollectBooking  State = "COLLECT_BOOKING_DETAILS"
	StateCheckAvail      State = "CHECK_AVAILABILITY"
	StateConfirm         State = "CONFIRM_BOOKING"
	StateModifyRes       State = "MODIFY_RESERVATION"
	StateCancelRes       State = "CANCEL_RESERVATION"
	StateHumanTransfer   State = "HUMAN_TRANSFER"
	StateUnavailable     State = "HANDLE_UNAVAILABLE"
	StateClosing         State = "CLOSING"
)

// ─── Booking Data ────────────────────────────────────────────────────────

type BookingData struct {
	PartySize int    `json:"partySize"`
	Date      string `json:"date"`
	Time      string `json:"time"`
	Name      string `json:"name"`
	Phone     string `json:"phone"`
	Email     string `json:"email,omitempty"`
	Notes     string `json:"notes,omitempty"`
	Reference string `json:"reference,omitempty"`
}

// MissingFields returns which booking fields still need to be collected.
func (b BookingData) MissingFields() []string {
	var missing []string
	if b.PartySize == 0 { missing = append(missing, "party_size") }
	if b.Date == ""     { missing = append(missing, "date") }
	if b.Time == ""     { missing = append(missing, "time") }
	if b.Name == ""     { missing = append(missing, "name") }
	if b.Phone == ""    { missing = append(missing, "phone") }
	return missing
}

// IsComplete returns true if all required booking fields are filled.
func (b BookingData) IsComplete() bool {
	return len(b.MissingFields()) == 0
}

// ─── Machine ─────────────────────────────────────────────────────────────

type Machine struct {
	current        State
	previous       State
	booking        BookingData
	faqReturnState State // where to return after FAQ answer
	restaurantName string
}

// New creates a new conversation state machine in the GREETING state.
func New(restaurantName string) *Machine {
	return &Machine{
		current:        StateGreeting,
		restaurantName: restaurantName,
	}
}

// Current returns the current conversation state.
func (m *Machine) Current() State { return m.current }

// Previous returns the previous state before the last transition.
func (m *Machine) Previous() State { return m.previous }

// Booking returns a copy of the accumulated booking data.
func (m *Machine) Booking() BookingData { return m.booking }

// SetBookingData updates a specific booking field.
func (m *Machine) SetBookingData(field string, value interface{}) {
	switch field {
	case "partySize":
		if v, ok := value.(int); ok { m.booking.PartySize = v }
		if v, ok := value.(float64); ok { m.booking.PartySize = int(v) }
	case "date":
		if v, ok := value.(string); ok { m.booking.Date = v }
	case "time":
		if v, ok := value.(string); ok { m.booking.Time = v }
	case "name":
		if v, ok := value.(string); ok { m.booking.Name = v }
	case "phone":
		if v, ok := value.(string); ok { m.booking.Phone = v }
	case "email":
		if v, ok := value.(string); ok { m.booking.Email = v }
	case "notes":
		if v, ok := value.(string); ok { m.booking.Notes = v }
	}
}

// ─── Transitions ─────────────────────────────────────────────────────────

// Transition attempts to change state. Returns error if transition is invalid.
func (m *Machine) Transition(newState State) error {
	if !isValidTransition(m.current, newState) {
		return fmt.Errorf("invalid state transition: %s → %s", m.current, newState)
	}
	m.previous = m.current
	m.current = newState

	// Side effects
	switch newState {
	case StateFAQ:
		m.faqReturnState = m.previous
	}
	return nil
}

// ReturnFromFAQ returns to the state before FAQ was entered.
func (m *Machine) ReturnFromFAQ() error {
	if m.current != StateFAQ {
		return fmt.Errorf("not in FAQ state")
	}
	// Direct state change — FAQ return doesn't go through normal transition validation
	m.previous = m.current
	m.current = m.faqReturnState
	return nil
}

// isValidTransition checks if a state transition is allowed.
func isValidTransition(from, to State) bool {
	transitions := map[State][]State{
		StateGreeting:       {StateFAQ, StateCollectBooking, StateHumanTransfer, StateClosing},
		StateFAQ:            {}, // return handled by ReturnFromFAQ
		StateCollectBooking: {StateCheckAvail, StateHumanTransfer, StateModifyRes, StateCancelRes, StateFAQ},
		StateCheckAvail:     {StateConfirm, StateUnavailable, StateCollectBooking},
		StateConfirm:        {StateClosing, StateCollectBooking},
		StateModifyRes:      {StateCheckAvail, StateConfirm, StateClosing},
		StateCancelRes:      {StateClosing, StateGreeting},
		StateHumanTransfer:  {StateClosing},
		StateUnavailable:    {StateCollectBooking, StateClosing},
		StateClosing:        {}, // terminal
	}
	for _, allowed := range transitions[from] {
		if to == allowed { return true }
	}
	return false
}

// ─── Tool Availability ───────────────────────────────────────────────────

// Tool defines a tool available to the AI.
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// AvailableTools returns tools scoped to the current state.
func (m *Machine) AvailableTools() []Tool {
	switch m.current {
	case StateGreeting:
		return nil
	case StateFAQ:
		return []Tool{toolGetFAQ}
	case StateCollectBooking:
		return nil // AI collects data without tools
	case StateCheckAvail:
		return []Tool{toolCheckAvailability}
	case StateConfirm:
		return []Tool{toolCreateBooking}
	case StateModifyRes:
		return []Tool{toolLookupReservation, toolModifyBooking, toolCancelBooking}
	case StateCancelRes:
		return []Tool{toolLookupReservation, toolCancelBooking}
	case StateHumanTransfer:
		return []Tool{toolTransferCall}
	case StateUnavailable:
		return []Tool{toolCheckAvailability}
	case StateClosing:
		return nil
	default:
		return nil
	}
}

// IsToolAllowed checks if a specific tool is allowed in the current state.
func (m *Machine) IsToolAllowed(toolName string) bool {
	for _, t := range m.AvailableTools() {
		if t.Name == toolName { return true }
	}
	return false
}

// ─── Tool Definitions ────────────────────────────────────────────────────

var toolCheckAvailability = Tool{
	Name:        "check_availability",
	Description: "Look up what times are available for a given date and party size. Use this before confirming any booking.",
	Parameters: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"date":      map[string]interface{}{"type": "string", "description": "Date in YYYY-MM-DD format"},
			"time":      map[string]interface{}{"type": "string", "description": "Preferred time in HH:MM 24-hour format"},
			"partySize": map[string]interface{}{"type": "integer", "description": "Number of people"},
		},
		"required": []string{"date", "time", "partySize"},
	},
}

var toolCreateBooking = Tool{
	Name:        "create_booking",
	Description: "Make a confirmed reservation. Only call this after the caller has explicitly said yes to the details you presented.",
	Parameters: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"date":      map[string]interface{}{"type": "string"},
			"time":      map[string]interface{}{"type": "string"},
			"partySize": map[string]interface{}{"type": "integer"},
			"name":      map[string]interface{}{"type": "string"},
			"phone":     map[string]interface{}{"type": "string"},
			"email":     map[string]interface{}{"type": "string"},
			"notes":     map[string]interface{}{"type": "string"},
		},
		"required": []string{"date", "time", "partySize", "name", "phone"},
	},
}

var toolCancelBooking = Tool{
	Name:        "cancel_booking",
	Description: "Cancel an existing reservation. Confirm with the caller before using this.",
	Parameters: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"reference": map[string]interface{}{"type": "string", "description": "Booking reference number"},
		},
		"required": []string{"reference"},
	},
}

var toolModifyBooking = Tool{
	Name:        "modify_booking",
	Description: "Change date, time, or party size on an existing booking",
	Parameters: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"reference": map[string]interface{}{"type": "string"},
			"date":      map[string]interface{}{"type": "string"},
			"time":      map[string]interface{}{"type": "string"},
			"partySize": map[string]interface{}{"type": "integer"},
		},
		"required": []string{"reference"},
	},
}

var toolLookupReservation = Tool{
	Name:        "lookup_reservation",
	Description: "Find an existing booking by name, phone number, or reference code",
	Parameters: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"reference": map[string]interface{}{"type": "string"},
			"name":      map[string]interface{}{"type": "string"},
			"phone":     map[string]interface{}{"type": "string"},
		},
	},
}

var toolTransferCall = Tool{
	Name:        "transfer_call",
	Description: "Put the caller through to a human staff member. Use immediately when asked.",
	Parameters: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"reason": map[string]interface{}{"type": "string", "description": "Brief reason for transfer"},
		},
	},
}

var toolGetFAQ = Tool{
	Name:        "get_faq",
	Description: "Find the answer to a question about the restaurant (hours, location, menu, parking, etc.)",
	Parameters: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string", "description": "What the caller is asking about"},
		},
		"required": []string{"query"},
	},
}

// ─── Anti-Hallucination Guardrails ───────────────────────────────────────

// ValidateResponse checks if an AI response would violate guardrails.
func (m *Machine) ValidateResponse(lastToolResult *ToolResult) error {
	if m.current == StateConfirm {
		if lastToolResult == nil || lastToolResult.Name != "create_booking" || !lastToolResult.Success {
			return fmt.Errorf("guardrail: cannot confirm booking without successful create_booking")
		}
	}
	return nil
}

// ToolResult captures the outcome of a tool execution for guardrail checks.
type ToolResult struct {
	Name    string
	Success bool
}

// ─── Prompt Building ─────────────────────────────────────────────────────

// BuildSystemPrompt returns the system prompt for the current state.
func (m *Machine) BuildSystemPrompt() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf(`You are Alex, the receptionist at %s, a Portuguese restaurant in Birmingham. You are warm, professional, and efficient.

HOW YOU TALK:
- Brief sentences. One or two at most.
- Natural conversational English, not scripted. Use contractions.
- Never say: "Certainly!", "Thank you for calling", "How may I assist you?", "I understand", "I'd be happy to"
- Never list options. Never say "you can also..." — just ask the next question.
- When pausing to check: "One moment" or "Let me check" — nothing longer.

HOW YOU HANDLE CALLERS:
- After greeting, listen for their request before asking anything.
- ASK ONE QUESTION AT A TIME. Never ask two things together.
- Wait for the answer, then ask the next thing if needed.
- Never repeat back everything the caller just said.
- If someone asks for a manager, transfer immediately. Don't ask why.

`, m.restaurantName))

	sb.WriteString(`BOOKING FLOW:
If caller wants a table, collect in order: date → time → party size → name → contact number.
One item per turn. Never say "I need to take some details" — just ask the first question.
When you have enough, check availability silently. Don't announce it.
Never say a booking is confirmed until the system confirms it.`)

	sb.WriteString(`

CRITICAL:
- Never confirm a booking until create_booking returns success.
- If asked for a manager: transfer immediately, no questions.
`)

	sb.WriteString(m.statePrompt())

	return sb.String()
}

func (m *Machine) statePrompt() string {
	switch m.current {
	case StateGreeting:
		return fmt.Sprintf(`

RIGHT NOW — GREETING:
Answer warmly: "%s, Alex speaking, how can I help?"
Then listen. Don't ask follow-up questions yet.`, m.restaurantName)

	case StateFAQ:
		return `

RIGHT NOW — QUESTION:
The caller asked about the restaurant. Use get_faq to find the answer.
Answer briefly, then ask if they need anything else or return to what they were doing before.`

	case StateCollectBooking:
		missing := m.booking.MissingFields()
		collected := []string{}
		if m.booking.PartySize > 0 { collected = append(collected, fmt.Sprintf("%d people", m.booking.PartySize)) }
		if m.booking.Date != "" { collected = append(collected, m.booking.Date) }
		if m.booking.Time != "" { collected = append(collected, m.booking.Time) }
		if m.booking.Name != "" { collected = append(collected, m.booking.Name) }

		collectedStr := "nothing yet"
		if len(collected) > 0 {
			collectedStr = strings.Join(collected, ", ")
		}

		return fmt.Sprintf(`

RIGHT NOW — BOOKING:
Collected: %s.
Still need: %s.
Ask for ONE missing detail at a time. Keep it short:
- "What date?"
- "What time?"
- "How many?"
- "What name?"
Don't announce what you're collecting. Just ask.`, collectedStr, strings.Join(missing, ", "))

	case StateCheckAvail:
		return fmt.Sprintf(`

RIGHT NOW — CHECKING:
Call check_availability with party_size=%d, date=%s, time=%s.
Say "One moment" — keep it brief. Report what you find naturally.`, m.booking.PartySize, m.booking.Date, m.booking.Time)

	case StateConfirm:
		return fmt.Sprintf(`

RIGHT NOW — CONFIRMING:
You have: %d people on %s at %s, under %s.
Say: "So that's %d for %s at %s under %s — all good?"
Wait for yes. Then call create_booking.
Never say "booked" until the tool confirms.`, m.booking.PartySize, m.booking.Date, m.booking.Time, m.booking.Name,
			m.booking.PartySize, m.booking.Date, m.booking.Time, m.booking.Name)

	case StateModifyRes:
		return `

RIGHT NOW — MODIFY:
Ask for the booking reference or name. Look it up, confirm, then ask what to change.`

	case StateCancelRes:
		return `

RIGHT NOW — CANCEL:
Ask for the booking reference. Look it up, confirm details. Ask if they're sure before cancelling.`

	case StateHumanTransfer:
		return `

RIGHT NOW — TRANSFER:
Call transfer_call. Say "Let me put you through."`

	case StateUnavailable:
		return `

RIGHT NOW — FULLY BOOKED:
Offer nearest alternatives. If nothing works, offer to take their number for cancellation waitlist.`

	case StateClosing:
		return `

RIGHT NOW — ENDING:
Keep it brief. Booking made: "All set for [date] at [time]. We'll see you then!"
Otherwise: "Thanks for calling, have a great evening."`


	default:
		return ""
	}
}
