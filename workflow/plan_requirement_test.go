//go:build integration

package workflow

import (
	"context"
	"strings"
	"testing"
)

func TestSaveRequirements_InvalidSlug(t *testing.T) {
	err := SaveRequirements(context.Background(), nil, []Requirement{}, "invalid slug!")
	if err == nil {
		t.Error("SaveRequirements() with invalid slug should return error")
	}
}

func TestValidateRequirementDAG(t *testing.T) {
	req := func(id string, deps ...string) Requirement {
		return Requirement{ID: id, DependsOn: deps}
	}

	tests := []struct {
		name        string
		reqs        []Requirement
		wantErr     bool
		errContains string
	}{
		{name: "empty slice passes", reqs: []Requirement{}, wantErr: false},
		{name: "root requirement with no dependencies passes", reqs: []Requirement{req("req-a")}, wantErr: false},
		{
			name:    "valid linear chain passes",
			reqs:    []Requirement{req("req-a"), req("req-b", "req-a"), req("req-c", "req-b")},
			wantErr: false,
		},
		{
			name:    "valid diamond dependency passes",
			reqs:    []Requirement{req("req-a"), req("req-b", "req-a"), req("req-c", "req-a"), req("req-d", "req-b", "req-c")},
			wantErr: false,
		},
		{name: "self-reference returns error", reqs: []Requirement{req("req-a", "req-a")}, wantErr: true, errContains: "depends on itself"},
		{name: "reference to nonexistent requirement returns error", reqs: []Requirement{req("req-a", "req-missing")}, wantErr: true, errContains: "unknown requirement"},
		{
			name:        "simple two-node cycle returns error",
			reqs:        []Requirement{req("req-a", "req-b"), req("req-b", "req-a")},
			wantErr:     true,
			errContains: "cycle detected",
		},
		{
			name:        "three-node cycle returns error",
			reqs:        []Requirement{req("req-a", "req-c"), req("req-b", "req-a"), req("req-c", "req-b")},
			wantErr:     true,
			errContains: "cycle detected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRequirementDAG(tt.reqs)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateRequirementDAG() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errContains != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %q, want it to contain %q", err, tt.errContains)
				}
			}
		})
	}
}

func TestSaveRequirements_RejectsInvalidDAG(t *testing.T) {
	cyclic := []Requirement{
		{ID: "req-a", DependsOn: []string{"req-b"}},
		{ID: "req-b", DependsOn: []string{"req-a"}},
	}
	if err := SaveRequirements(context.Background(), nil, cyclic, "dag-test"); err == nil {
		t.Error("SaveRequirements() with cyclic requirements should return error")
	}
}

func TestSaveRequirements_AcceptsValidDAG(t *testing.T) {
	diamond := []Requirement{
		{ID: "req-a"},
		{ID: "req-b", DependsOn: []string{"req-a"}},
		{ID: "req-c", DependsOn: []string{"req-a"}},
		{ID: "req-d", DependsOn: []string{"req-b", "req-c"}},
	}
	if err := SaveRequirements(context.Background(), nil, diamond, "dag-valid"); err != nil {
		t.Errorf("SaveRequirements() with valid diamond DAG should not error, got: %v", err)
	}
}
