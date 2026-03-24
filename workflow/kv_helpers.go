package workflow

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
)

// kvPutEntity writes a graph.EntityState to the ENTITY_STATES KV bucket.
// The entity ID is used as the KV key. When kv is nil, the write is silently
// skipped (no persistent storage available — test/offline mode).
func kvPutEntity(ctx context.Context, kv *natsclient.KVStore, entityID string, msgType message.Type, triples []message.Triple) error {
	if kv == nil {
		return nil
	}
	entity := graph.EntityState{
		ID:          entityID,
		Triples:     triples,
		MessageType: msgType,
	}
	data, err := json.Marshal(entity)
	if err != nil {
		return fmt.Errorf("marshal entity %s: %w", entityID, err)
	}
	if _, err := kv.Put(ctx, entityID, data); err != nil {
		return fmt.Errorf("kv put %s: %w", entityID, err)
	}
	return nil
}

// kvGetEntity reads a graph.EntityState from the ENTITY_STATES KV bucket.
// Returns ErrPlanNotFound-style errors for missing keys.
// Validates the entity type via payload registry.
func kvGetEntity(ctx context.Context, kv *natsclient.KVStore, entityID string) (*graph.EntityState, error) {
	if kv == nil {
		return nil, fmt.Errorf("entity not found: %s", entityID)
	}
	entry, err := kv.Get(ctx, entityID)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			return nil, fmt.Errorf("entity not found: %s", entityID)
		}
		return nil, fmt.Errorf("kv get %s: %w", entityID, err)
	}

	var entity graph.EntityState
	if err := json.Unmarshal(entry.Value, &entity); err != nil {
		return nil, fmt.Errorf("unmarshal entity %s: %w", entityID, err)
	}

	// Payload registry runtime validation
	if component.CreatePayload(entity.MessageType.Domain, entity.MessageType.Category, entity.MessageType.Version) == nil {
		return nil, fmt.Errorf("unregistered entity type %s for %s", entity.MessageType, entityID)
	}

	return &entity, nil
}

// kvEntityExists checks if an entity exists in the ENTITY_STATES KV bucket.
// Returns false when kv is nil (no storage available).
func kvEntityExists(ctx context.Context, kv *natsclient.KVStore, entityID string) bool {
	if kv == nil {
		return false
	}
	_, err := kv.Get(ctx, entityID)
	return err == nil
}
