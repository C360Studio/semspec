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
	globalToolCallStore   *ToolCallStore
	globalToolCallStoreMu sync.RWMutex
	toolCallInitOnce      sync.Once
	toolCallInitErr       error
)

// ToolCallsBucket is the KV bucket name for storing tool call records.
const ToolCallsBucket = "TOOL_CALLS"

// DefaultToolCallsTTL is the default TTL for tool call records (7 days).
const DefaultToolCallsTTL = 7 * 24 * time.Hour

// ToolCallRecord represents a single tool execution with context for trajectory tracking.
type ToolCallRecord struct {
	// CallID uniquely identifies this tool call.
	CallID string `json:"call_id"`

	// TraceID correlates this call with other messages in the same request flow.
	TraceID string `json:"trace_id"`

	// LoopID is the agent loop that initiated this call (if any).
	LoopID string `json:"loop_id,omitempty"`

	// ToolName is the name of the tool executed (e.g. "file_read", "git_status").
	ToolName string `json:"tool_name"`

	// Parameters is the JSON-encoded tool parameters (truncated for storage).
	Parameters string `json:"parameters"`

	// Result is the truncated output from the tool execution.
	Result string `json:"result"`

	// Status is the execution status ("success", "error").
	Status string `json:"status"`

	// Error contains any error message if the call failed.
	Error string `json:"error,omitempty"`

	// StartedAt is when the tool call began.
	StartedAt time.Time `json:"started_at"`

	// CompletedAt is when the tool call finished.
	CompletedAt time.Time `json:"completed_at"`

	// DurationMs is the call duration in milliseconds.
	DurationMs int64 `json:"duration_ms"`
}

// ToolCallStore persists tool call records to a KV bucket for trajectory tracking.
type ToolCallStore struct {
	nc     *natsclient.Client // NATS client for JetStream operations
	bucket jetstream.KeyValue // KV bucket handle
	ttl    time.Duration      // TTL for stored records
	logger *slog.Logger       // Logger for error reporting
}

// ToolCallStoreOption configures a ToolCallStore.
type ToolCallStoreOption func(*ToolCallStore)

// WithToolCallsTTL sets the TTL for tool call records.
func WithToolCallsTTL(ttl time.Duration) ToolCallStoreOption {
	return func(s *ToolCallStore) {
		s.ttl = ttl
	}
}

// WithToolCallStoreLogger sets the logger for the tool call store.
func WithToolCallStoreLogger(logger *slog.Logger) ToolCallStoreOption {
	return func(s *ToolCallStore) {
		s.logger = logger
	}
}

// NewToolCallStore creates a new tool call store.
// The context is used for the initial bucket creation/update operation.
func NewToolCallStore(ctx context.Context, nc *natsclient.Client, opts ...ToolCallStoreOption) (*ToolCallStore, error) {
	if nc == nil {
		return nil, fmt.Errorf("NATS client required")
	}

	s := &ToolCallStore{
		nc:     nc,
		ttl:    DefaultToolCallsTTL,
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
		Bucket:      ToolCallsBucket,
		Description: "Tool call records for trajectory tracking",
		TTL:         s.ttl,
	})
	if err != nil {
		return nil, fmt.Errorf("create/update kv bucket: %w", err)
	}

	s.bucket = bucket
	return s, nil
}

// InitGlobalToolCallStore initializes the global tool call store.
// This should be called once during application startup after NATS connection.
// It's safe to call multiple times - subsequent calls return the cached result.
// If initialization fails, all callers receive the same error and GlobalToolCallStore()
// returns nil (which gracefully disables tool call recording).
func InitGlobalToolCallStore(ctx context.Context, nc *natsclient.Client, opts ...ToolCallStoreOption) error {
	toolCallInitOnce.Do(func() {
		store, err := NewToolCallStore(ctx, nc, opts...)
		if err != nil {
			toolCallInitErr = err
			return
		}
		globalToolCallStoreMu.Lock()
		globalToolCallStore = store
		globalToolCallStoreMu.Unlock()
	})
	return toolCallInitErr
}

// GlobalToolCallStore returns the global tool call store.
// Returns nil if InitGlobalToolCallStore hasn't been called or failed.
func GlobalToolCallStore() *ToolCallStore {
	globalToolCallStoreMu.RLock()
	defer globalToolCallStoreMu.RUnlock()
	return globalToolCallStore
}

// Store saves a tool call record to the KV bucket.
// Key format: {trace_id}.{call_id} to enable prefix queries by trace.
func (s *ToolCallStore) Store(ctx context.Context, record *ToolCallRecord) error {
	if record.CallID == "" {
		return fmt.Errorf("call_id is required")
	}

	// Use trace_id.call_id as key for prefix queries
	// If no trace_id, use just call_id (still queryable individually)
	key := record.CallID
	if record.TraceID != "" {
		key = fmt.Sprintf("%s.%s", record.TraceID, record.CallID)
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

// Get retrieves a tool call record by its key (trace_id.call_id or just call_id).
func (s *ToolCallStore) Get(ctx context.Context, key string) (*ToolCallRecord, error) {
	entry, err := s.bucket.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("get record: %w", err)
	}

	var record ToolCallRecord
	if err := json.Unmarshal(entry.Value(), &record); err != nil {
		return nil, fmt.Errorf("unmarshal record: %w", err)
	}

	return &record, nil
}

// GetByTraceID retrieves all tool call records for a given trace ID.
// Records are returned in chronological order (oldest first).
func (s *ToolCallStore) GetByTraceID(ctx context.Context, traceID string) ([]*ToolCallRecord, error) {
	if traceID == "" {
		return nil, fmt.Errorf("trace_id is required")
	}

	keys, err := s.bucket.Keys(ctx)
	if err != nil {
		// No keys is not an error - return empty slice
		if err == jetstream.ErrNoKeysFound {
			return []*ToolCallRecord{}, nil
		}
		return nil, fmt.Errorf("list keys: %w", err)
	}

	prefix := traceID + "."
	var records []*ToolCallRecord

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

		var record ToolCallRecord
		if err := json.Unmarshal(entry.Value(), &record); err != nil {
			s.logger.Warn("Failed to unmarshal record", "key", key, "error", err)
			continue
		}

		records = append(records, &record)
	}

	// Sort by StartedAt (chronological order)
	SortToolCallsByStartTime(records)

	return records, nil
}

// GetByLoopID retrieves all tool call records for a given loop ID.
// This is less efficient than GetByTraceID as it requires scanning all keys.
func (s *ToolCallStore) GetByLoopID(ctx context.Context, loopID string) ([]*ToolCallRecord, error) {
	if loopID == "" {
		return nil, fmt.Errorf("loop_id is required")
	}

	keys, err := s.bucket.Keys(ctx)
	if err != nil {
		if err == jetstream.ErrNoKeysFound {
			return []*ToolCallRecord{}, nil
		}
		return nil, fmt.Errorf("list keys: %w", err)
	}

	var records []*ToolCallRecord

	for _, key := range keys {
		entry, err := s.bucket.Get(ctx, key)
		if err != nil {
			// ErrKeyDeleted is expected during concurrent access
			if !errors.Is(err, jetstream.ErrKeyDeleted) && !errors.Is(err, jetstream.ErrKeyNotFound) {
				s.logger.Warn("Failed to get key", "key", key, "error", err)
			}
			continue
		}

		var record ToolCallRecord
		if err := json.Unmarshal(entry.Value(), &record); err != nil {
			s.logger.Warn("Failed to unmarshal record", "key", key, "error", err)
			continue
		}

		if record.LoopID == loopID {
			records = append(records, &record)
		}
	}

	SortToolCallsByStartTime(records)
	return records, nil
}

// Delete removes a tool call record by its key.
func (s *ToolCallStore) Delete(ctx context.Context, key string) error {
	return s.bucket.Delete(ctx, key)
}

// SortToolCallsByStartTime sorts tool call records chronologically by StartedAt.
// Exported for use by trajectory-api and other packages.
func SortToolCallsByStartTime(records []*ToolCallRecord) {
	sort.Slice(records, func(i, j int) bool {
		return records[i].StartedAt.Before(records[j].StartedAt)
	})
}
