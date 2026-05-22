// Package redis provides Redis-backed session state persistence.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// ─── Client ──────────────────────────────────────────────────────────────

// Client wraps the Redis client with session-specific operations.
type Client struct {
	rdb *goredis.Client
}

// Config holds Redis connection parameters.
type Config struct {
	Addr     string
	Password string
	DB       int
}

// NewClient creates a new Redis client with connection pooling.
func NewClient(cfg Config) (*Client, error) {
	rdb := goredis.NewClient(&goredis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     20,
		MinIdleConns: 5,
		MaxRetries:   3,
		DialTimeout:  2 * time.Second,
		ReadTimeout:  500 * time.Millisecond,
		WriteTimeout: 500 * time.Millisecond,
		PoolTimeout:  1 * time.Second,
	})

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	return &Client{rdb: rdb}, nil
}

// Close closes the Redis connection pool.
func (c *Client) Close() error {
	return c.rdb.Close()
}

// Ping checks Redis connectivity.
func (c *Client) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}

// ─── Session State ───────────────────────────────────────────────────────

const (
	sessionKeyPrefix = "call:session:"
	activeCallsKey   = "call:active"
	sessionTTL       = 35 * time.Minute
)

// SessionState stores the full state of an active call session.
type SessionState struct {
	CallSid             string           `json:"callSid"`
	TenantID            string           `json:"tenantId"`
	PhoneFrom           string           `json:"phoneFrom"`
	PhoneTo             string           `json:"phoneTo"`
	MetaState           string           `json:"metaState"`
	ConvState           string           `json:"convState"`
	ConversationHistory []Turn           `json:"conversationHistory"`
	InputAudioSecs      float64          `json:"inputAudioSecs"`
	OutputAudioSecs     float64          `json:"outputAudioSecs"`
	TextTokensIn        int              `json:"textTokensIn"`
	TextTokensOut       int              `json:"textTokensOut"`
	CreatedAt           time.Time        `json:"createdAt"`
	LastActivity        time.Time        `json:"lastActivity"`
}

// Turn represents a single conversation turn.
type Turn struct {
	Role      string    `json:"role"` // "user", "assistant", "system"
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// ToolCallRecord stores a tool call audit entry.
type ToolCallRecord struct {
	Tool      string          `json:"tool"`
	Args      json.RawMessage `json:"args"`
	Result    json.RawMessage `json:"result"`
	Timestamp time.Time       `json:"timestamp"`
}

// ─── Operations ──────────────────────────────────────────────────────────

// SaveSession stores session state in Redis with TTL.
func (c *Client) SaveSession(ctx context.Context, state *SessionState) error {
	key := sessionKeyPrefix + state.CallSid
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	pipe := c.rdb.Pipeline()
	pipe.Set(ctx, key, data, sessionTTL)
	pipe.SAdd(ctx, activeCallsKey, state.CallSid)
	_, err = pipe.Exec(ctx)
	return err
}

// GetSession retrieves session state from Redis.
func (c *Client) GetSession(ctx context.Context, callSid string) (*SessionState, error) {
	key := sessionKeyPrefix + callSid
	data, err := c.rdb.Get(ctx, key).Bytes()
	if err != nil {
		return nil, err
	}

	var state SessionState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}
	return &state, nil
}

// DeleteSession removes session state and updates the active set.
func (c *Client) DeleteSession(ctx context.Context, callSid string) error {
	key := sessionKeyPrefix + callSid
	pipe := c.rdb.Pipeline()
	pipe.Del(ctx, key)
	pipe.SRem(ctx, activeCallsKey, callSid)
	_, err := pipe.Exec(ctx)
	return err
}

// UpdateMetaState atomically updates the meta state of a session.
func (c *Client) UpdateMetaState(ctx context.Context, callSid, state string) error {
	key := sessionKeyPrefix + callSid

	// Use Lua to atomically read-update-write
	script := `
		local key = KEYS[1]
		local newState = ARGV[1]
		local data = redis.call('GET', key)
		if not data then return 0 end
		local session = cjson.decode(data)
		session.metaState = newState
		session.lastActivity = cjson.encode(os.date("!%Y-%m-%dT%H:%M:%SZ"))
		redis.call('SET', key, cjson.encode(session), 'KEEPTTL')
		return 1
	`
	return c.rdb.Eval(ctx, script, []string{key}, state).Err()
}

// UpdateConvState atomically updates the conversation state and last activity.
func (c *Client) UpdateConvState(ctx context.Context, callSid, state string) error {
	key := sessionKeyPrefix + callSid

	script := `
		local key = KEYS[1]
		local newState = ARGV[1]
		local data = redis.call('GET', key)
		if not data then return 0 end
		local session = cjson.decode(data)
		session.convState = newState
		session.lastActivity = os.date("!%Y-%m-%dT%H:%M:%SZ")
		redis.call('SET', key, cjson.encode(session), 'KEEPTTL')
		return 1
	`
	return c.rdb.Eval(ctx, script, []string{key}, state).Err()
}

// AppendTurn adds a conversation turn to the session history.
func (c *Client) AppendTurn(ctx context.Context, callSid string, turn Turn) error {
	key := sessionKeyPrefix + callSid

	script := `
		local key = KEYS[1]
		local role = ARGV[1]
		local content = ARGV[2]
		local ts = ARGV[3]
		local data = redis.call('GET', key)
		if not data then return 0 end
		local session = cjson.decode(data)
		local turn = {role = role, content = content, timestamp = ts}
		table.insert(session.conversationHistory, turn)
		-- Keep only last 30 turns
		while #session.conversationHistory > 30 do
			table.remove(session.conversationHistory, 1)
		end
		session.lastActivity = ts
		redis.call('SET', key, cjson.encode(session), 'KEEPTTL')
		return 1
	`
	return c.rdb.Eval(ctx, script, []string{key},
		turn.Role, turn.Content, turn.Timestamp.Format(time.RFC3339),
	).Err()
}

// AppendToolCall adds a tool call record to the audit log.
func (c *Client) AppendToolCall(ctx context.Context, callSid string, record ToolCallRecord) error {
	key := "call:tool_audit:" + callSid
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return c.rdb.RPush(ctx, key, data).Err()
}

// GetActiveCallCount returns the number of currently active calls.
func (c *Client) GetActiveCallCount(ctx context.Context) (int64, error) {
	return c.rdb.SCard(ctx, activeCallsKey).Result()
}

// IsActive checks if a call is in the active set.
func (c *Client) IsActive(ctx context.Context, callSid string) (bool, error) {
	return c.rdb.SIsMember(ctx, activeCallsKey, callSid).Result()
}

// RefreshTTL extends the session TTL.
func (c *Client) RefreshTTL(ctx context.Context, callSid string) error {
	key := sessionKeyPrefix + callSid
	return c.rdb.Expire(ctx, key, sessionTTL).Err()
}

// ─── OpenAI Session Mapping ──────────────────────────────────────────────

// SetOpenAISessionID stores the OpenAI session ID for reconnection support.
func (c *Client) SetOpenAISessionID(ctx context.Context, callSid, openAISessID string) error {
	key := "call:openai_session:" + callSid
	return c.rdb.Set(ctx, key, openAISessID, sessionTTL).Err()
}

// GetOpenAISessionID retrieves the stored OpenAI session ID.
func (c *Client) GetOpenAISessionID(ctx context.Context, callSid string) (string, error) {
	key := "call:openai_session:" + callSid
	return c.rdb.Get(ctx, key).Result()
}
