// Package llm provides a provider-agnostic LLM client with retry and fallback support.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
)

var (
	globalCallStore   *CallStore
	globalCallStoreMu sync.RWMutex
	initOnce          sync.Once
	initErr           error // Package-level error for sync.Once pattern
)

// graphIngestSubject is the NATS subject for graph entity ingestion.
const graphIngestSubject = "graph.ingest.entity"

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

	// MessagesCount is the number of messages (used for graph representation).
	MessagesCount int `json:"messages_count,omitempty"`

	// ResponsePreview is a truncated response preview (first 500 chars).
	ResponsePreview string `json:"response_preview,omitempty"`

	// Deprecated: StorageRef is kept for backwards compatibility during migration.
	// New code should not use this field. LLM calls are now stored in the knowledge graph.
	StorageRef *message.StorageReference `json:"storage_ref,omitempty"`
}

// CallStore publishes LLM call records to the knowledge graph.
// Graph is the single source of truth - no KV storage.
type CallStore struct {
	nc      *natsclient.Client
	logger  *slog.Logger
	org     string
	project string
}

// CallStoreOption configures a CallStore.
type CallStoreOption func(*CallStore)

// WithOrg sets the organization for entity ID generation.
func WithOrg(org string) CallStoreOption {
	return func(s *CallStore) {
		s.org = org
	}
}

// WithProject sets the project name for entity ID generation.
func WithProject(project string) CallStoreOption {
	return func(s *CallStore) {
		s.project = project
	}
}

// WithStoreLogger sets the logger for the LLM call store.
func WithStoreLogger(logger *slog.Logger) CallStoreOption {
	return func(s *CallStore) {
		s.logger = logger
	}
}

// Deprecated options - kept for backwards compatibility during migration.
// These are no-ops and will be removed in a future version.

// WithArtifactSubject is deprecated - ObjectStore is no longer used.
func WithArtifactSubject(_ string) CallStoreOption {
	return func(_ *CallStore) {
		// No-op: ObjectStore removed, graph is the only storage
	}
}

// WithStorageInstance is deprecated - ObjectStore is no longer used.
func WithStorageInstance(_ string) CallStoreOption {
	return func(_ *CallStore) {
		// No-op: ObjectStore removed, graph is the only storage
	}
}

// NewCallStore creates a new LLM call store.
func NewCallStore(nc *natsclient.Client, opts ...CallStoreOption) (*CallStore, error) {
	if nc == nil {
		return nil, fmt.Errorf("NATS client required")
	}

	s := &CallStore{
		nc:      nc,
		logger:  slog.Default(),
		org:     "local",
		project: "semspec",
	}

	for _, opt := range opts {
		opt(s)
	}

	return s, nil
}

// InitGlobalCallStore initializes the global LLM call store.
// This should be called once during application startup after NATS connection.
// It's safe to call multiple times - subsequent calls return the cached result.
// If initialization fails, all callers receive the same error and GlobalCallStore()
// returns nil (which gracefully disables trajectory tracking).
func InitGlobalCallStore(nc *natsclient.Client, opts ...CallStoreOption) error {
	initOnce.Do(func() {
		store, err := NewCallStore(nc, opts...)
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

// Store publishes an LLM call record to the knowledge graph.
func (s *CallStore) Store(ctx context.Context, record *CallRecord) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if record.RequestID == "" {
		return fmt.Errorf("request_id is required")
	}

	entity := NewLLMCallEntity(record, s.org, s.project)

	payload := &LLMCallPayload{
		ID:         entity.EntityID(),
		TripleData: entity.Triples(),
		UpdatedAt:  record.CompletedAt,
	}

	msg := message.NewBaseMessage(LLMCallType, payload, "llm")
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal entity: %w", err)
	}

	// Use JetStream for reliable delivery
	js, err := s.nc.JetStream()
	if err != nil {
		return fmt.Errorf("get jetstream: %w", err)
	}

	if _, err := js.Publish(ctx, graphIngestSubject, data); err != nil {
		return fmt.Errorf("publish to graph: %w", err)
	}

	s.logger.Debug("Published LLM call to graph",
		"entity_id", entity.EntityID(),
		"request_id", record.RequestID,
		"trace_id", record.TraceID,
		"capability", record.Capability)

	return nil
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
