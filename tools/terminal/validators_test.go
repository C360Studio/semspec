package terminal

import (
	"strings"
	"testing"
)

func TestValidateDeveloperDeliverable(t *testing.T) {
	tests := []struct {
		name      string
		input     map[string]any
		wantError string // substring; empty means should succeed
	}{
		{
			name: "valid",
			input: map[string]any{
				"summary":        "Implemented loan calculator with unit tests",
				"files_modified": []any{"calculator/calc.go", "calculator/calc_test.go"},
			},
		},
		{
			name: "missing summary",
			input: map[string]any{
				"files_modified": []any{"calc.go"},
			},
			wantError: "summary is required",
		},
		{
			name: "empty summary",
			input: map[string]any{
				"summary":        "",
				"files_modified": []any{"calc.go"},
			},
			wantError: "summary is required",
		},
		{
			name: "missing files_modified",
			input: map[string]any{
				"summary": "Did the thing",
			},
			wantError: "files_modified is required",
		},
		{
			name: "empty files_modified",
			input: map[string]any{
				"summary":        "Agent stopped with nothing",
				"files_modified": []any{},
			},
			wantError: "files_modified must not be empty",
		},
		{
			name: "files_modified wrong type",
			input: map[string]any{
				"summary":        "Did the thing",
				"files_modified": "calc.go",
			},
			wantError: "files_modified must be an array",
		},
		{
			name: "files_modified contains non-string",
			input: map[string]any{
				"summary":        "Did the thing",
				"files_modified": []any{"calc.go", 42},
			},
			wantError: "must be a string path",
		},
		{
			name: "files_modified contains empty string",
			input: map[string]any{
				"summary":        "Did the thing",
				"files_modified": []any{"calc.go", ""},
			},
			wantError: "must be a non-empty path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDeveloperDeliverable(tt.input)
			if tt.wantError == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantError)
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantError)
			}
		})
	}
}

func TestDeveloperValidatorIsRegistered(t *testing.T) {
	v := GetDeliverableValidator("developer")
	if v == nil {
		t.Fatal("no validator registered for deliverable_type=developer")
	}
	// Empty files_modified should fail through the registered validator too,
	// not just the direct ValidateDeveloperDeliverable call.
	err := v(map[string]any{
		"summary":        "nothing",
		"files_modified": []any{},
	})
	if err == nil {
		t.Error("registered developer validator must reject empty files_modified")
	}
}
