package planner

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/jsonutil"
)

func TestParsePlanFromResult(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantGoal    string
		wantContext string
		wantInclude []string
		wantErr     bool
	}{
		{
			name: "valid plan",
			input: `{
				"goal": "Add user authentication",
				"context": "The API needs secure access",
				"scope": {
					"include": ["api/auth/", "api/middleware/"],
					"exclude": ["api/public/"]
				}
			}`,
			wantGoal:    "Add user authentication",
			wantContext: "The API needs secure access",
			wantInclude: []string{"api/auth/", "api/middleware/"},
		},
		{
			name: "plan with status field",
			input: `{
				"status": "committed",
				"goal": "Implement caching",
				"context": "Performance optimization needed",
				"scope": {
					"include": ["cache/"]
				}
			}`,
			wantGoal:    "Implement caching",
			wantContext: "Performance optimization needed",
			wantInclude: []string{"cache/"},
		},
		{
			name: "minimal plan",
			input: `{
				"goal": "Simple task",
				"context": "",
				"scope": {}
			}`,
			wantGoal:    "Simple task",
			wantContext: "",
			wantInclude: nil,
		},
		{
			name:        "json in code block",
			input:       "Here's the plan:\n```json\n" + `{"goal": "Fenced", "context": "ctx", "scope": {}}` + "\n```\nDone.",
			wantGoal:    "Fenced",
			wantContext: "ctx",
		},
		{
			// ADR-035 C.3 — pin the new behavior gained by migrating to
			// jsonutil.ParseStrict: trailing commas inside the plan JSON,
			// which the previous local extractJSON couldn't handle, now
			// parse cleanly via the trailing_commas named quirk.
			name: "json with trailing commas (named-quirks list handles)",
			input: `{
				"goal": "Trailing-comma plan",
				"context": "ctx",
				"scope": {
					"include": ["a/", "b/",],
				},
			}`,
			wantGoal:    "Trailing-comma plan",
			wantContext: "ctx",
			wantInclude: []string{"a/", "b/"},
		},
		{
			// ADR-035 C.3 — JS-style line comments inside the JSON used
			// to break the local helper (json.Unmarshal would fail);
			// jsonutil's js_line_comments quirk strips them first.
			name: "json with JS line comments (named-quirks list handles)",
			input: "```json\n" + `{
				"goal": "Commented plan", // primary objective
				"context": "ctx", // why this matters
				"scope": {"include": ["x/"]}
			}` + "\n```",
			wantGoal:    "Commented plan",
			wantContext: "ctx",
			wantInclude: []string{"x/"},
		},
		{
			name:    "missing goal",
			input:   `{"context": "No goal here", "scope": {}}`,
			wantErr: true,
		},
		{
			name:    "invalid json",
			input:   `{not valid json}`,
			wantErr: true,
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := parsePlanFromResult(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parsePlanFromResult() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			if got.Goal != tt.wantGoal {
				t.Errorf("Goal = %q, want %q", got.Goal, tt.wantGoal)
			}
			if got.Context != tt.wantContext {
				t.Errorf("Context = %q, want %q", got.Context, tt.wantContext)
			}
			if len(got.Scope.Include) != len(tt.wantInclude) {
				t.Errorf("Scope.Include = %v, want %v", got.Scope.Include, tt.wantInclude)
			} else {
				for i, v := range got.Scope.Include {
					if v != tt.wantInclude[i] {
						t.Errorf("Scope.Include[%d] = %q, want %q", i, v, tt.wantInclude[i])
					}
				}
			}
		})
	}
}

// ADR-035 CP-1 phase-2 wire: parsePlanFromResult must surface
// QuirksFired so handleLoopCompletion can attribute per-fire quirks
// to the SKG via parseincident.Emit. Pin the surfacing across the
// three quirks the planner is realistically going to see.
func TestParsePlanFromResult_SurfacesQuirksFired(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantQuirks []jsonutil.QuirkID
	}{
		{
			name:       "clean direct-unmarshal — no quirks",
			input:      `{"goal":"x","context":"y","scope":{}}`,
			wantQuirks: nil,
		},
		{
			name:       "fenced JSON — fenced_json_wrapper fires",
			input:      "```json\n" + `{"goal":"x","context":"y","scope":{}}` + "\n```",
			wantQuirks: []jsonutil.QuirkID{jsonutil.QuirkFencedJSONWrapper},
		},
		{
			name: "trailing commas — trailing_commas fires",
			input: `{
				"goal":"x",
				"context":"y",
				"scope":{},
			}`,
			wantQuirks: []jsonutil.QuirkID{jsonutil.QuirkTrailingCommas},
		},
		{
			name:       "no JSON found — no quirks but error returned",
			input:      "this is not json at all",
			wantQuirks: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, gotQuirks, _ := parsePlanFromResult(tt.input)
			if len(gotQuirks) != len(tt.wantQuirks) {
				t.Errorf("QuirksFired len = %d, want %d (got %v)", len(gotQuirks), len(tt.wantQuirks), gotQuirks)
				return
			}
			for i, want := range tt.wantQuirks {
				if gotQuirks[i] != want {
					t.Errorf("QuirksFired[%d] = %q, want %q", i, gotQuirks[i], want)
				}
			}
		})
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:    "valid default config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name:    "empty capability is valid (defaults applied at construction)",
			config:  Config{},
			wantErr: false,
		},
		{
			name: "zero retries is valid",
			config: Config{
				MaxGenerationRetries: 0,
				DefaultCapability:    "planning",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRevisionDetection(t *testing.T) {
	// Tests the revision detection logic used in watchPlanStates.
	// A plan is a revision candidate when it has a Goal AND ReviewFindings.
	tests := []struct {
		name           string
		plan           workflow.Plan
		wantRevision   bool
		wantPromptFrom string // "formatted", "summary", or ""
	}{
		{
			name: "fresh plan (no Goal)",
			plan: workflow.Plan{
				Slug:  "fresh",
				Title: "Fresh plan",
			},
			wantRevision: false,
		},
		{
			name: "plan with Goal but no ReviewFindings",
			plan: workflow.Plan{
				Slug: "has-goal",
				Goal: "Add /goodbye endpoint",
			},
			wantRevision: false,
		},
		{
			name: "plan with Goal and ReviewFindings — revision",
			plan: workflow.Plan{
				Slug:                    "revision",
				Goal:                    "Add /goodbye endpoint",
				ReviewFindings:          json.RawMessage(`[{"issue":"too vague"}]`),
				ReviewFormattedFindings: "### Violations\n- Goal is too vague",
				ReviewSummary:           "Goal needs work",
			},
			wantRevision:   true,
			wantPromptFrom: "formatted",
		},
		{
			name: "revision with empty FormattedFindings falls back to Summary",
			plan: workflow.Plan{
				Slug:                    "revision-summary",
				Goal:                    "Add endpoint",
				ReviewFindings:          json.RawMessage(`[{"issue":"vague"}]`),
				ReviewFormattedFindings: "",
				ReviewSummary:           "Summary fallback",
			},
			wantRevision:   true,
			wantPromptFrom: "summary",
		},
		{
			name: "plan with empty ReviewFindings (zero-length JSON)",
			plan: workflow.Plan{
				Slug:           "empty-findings",
				Goal:           "Add endpoint",
				ReviewFindings: json.RawMessage{},
			},
			wantRevision: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isRevision := tt.plan.Goal != "" && len(tt.plan.ReviewFindings) > 0

			if isRevision != tt.wantRevision {
				t.Errorf("isRevision = %v, want %v", isRevision, tt.wantRevision)
			}

			if isRevision {
				revisionPrompt := tt.plan.ReviewFormattedFindings
				if revisionPrompt == "" {
					revisionPrompt = tt.plan.ReviewSummary
				}

				switch tt.wantPromptFrom {
				case "formatted":
					if revisionPrompt != tt.plan.ReviewFormattedFindings {
						t.Errorf("expected formatted findings, got %q", revisionPrompt)
					}
				case "summary":
					if revisionPrompt != tt.plan.ReviewSummary {
						t.Errorf("expected summary fallback, got %q", revisionPrompt)
					}
				}

				if revisionPrompt == "" {
					t.Error("revision prompt should not be empty for a revision plan")
				}
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.DefaultCapability != "planning" {
		t.Errorf("DefaultCapability = %q, want %q", config.DefaultCapability, "planning")
	}
	if config.MaxGenerationRetries != 2 {
		t.Errorf("MaxGenerationRetries = %d, want 2", config.MaxGenerationRetries)
	}
}
