package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// mockKV implements KVGetter for testing.
type mockKV struct {
	entries map[string][]byte
}

func (m *mockKV) KeysByPrefix(_ context.Context, prefix string) ([]string, error) {
	var keys []string
	for k := range m.entries {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (m *mockKV) Get(_ context.Context, key string) (*KVEntry, error) {
	data, ok := m.entries[key]
	if !ok {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	return &KVEntry{Value: data}, nil
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestMapFilesToRequirements(t *testing.T) {
	tests := []struct {
		name    string
		entries map[string][]byte
		slug    string
		want    map[string][]string
	}{
		{
			name:    "no entries",
			entries: map[string][]byte{},
			slug:    "my-plan",
			want:    map[string][]string{},
		},
		{
			name: "single requirement single file",
			slug: "my-plan",
			want: map[string][]string{
				"src/main.go": {"req-1"},
			},
		},
		{
			name: "multiple requirements same file",
			slug: "my-plan",
			want: map[string][]string{
				"src/auth.go": {"req-1", "req-2"},
			},
		},
		{
			name: "multiple files across requirements",
			slug: "my-plan",
			want: map[string][]string{
				"src/api.go":  {"req-1"},
				"src/db.go":   {"req-2"},
				"src/auth.go": {"req-1", "req-2"},
			},
		},
	}

	// Build entries for each test case.
	tests[1].entries = map[string][]byte{
		"req.my-plan.req-1": mustMarshal(t, RequirementExecution{
			NodeResults: []NodeResult{
				{NodeID: "n1", FilesModified: []string{"src/main.go"}},
			},
		}),
	}

	tests[2].entries = map[string][]byte{
		"req.my-plan.req-1": mustMarshal(t, RequirementExecution{
			NodeResults: []NodeResult{
				{NodeID: "n1", FilesModified: []string{"src/auth.go"}},
			},
		}),
		"req.my-plan.req-2": mustMarshal(t, RequirementExecution{
			NodeResults: []NodeResult{
				{NodeID: "n1", FilesModified: []string{"src/auth.go"}},
			},
		}),
	}

	tests[3].entries = map[string][]byte{
		"req.my-plan.req-1": mustMarshal(t, RequirementExecution{
			NodeResults: []NodeResult{
				{NodeID: "n1", FilesModified: []string{"src/api.go", "src/auth.go"}},
			},
		}),
		"req.my-plan.req-2": mustMarshal(t, RequirementExecution{
			NodeResults: []NodeResult{
				{NodeID: "n1", FilesModified: []string{"src/db.go"}},
				{NodeID: "n2", FilesModified: []string{"src/auth.go"}},
			},
		}),
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kv := &mockKV{entries: tt.entries}
			got, err := MapFilesToRequirements(context.Background(), kv, tt.slug)
			if err != nil {
				t.Fatal(err)
			}

			if len(got) != len(tt.want) {
				t.Fatalf("got %d files, want %d: %v", len(got), len(tt.want), got)
			}
			for file, wantIDs := range tt.want {
				gotIDs := got[file]
				if len(gotIDs) != len(wantIDs) {
					t.Errorf("file %s: got %d reqs, want %d: %v", file, len(gotIDs), len(wantIDs), gotIDs)
					continue
				}
				// Check all expected IDs are present (order may vary).
				gotSet := make(map[string]bool, len(gotIDs))
				for _, id := range gotIDs {
					gotSet[id] = true
				}
				for _, id := range wantIDs {
					if !gotSet[id] {
						t.Errorf("file %s: missing requirement %s in %v", file, id, gotIDs)
					}
				}
			}
		})
	}
}
