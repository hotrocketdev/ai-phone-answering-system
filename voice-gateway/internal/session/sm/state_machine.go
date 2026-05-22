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
	Description: "Check table availability for a given date, time, and party size",
	Parameters: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"date":      map[string]interface{}{"type": "string", "description": "Date in YYYY-MM-DD format"},
			"time":      map[string]interface{}{"type": "string", "description": "Time in HH:MM 24-hour format"},
			"partySize": map[string]interface{}{"type": "integer", "description": "Number of guests"},
		},
		"required": []string{"date", "time", "partySize"},
	},
}

var toolCreateBooking = Tool{
	Name:        "create_booking",
	Description: "Create a confirmed table booking. Only call after caller explicitly confirms.",
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
	Description: "Cancel an existing reservation by reference number",
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
	Description: "Modify an existing reservation",
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
	Description: "Find a reservation by name, phone, or reference",
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
	Description: "Transfer the caller to a human staff member",
	Parameters: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"reason": map[string]interface{}{"type": "string", "description": "Brief reason for transfer"},
		},
	},
}

var toolGetFAQ = Tool{
	Name:        "get_faq",
	Description: "Look up answer to a frequently asked question",
	Parameters: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string", "description": "The question to look up"},
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

	// Base identity
	sb.WriteString(fmt.Sprintf("You are the friendly AI receptionist for %s, a premium restaurant.\n", m.restaurantName))
	sb.WriteString("You have answered the phone. Be warm, natural, and conversational like a real person.\n\n")

	// Guardrails (always included)
	sb.WriteString("CRITICAL RULES:\n")
	sb.WriteString("- Never confirm a booking until create_booking returns success.\n")
	sb.WriteString("- Never transition state yourself — only tool results change state.\n")
	sb.WriteString("- If caller asks for a human, transfer immediately — do not negotiate.\n")
	sb.WriteString("- Do not apologize excessively. Be warm but concise.\n")
	sb.WriteString("- Keep responses brief and natural — under 2 sentences when possible.\n\n")

	// State-specific instructions
	sb.WriteString(m.statePrompt())

	return sb.String()
}

func (m *Machine) statePrompt() string {
	switch m.current {
	case StateGreeting:
		return `CURRENT STATE: GREETING
You have just answered the phone. Greet the caller warmly using the restaurant name.
Detect their intent: booking, modification, cancellation, FAQ, or speak-to-human.
Do not ask for details until you understand what they want.`

	case StateFAQ:
		return `CURRENT STATE: FAQ
The caller asked a question. Call get_faq with their query to find the answer.
Respond naturally with the information. Then ask if they need anything else.
Return to their previous request after answering.`

	case StateCollectBooking:
		missing := m.booking.MissingFields()
		return fmt.Sprintf(`CURRENT STATE: COLLECTING BOOKING DETAILS
You are gathering booking information conversationally.
Still needed: %s.
Already collected: party_size=%d, date=%s, time=%s, name=%s, phone=%s.
Ask for ONE missing field at a time. Be natural — don't list fields.
Do not check availability until all fields are collected.`,
			strings.Join(missing, ", "),
			m.booking.PartySize, m.booking.Date, m.booking.Time,
			m.booking.Name, m.booking.Phone)

	case StateCheckAvail:
		return fmt.Sprintf(`CURRENT STATE: CHECKING AVAILABILITY
Call check_availability with: party_size=%d, date=%s, time=%s.
Report the result naturally to the caller.`,
			m.booking.PartySize, m.booking.Date, m.booking.Time)

	case StateConfirm:
		return fmt.Sprintf(`CURRENT STATE: CONFIRMING BOOKING
Booking details ready: %d people, %s at %s. Name: %s, Phone: %s.
Ask the caller to confirm these details are correct.
ONLY call create_booking when the caller explicitly says yes/confirm.
Do NOT say "it's booked" until create_booking returns success.`,
			m.booking.PartySize, m.booking.Date, m.booking.Time,
			m.booking.Name, m.booking.Phone)

	case StateModifyRes:
		return `CURRENT STATE: MODIFYING RESERVATION
The caller wants to modify an existing reservation.
Ask for their booking reference or name, then look it up with lookup_reservation.
After finding it, collect the new details and use modify_booking.`

	case StateCancelRes:
		return `CURRENT STATE: CANCELLING RESERVATION
The caller wants to cancel. Ask for their booking reference or name.
Look it up with lookup_reservation, then confirm before calling cancel_booking.
Be empathetic but professional.`

	case StateHumanTransfer:
		return `CURRENT STATE: TRANSFERRING TO HUMAN
Call transfer_call to connect the caller to staff.
Tell the caller you're transferring them now.`

	case StateUnavailable:
		return `CURRENT STATE: NO AVAILABILITY
The requested time is not available. Present alternatives naturally.
Offer to check other times using check_availability.
Be helpful — suggest nearby times or taking a callback number.`

	case StateClosing:
		return `CURRENT STATE: CLOSING
The call is ending. Be warm and brief.
Thank the caller. If a booking was made, mention the reference number.
Say goodbye naturally.`

	default:
		return ""
	}
}
