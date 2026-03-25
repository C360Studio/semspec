package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
)

// kvPut stores a value as JSON in the KV bucket.
// When kv is nil, the write is silently skipped (test/offline mode).
func kvPut(ctx context.Context, kv *natsclient.KVStore, key string, value any) error {
	if kv == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", key, err)
	}
	if _, err := kv.Put(ctx, key, data); err != nil {
		return fmt.Errorf("kv put %s: %w", key, err)
	}
	return nil
}

// kvGet reads a JSON value from the KV bucket into target.
// Returns a not-found error when kv is nil or key is missing.
func kvGet(ctx context.Context, kv *natsclient.KVStore, key string, target any) error {
	if kv == nil {
		return fmt.Errorf("not found: %s", key)
	}
	entry, err := kv.Get(ctx, key)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			return fmt.Errorf("not found: %s", key)
		}
		return fmt.Errorf("kv get %s: %w", key, err)
	}
	if err := json.Unmarshal(entry.Value, target); err != nil {
		return fmt.Errorf("unmarshal %s: %w", key, err)
	}
	return nil
}

// kvExists checks if a key exists in the KV bucket.
// Returns false when kv is nil.
func kvExists(ctx context.Context, kv *natsclient.KVStore, key string) bool {
	if kv == nil {
		return false
	}
	_, err := kv.Get(ctx, key)
	return err == nil
}

// kvDelete removes a key from the KV bucket.
// Returns nil when kv is nil or key doesn't exist.
func kvDelete(ctx context.Context, kv *natsclient.KVStore, key string) error {
	if kv == nil {
		return nil
	}
	if err := kv.Delete(ctx, key); err != nil && !natsclient.IsKVNotFoundError(err) {
		return fmt.Errorf("kv delete %s: %w", key, err)
	}
	return nil
}

// entityStatesKV is the optional ENTITY_STATES KV bucket for write-through
// side-effect publishing. Set via SetEntityStatesKV during component startup.
// When nil, side-effect publishing is silently skipped.
var entityStatesKV *natsclient.KVStore

// SetEntityStatesKV configures the ENTITY_STATES KV bucket for write-through
// side-effect publishing. Call this during component initialization.
func SetEntityStatesKV(kv *natsclient.KVStore) {
	entityStatesKV = kv
}

// publishEntitySideEffect publishes key semantic facts to ENTITY_STATES for
// graph/rules visibility. This is a write-through side effect — the dedicated
// KV bucket is the source of truth, ENTITY_STATES is for graph queries and
// rule evaluation. Uses the package-level entityStatesKV set via SetEntityStatesKV.
func publishEntitySideEffect(ctx context.Context, entityID string, msgType message.Type, triples []message.Triple) {
	entityKV := entityStatesKV
	if entityKV == nil {
		return
	}
	entity := graph.EntityState{
		ID:          entityID,
		Triples:     triples,
		MessageType: msgType,
		UpdatedAt:   time.Now(),
	}
	data, err := json.Marshal(entity)
	if err != nil {
		slog.Warn("failed to marshal entity side-effect", "entity_id", entityID, "error", err)
		return
	}
	if _, err := entityKV.Put(ctx, entityID, data); err != nil {
		slog.Warn("failed to publish entity side-effect", "entity_id", entityID, "error", err)
	}
}
