package agentgraph

import (
	"context"

	"github.com/c360studio/semstreams/natsclient"
)

// KVStoreAdapter wraps *natsclient.KVStore to satisfy the KVEntityStore
// interface. The adapter converts between natsclient.KVEntry and the
// agentgraph.KVEntry type so that the agentgraph package doesn't import
// natsclient directly in its interface definition.
type KVStoreAdapter struct {
	store *natsclient.KVStore
}

// NewKVStoreAdapter creates a KVStoreAdapter from a *natsclient.KVStore.
func NewKVStoreAdapter(store *natsclient.KVStore) *KVStoreAdapter {
	return &KVStoreAdapter{store: store}
}

// Get retrieves an entity by key, converting the natsclient entry type.
func (a *KVStoreAdapter) Get(ctx context.Context, key string) (KVEntry, error) {
	entry, err := a.store.Get(ctx, key)
	if err != nil {
		return KVEntry{}, err
	}
	return KVEntry{
		Key:      entry.Key,
		Value:    entry.Value,
		Revision: entry.Revision,
	}, nil
}

// Put writes a value to the given key, returning the revision.
func (a *KVStoreAdapter) Put(ctx context.Context, key string, value []byte) (uint64, error) {
	return a.store.Put(ctx, key, value)
}

// UpdateWithRetry performs an atomic CAS update with automatic retry on conflicts.
func (a *KVStoreAdapter) UpdateWithRetry(ctx context.Context, key string, updateFn func(current []byte) ([]byte, error)) error {
	return a.store.UpdateWithRetry(ctx, key, updateFn)
}

// KeysByPrefix returns all keys matching the given prefix.
func (a *KVStoreAdapter) KeysByPrefix(ctx context.Context, prefix string) ([]string, error) {
	return a.store.KeysByPrefix(ctx, prefix)
}
