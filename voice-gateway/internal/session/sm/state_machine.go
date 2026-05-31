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
	StateGreeting       State = "GREETING"
	StateFAQ            State = "FAQ_ANSWER"
	StateCollectBooking State = "COLLECT_BOOKING_DETAILS"
	StateCheckAvail     State = "CHECK_AVAILABILITY"
	StateConfirm        State = "CONFIRM_BOOKING"
	StateModifyRes      State = "MODIFY_RESERVATION"
	StateCancelRes      State = "CANCEL_RESERVATION"
	StateHumanTransfer  State = "HUMAN_TRANSFER"
	StateUnavailable    State = "HANDLE_UNAVAILABLE"
	StateClosing        State = "CLOSING"
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
	if b.Date == "" {
		missing = append(missing, "date")
	}
	if b.Time == "" {
		missing = append(missing, "time")
	}
	if b.PartySize == 0 {
		missing = append(missing, "party_size")
	}
	if b.Name == "" {
		missing = append(missing, "name")
	}
	if b.Phone == "" {
		missing = append(missing, "phone")
	}
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
	tenant         TenantConfig
}

// TenantConfig contains tenant-facing facts used by the prompt layer.
// Core receptionist and industry pack rules must not contain these facts.
type TenantConfig struct {
	BusinessName string
	AgentName    string
	Industry     string
}

// New creates a new conversation state machine in the GREETING state.
func New(restaurantName string) *Machine {
	return NewWithTenant(TenantConfig{
		BusinessName: restaurantName,
		AgentName:    "Alex",
		Industry:     "restaurant",
	})
}

// NewWithTenant creates a state machine with explicit tenant prompt data.
func NewWithTenant(tenant TenantConfig) *Machine {
	if strings.TrimSpace(tenant.BusinessName) == "" {
		tenant.BusinessName = "the business"
	}
	if strings.TrimSpace(tenant.AgentName) == "" {
		tenant.AgentName = "Alex"
	}
	if strings.TrimSpace(tenant.Industry) == "" {
		tenant.Industry = "restaurant"
	}
	return &Machine{
		current: StateGreeting,
		tenant:  tenant,
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
		if v, ok := value.(int); ok {
			m.booking.PartySize = v
		}
		if v, ok := value.(float64); ok {
			m.booking.PartySize = int(v)
		}
	case "date":
		if v, ok := value.(string); ok {
			m.booking.Date = v
		}
	case "time":
		if v, ok := value.(string); ok {
			m.booking.Time = v
		}
	case "name":
		if v, ok := value.(string); ok {
			m.booking.Name = v
		}
	case "phone":
		if v, ok := value.(string); ok {
			m.booking.Phone = v
		}
	case "email":
		if v, ok := value.(string); ok {
			m.booking.Email = v
		}
	case "notes":
		if v, ok := value.(string); ok {
			m.booking.Notes = v
		}
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
		if to == allowed {
			return true
		}
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
		if t.Name == toolName {
			return true
		}
	}
	return false
}

// AllTools returns every tool regardless of state. Used for session initialization.
func AllTools() []Tool {
	return []Tool{
		toolCheckAvailability,
		toolCreateBooking,
		toolCancelBooking,
		toolModifyBooking,
		toolLookupReservation,
		toolGetFAQ,
		toolTransferCall,
	}
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
	return strings.Join([]string{
		m.buildCoreReceptionistPrompt(),
		m.buildRestaurantBehaviourPackPrompt(),
		m.buildTenantConfigPrompt(),
		m.statePrompt(),
	}, "\n\n")
}

func (m *Machine) buildCoreReceptionistPrompt() string {
	return `LAYER 1: VOXLANE CORE RECEPTIONIST
Purpose: answer the phone, identify caller intent, handle simple requests, collect accurate information, and escalate when needed.
You are a receptionist, not a general assistant, friend, sales agent, or chat companion.

Core tone:
- Professional, warm, brief, efficient, calm, natural.
- Use British English.
- Keep replies brief: usually one natural sentence, sometimes two short sentences.
- Ask for one missing detail at a time.
- Do not sound excited.
- Do not say "I'm all ears", "let's chat", "I'm here to help", or invite casual chat.
- Do not over-apologise, over-thank, over-explain, or pretend to know unavailable information.

Core call handling:
- If asked for a manager, owner, named staff member, or transfer, escalate promptly.
- If transfer is unavailable, take a message and callback details.
- For staff lookup, do not pretend to know live availability unless tenant data/tooling confirms it.
- For complaints, stay calm, take brief details, and pass them to the right person.
- For emergencies or immediate danger, tell the caller to call 999 now.
- Close calls only after the task is complete, message is taken, transfer starts, or the caller has no further request.`
}

func (m *Machine) buildRestaurantBehaviourPackPrompt() string {
	return `LAYER 2: RESTAURANT BEHAVIOUR PACK
Handle restaurant-related calls without becoming a casual chatbot.

Booking workflow:
Collect exactly: date → time → guest count → name → contact details.
Ask for ONE missing booking detail at a time. Never ask multiple booking questions together.
If the caller wants to book, reserve, or get a table, treat that as a booking.
Do not ask whether they want a table or a drink after they ask to book a table.
If the caller gives multiple details in one answer, preserve them and ask only for the next missing item.
If the caller says "tomorrow at four" or "tomorrow at 4 pm", do not ask for the date or time again; ask "How many people is that for?"
Never confirm a booking, change, or cancellation until the appropriate tool returns success.
If checking availability, say "One moment, I'll check that." Then use the tool.
After confirming a booking, say: "All set, we'll see you then."

Restaurant enquiries:
- Opening days and times: use tenant data or get_faq only. Do not guess.
- Address, location, parking, menu, live music, events, dietary requirements, group bookings, special occasions, and waiting list questions: use tenant data or get_faq only.
- If the answer is unknown, say briefly that you do not have the confirmed details and offer to take a message or arrange a callback.
- For allergen safety, do not guarantee details unless tenant data explicitly confirms them.`
}

func (m *Machine) buildTenantConfigPrompt() string {
	return fmt.Sprintf(`LAYER 3: TENANT CONFIGURATION
Agent name: %s.
Business name: %s.
Industry pack: %s.
Use the tenant business name in the greeting and caller-facing responses.
Tenant-specific facts such as address, phone, opening hours, parking, live music, manager names, and staff names must come from tenant config or tools, not from core rules.
If a tenant fact is not available, do not guess; offer to take details and arrange a callback.`, m.tenant.AgentName, m.tenant.BusinessName, m.tenant.Industry)
}

func (m *Machine) statePrompt() string {
	switch m.current {
	case StateGreeting:
		return "\n\nSTATE: GREETING\nSay: \"" + m.tenant.BusinessName + ", " + m.tenant.AgentName + " speaking. How can I help?\" Then wait."

	case StateFAQ:
		return "\n\nSTATE: FAQ\nUse get_faq. Answer briefly. If the answer is unknown, offer to take a message or arrange a callback."

	case StateCollectBooking:
		missing := m.booking.MissingFields()
		return fmt.Sprintf(`

STATE: BOOKING — need: %s.
Ask for ONE missing detail in a natural receptionist style.`, strings.Join(missing, ", "))

	case StateCheckAvail:
		return "\n\nCHECKING:\nSay: \"One moment.\" Then call check_availability."

	case StateConfirm:
		return fmt.Sprintf("\n\nCONFIRM: %d/%s/%s for %s. Say it back briefly. Wait for yes. Then create_booking.",
			m.booking.PartySize, m.booking.Date, m.booking.Time, m.booking.Name)

	case StateModifyRes:
		return "\n\nMODIFY:\nAsk for booking reference or name. Then what to change."

	case StateCancelRes:
		return "\n\nCANCEL:\nAsk for reference. Confirm details. Ask if sure."

	case StateHumanTransfer:
		return "\n\nTRANSFER:\n\"Let me put you through.\" Then transfer_call."

	case StateUnavailable:
		return "\n\nBOOKED:\nOffer nearest times. Brief."

	case StateClosing:
		return "\n\nCLOSING:\nOne sentence. Done."

	default:
		return ""
	}
}
