// Package llm provides a provider-agnostic LLM client with retry and fallback support.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

var (
	globalCallStore   *CallStore
	globalCallStoreMu sync.RWMutex
	initOnce          sync.Once
	initErr           error // Package-level error for sync.Once pattern
)

// LLMCallsBucket is the KV bucket name for storing LLM call records.
const LLMCallsBucket = "LLM_CALLS"

// DefaultLLMCallsTTL is the default TTL for LLM call records (7 days).
const DefaultLLMCallsTTL = 7 * 24 * time.Hour

// CallRecord represents a single LLM API call with full context for trajectory tracking.
type CallRecord struct {
	// RequestID uniquely identifies this LLM call.
	RequestID string `json:"request_id"`

	// TraceID correlates this call with other messages in the same request flow.
	TraceID string `json:"trace_id"`

	// LoopID is the agent loop that initiated this call (if any).
	LoopID string `json:"loop_id,omitempty"`

	// Capability is the semantic capability requested (planning, writing, coding, etc.).
	Capability string `json:"capability"`

	// Model is the actual model that was used for this call.
	Model string `json:"model"`

	// Provider is the LLM provider (anthropic, ollama, openai, etc.).
	Provider string `json:"provider"`

	// Messages is the input message history sent to the LLM.
	Messages []Message `json:"messages"`

	// Response is the generated content from the LLM.
	Response string `json:"response"`

	// PromptTokens is the number of input/prompt tokens consumed.
	PromptTokens int `json:"prompt_tokens"`

	// CompletionTokens is the number of output/completion tokens generated.
	CompletionTokens int `json:"completion_tokens"`

	// TotalTokens is the total tokens consumed (prompt + completion).
	TotalTokens int `json:"total_tokens"`

	// ContextBudget is the maximum context window size for this model (optional).
	ContextBudget int `json:"context_budget,omitempty"`

	// ContextTruncated indicates if context was truncated to fit budget (optional).
	ContextTruncated bool `json:"context_truncated,omitempty"`

	// FinishReason indicates why generation stopped (stop, length, tool_use, etc.).
	FinishReason string `json:"finish_reason"`

	// StartedAt is when the LLM call began.
	StartedAt time.Time `json:"started_at"`

	// CompletedAt is when the LLM call finished.
	CompletedAt time.Time `json:"completed_at"`

	// DurationMs is the call duration in milliseconds.
	DurationMs int64 `json:"duration_ms"`

	// Error contains any error message if the call failed.
	Error string `json:"error,omitempty"`

	// Retries is the number of retry attempts made.
	Retries int `json:"retries"`

	// FallbacksUsed lists models tried before success (if fallback was needed).
	FallbacksUsed []string `json:"fallbacks_used,omitempty"`
}

// CallStore persists LLM call records to a KV bucket for trajectory tracking.
type CallStore struct {
	nc     *natsclient.Client // NATS client for JetStream operations
	bucket jetstream.KeyValue // KV bucket handle
	ttl    time.Duration      // TTL for stored records
	logger *slog.Logger       // Logger for error reporting
}

// CallStoreOption configures an CallStore.
type CallStoreOption func(*CallStore)

// WithCallsTTL sets the TTL for LLM call records.
func WithCallsTTL(ttl time.Duration) CallStoreOption {
	return func(s *CallStore) {
		s.ttl = ttl
	}
}

// WithStoreLogger sets the logger for the LLM call store.
func WithStoreLogger(logger *slog.Logger) CallStoreOption {
	return func(s *CallStore) {
		s.logger = logger
	}
}

// NewCallStore creates a new LLM call store.
// The context is used for the initial bucket creation/update operation.
func NewCallStore(ctx context.Context, nc *natsclient.Client, opts ...CallStoreOption) (*CallStore, error) {
	if nc == nil {
		return nil, fmt.Errorf("NATS client required")
	}

	s := &CallStore{
		nc:     nc,
		ttl:    DefaultLLMCallsTTL,
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(s)
	}

	js, err := nc.JetStream()
	if err != nil {
		return nil, fmt.Errorf("get jetstream: %w", err)
	}

	// CreateOrUpdateKeyValue is idempotent and handles race conditions
	bucket, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      LLMCallsBucket,
		Description: "LLM call records for trajectory tracking",
		TTL:         s.ttl,
	})
	if err != nil {
		return nil, fmt.Errorf("create/update kv bucket: %w", err)
	}

	s.bucket = bucket
	return s, nil
}

// InitGlobalCallStore initializes the global LLM call store.
// This should be called once during application startup after NATS connection.
// It's safe to call multiple times - subsequent calls return the cached result.
// If initialization fails, all callers receive the same error and GlobalCallStore()
// returns nil (which gracefully disables trajectory tracking).
func InitGlobalCallStore(ctx context.Context, nc *natsclient.Client, opts ...CallStoreOption) error {
	initOnce.Do(func() {
		store, err := NewCallStore(ctx, nc, opts...)
		if err != nil {
			initErr = err
			return
		}
		globalCallStoreMu.Lock()
		globalCallStore = store
		globalCallStoreMu.Unlock()
	})
	return initErr
}

// GlobalCallStore returns the global LLM call store.
// Returns nil if InitGlobalCallStore hasn't been called.
// This follows the same pattern as model.Global() for consistency.
func GlobalCallStore() *CallStore {
	globalCallStoreMu.RLock()
	defer globalCallStoreMu.RUnlock()
	return globalCallStore
}

// Store saves an LLM call record to the KV bucket.
// Key format: {trace_id}.{request_id} to enable prefix queries by trace.
// Uses dot separator since NATS KV keys don't support colons.
func (s *CallStore) Store(ctx context.Context, record *CallRecord) error {
	if record.RequestID == "" {
		return fmt.Errorf("request_id is required")
	}

	// Use trace_id.request_id as key for prefix queries
	// If no trace_id, use just request_id (still queryable individually)
	key := record.RequestID
	if record.TraceID != "" {
		key = fmt.Sprintf("%s.%s", record.TraceID, record.RequestID)
	}

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}

	_, err = s.bucket.Put(ctx, key, data)
	if err != nil {
		return fmt.Errorf("put record: %w", err)
	}

	return nil
}

// Get retrieves an LLM call record by its key (trace_id:request_id or just request_id).
func (s *CallStore) Get(ctx context.Context, key string) (*CallRecord, error) {
	entry, err := s.bucket.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("get record: %w", err)
	}

	var record CallRecord
	if err := json.Unmarshal(entry.Value(), &record); err != nil {
		return nil, fmt.Errorf("unmarshal record: %w", err)
	}

	return &record, nil
}

// GetByTraceID retrieves all LLM call records for a given trace ID.
// Records are returned in chronological order (oldest first).
func (s *CallStore) GetByTraceID(ctx context.Context, traceID string) ([]*CallRecord, error) {
	if traceID == "" {
		return nil, fmt.Errorf("trace_id is required")
	}

	keys, err := s.bucket.Keys(ctx)
	if err != nil {
		// No keys is not an error - return empty slice
		if err == jetstream.ErrNoKeysFound {
			return []*CallRecord{}, nil
		}
		return nil, fmt.Errorf("list keys: %w", err)
	}

	prefix := traceID + "."
	var records []*CallRecord

	for _, key := range keys {
		if !strings.HasPrefix(key, prefix) {
			continue
		}

		entry, err := s.bucket.Get(ctx, key)
		if err != nil {
			// ErrKeyDeleted is expected during concurrent access
			if !errors.Is(err, jetstream.ErrKeyDeleted) && !errors.Is(err, jetstream.ErrKeyNotFound) {
				s.logger.Warn("Failed to get key", "key", key, "error", err)
			}
			continue
		}

		var record CallRecord
		if err := json.Unmarshal(entry.Value(), &record); err != nil {
			s.logger.Warn("Failed to unmarshal record", "key", key, "error", err)
			continue
		}

		records = append(records, &record)
	}

	// Sort by StartedAt (chronological order)
	SortByStartTime(records)

	return records, nil
}

// GetByLoopID retrieves all LLM call records for a given loop ID.
// This is less efficient than GetByTraceID as it requires scanning all keys.
func (s *CallStore) GetByLoopID(ctx context.Context, loopID string) ([]*CallRecord, error) {
	if loopID == "" {
		return nil, fmt.Errorf("loop_id is required")
	}

	keys, err := s.bucket.Keys(ctx)
	if err != nil {
		if err == jetstream.ErrNoKeysFound {
			return []*CallRecord{}, nil
		}
		return nil, fmt.Errorf("list keys: %w", err)
	}

	var records []*CallRecord

	for _, key := range keys {
		entry, err := s.bucket.Get(ctx, key)
		if err != nil {
			// ErrKeyDeleted is expected during concurrent access
			if !errors.Is(err, jetstream.ErrKeyDeleted) && !errors.Is(err, jetstream.ErrKeyNotFound) {
				s.logger.Warn("Failed to get key", "key", key, "error", err)
			}
			continue
		}

		var record CallRecord
		if err := json.Unmarshal(entry.Value(), &record); err != nil {
			s.logger.Warn("Failed to unmarshal record", "key", key, "error", err)
			continue
		}

		if record.LoopID == loopID {
			records = append(records, &record)
		}
	}

	SortByStartTime(records)
	return records, nil
}

// Delete removes an LLM call record by its key.
func (s *CallStore) Delete(ctx context.Context, key string) error {
	return s.bucket.Delete(ctx, key)
}

// SortByStartTime sorts records chronologically by StartedAt.
// Exported for use by trajectory-api and other packages.
func SortByStartTime(records []*CallRecord) {
	sort.Slice(records, func(i, j int) bool {
		return records[i].StartedAt.Before(records[j].StartedAt)
	})
}

// TraceContext holds trace information extracted from context.
type TraceContext struct {
	TraceID string
	LoopID  string
}

// traceContextKey is the context key for trace information.
type traceContextKey struct{}

// WithTraceContext adds trace information to a context.
func WithTraceContext(ctx context.Context, tc TraceContext) context.Context {
	return context.WithValue(ctx, traceContextKey{}, tc)
}

// GetTraceContext extracts trace information from a context.
func GetTraceContext(ctx context.Context) TraceContext {
	if tc, ok := ctx.Value(traceContextKey{}).(TraceContext); ok {
		return tc
	}
	return TraceContext{}
}
