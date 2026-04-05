package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// KVGetter is the minimal KV interface needed by MapFilesToRequirements.
// Satisfied by natsclient.KVStore.
type KVGetter interface {
	KeysByPrefix(ctx context.Context, prefix string) ([]string, error)
	Get(ctx context.Context, key string) (*KVEntry, error)
}

// KVEntry is a minimal KV entry returned by KVGetter.Get.
type KVEntry struct {
	Value []byte
}

// MapFilesToRequirements builds a reverse index from file paths to requirement IDs
// by scanning EXECUTION_STATES for req.<slug>.* entries and aggregating
// FilesModified from all NodeResults.
//
// Returns filePath → []requirementID. If multiple requirements modified the same
// file, all are included (conservative for PR feedback targeting).
func MapFilesToRequirements(ctx context.Context, kv KVGetter, slug string) (map[string][]string, error) {
	prefix := "req." + slug + "."
	keys, err := kv.KeysByPrefix(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("list execution keys for slug %q: %w", slug, err)
	}

	result := make(map[string][]string)

	for _, key := range keys {
		entry, err := kv.Get(ctx, key)
		if err != nil {
			continue // skip entries that can't be read
		}

		var exec RequirementExecution
		if err := json.Unmarshal(entry.Value, &exec); err != nil {
			continue
		}

		// Extract requirement ID from key: req.<slug>.<requirementID>
		reqID := strings.TrimPrefix(key, prefix)
		if reqID == "" {
			continue
		}

		// Aggregate files from all node results.
		for _, nr := range exec.NodeResults {
			for _, file := range nr.FilesModified {
				result[file] = append(result[file], reqID)
			}
		}
	}

	return result, nil
}
