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
				"file_intents": []any{
					map[string]any{"path": "calculator/calc.go", "intent": "modified_existing", "rationale": "Updated existing calculator."},
					map[string]any{"path": "calculator/calc_test.go", "intent": "companion_test", "rationale": "Added unit coverage."},
				},
			},
		},
		{
			name: "missing summary",
			input: map[string]any{
				"files_modified": []any{"calc.go"},
				"file_intents": []any{
					map[string]any{"path": "calc.go", "intent": "modified_existing", "rationale": "Updated existing file."},
				},
			},
			wantError: "summary is required",
		},
		{
			name: "empty summary",
			input: map[string]any{
				"summary":        "",
				"files_modified": []any{"calc.go"},
				"file_intents": []any{
					map[string]any{"path": "calc.go", "intent": "modified_existing", "rationale": "Updated existing file."},
				},
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
				"file_intents":   []any{},
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
				"file_intents": []any{
					map[string]any{"path": "calc.go", "intent": "modified_existing", "rationale": "Updated existing file."},
					map[string]any{"path": "", "intent": "modified_existing", "rationale": "Invalid empty path."},
				},
			},
			wantError: "must be a non-empty path",
		},
		{
			name: "missing file_intents",
			input: map[string]any{
				"summary":        "Did the thing",
				"files_modified": []any{"calc.go"},
			},
			wantError: "file_intents is required",
		},
		{
			name: "file_intents wrong type",
			input: map[string]any{
				"summary":        "Did the thing",
				"files_modified": []any{"calc.go"},
				"file_intents":   "calc.go",
			},
			wantError: "file_intents must be an array",
		},
		{
			name: "file_intents missing files_modified path",
			input: map[string]any{
				"summary":        "Did the thing",
				"files_modified": []any{"calc.go", "calc_test.go"},
				"file_intents": []any{
					map[string]any{"path": "calc.go", "intent": "modified_existing", "rationale": "Updated existing file."},
				},
			},
			wantError: "exactly one entry",
		},
		{
			name: "file_intents path not in files_modified",
			input: map[string]any{
				"summary":        "Did the thing",
				"files_modified": []any{"calc.go"},
				"file_intents": []any{
					map[string]any{"path": "other.go", "intent": "modified_existing", "rationale": "Updated existing file."},
				},
			},
			wantError: "must match a files_modified entry",
		},
		{
			name: "file_intents invalid enum",
			input: map[string]any{
				"summary":        "Did the thing",
				"files_modified": []any{"calc.go"},
				"file_intents": []any{
					map[string]any{"path": "calc.go", "intent": "probably_test", "rationale": "Nope."},
				},
			},
			wantError: "intent must be one of",
		},
		{
			name: "file_intents missing rationale",
			input: map[string]any{
				"summary":        "Did the thing",
				"files_modified": []any{"calc.go"},
				"file_intents": []any{
					map[string]any{"path": "calc.go", "intent": "modified_existing", "rationale": ""},
				},
			},
			wantError: "rationale is required",
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
		"file_intents":   []any{},
	})
	if err == nil {
		t.Error("registered developer validator must reject empty files_modified")
	}
}

// minimalValidArchitecture returns the smallest deliverable that passes the
// trimmed validator: required actors + integrations + test_surface (with at
// least one flow). Helper for the table tests below — each test case varies
// one axis from this baseline.
func minimalValidArchitecture() map[string]any {
	return map[string]any{
		"actors": []any{
			map[string]any{
				"name":     "User",
				"type":     "human",
				"triggers": []any{"sends GET /health"},
			},
		},
		"integrations": []any{
			map[string]any{
				"name":      "HTTP API",
				"direction": "inbound",
				"protocol":  "http",
			},
		},
		"test_surface": map[string]any{
			"integration_flows": []any{
				map[string]any{
					"name":                "health endpoint smoke",
					"description":         "GET /health returns 200 with {status: ok}",
					"components_involved": []any{},
				},
			},
			"e2e_flows": []any{},
		},
	}
}

func TestValidateArchitectDeliverable(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(d map[string]any)
		wantError string // substring; empty means should succeed
	}{
		{
			name:   "minimal valid (actors + integrations + test_surface only)",
			mutate: func(_ map[string]any) {},
		},
		{
			name: "missing actors fails",
			mutate: func(d map[string]any) {
				delete(d, "actors")
			},
			wantError: "actors is required",
		},
		{
			name: "missing integrations fails",
			mutate: func(d map[string]any) {
				delete(d, "integrations")
			},
			wantError: "integrations is required",
		},
		{
			name: "missing test_surface fails",
			mutate: func(d map[string]any) {
				delete(d, "test_surface")
			},
			wantError: "test_surface is required",
		},
		{
			name: "test_surface with both flows empty fails",
			mutate: func(d map[string]any) {
				d["test_surface"] = map[string]any{
					"integration_flows": []any{},
					"e2e_flows":         []any{},
				}
			},
			wantError: "at least one entry in integration_flows or e2e_flows",
		},
		{
			name: "test_surface with only e2e_flows is valid",
			mutate: func(d map[string]any) {
				d["test_surface"] = map[string]any{
					"e2e_flows": []any{
						map[string]any{
							"actor":            "User",
							"steps":            []any{"send GET /health"},
							"success_criteria": []any{"200 status"},
						},
					},
				}
			},
		},
		{
			name: "absent optional fields pass",
			mutate: func(d map[string]any) {
				// the minimal map already lacks them, but pin the contract
				delete(d, "technology_choices")
				delete(d, "component_boundaries")
				delete(d, "data_flow")
				delete(d, "decisions")
			},
		},
		{
			name: "well-formed optional technology_choices passes",
			mutate: func(d map[string]any) {
				d["technology_choices"] = []any{
					map[string]any{
						"category":  "framework",
						"choice":    "net/http",
						"rationale": "stdlib, no deps",
					},
				}
			},
		},
		{
			name: "malformed optional technology_choices still fails (shape preserved)",
			mutate: func(d map[string]any) {
				d["technology_choices"] = []any{
					map[string]any{
						"category": "framework",
						// missing choice + rationale
					},
				}
			},
			wantError: "technology_choices[0] requires",
		},
		{
			name: "well-formed optional component_boundaries passes",
			mutate: func(d map[string]any) {
				d["component_boundaries"] = []any{
					map[string]any{
						"name":           "health-handler",
						"responsibility": "respond to GET /health",
						"dependencies":   []any{},
					},
				}
			},
		},
		{
			name: "malformed optional component_boundaries fails (shape preserved)",
			mutate: func(d map[string]any) {
				d["component_boundaries"] = []any{
					map[string]any{
						"name": "x",
						// missing responsibility + dependencies
					},
				}
			},
			wantError: "component_boundaries[0]",
		},
		{
			name: "well-formed optional decisions passes",
			mutate: func(d map[string]any) {
				d["decisions"] = []any{
					map[string]any{
						"id":        "ARCH-001",
						"title":     "Use stdlib net/http",
						"decision":  "Stdlib only",
						"rationale": "No external deps for a single endpoint",
					},
				}
			},
		},
		{
			name: "malformed optional decisions fails (shape preserved)",
			mutate: func(d map[string]any) {
				d["decisions"] = []any{
					map[string]any{
						"id": "ARCH-001",
						// missing title/decision/rationale
					},
				}
			},
			wantError: "decisions[0] requires",
		},
		{
			name: "actors with invalid type still fails",
			mutate: func(d map[string]any) {
				d["actors"] = []any{
					map[string]any{
						"name":     "X",
						"type":     "alien",
						"triggers": []any{},
					},
				}
			},
			wantError: "actors[0] type must be one of",
		},
		{
			name: "integrations with invalid direction still fails",
			mutate: func(d map[string]any) {
				d["integrations"] = []any{
					map[string]any{
						"name":      "X",
						"direction": "sideways",
						"protocol":  "http",
					},
				}
			},
			wantError: "integrations[0] direction must be one of",
		},
		{
			name: "test_surface integration_flow missing description fails",
			mutate: func(d map[string]any) {
				d["test_surface"] = map[string]any{
					"integration_flows": []any{
						map[string]any{
							"name":                "x",
							"components_involved": []any{},
							// missing description
						},
					},
				}
			},
			wantError: "integration_flows[0] requires",
		},
		{
			name: "test_surface e2e_flow missing actor fails",
			mutate: func(d map[string]any) {
				d["test_surface"] = map[string]any{
					"e2e_flows": []any{
						map[string]any{
							// missing actor
							"steps":            []any{},
							"success_criteria": []any{},
						},
					},
				}
			},
			wantError: "e2e_flows[0] requires an actor",
		},
		// --- pure-library architecture relaxations (2026-05-15) ---
		{
			name: "pure-library: empty integrations + no human actor + empty flows is valid",
			mutate: func(d map[string]any) {
				// Strip the human actor — only the system actor remains.
				d["actors"] = []any{
					map[string]any{
						"name":     "Test runner",
						"type":     "system",
						"triggers": []any{"go test ./..."},
					},
				}
				d["integrations"] = []any{}
				d["test_surface"] = map[string]any{
					"integration_flows": []any{},
					"e2e_flows":         []any{},
				}
			},
		},
		{
			name: "empty integrations fails when a human actor is declared",
			mutate: func(d map[string]any) {
				// Baseline keeps the human actor — emptying integrations must fail.
				d["integrations"] = []any{}
			},
			wantError: "integrations must not be empty",
		},
		{
			name: "empty integrations fails when an integration_target upstream is declared",
			mutate: func(d map[string]any) {
				d["actors"] = []any{
					map[string]any{
						"name":     "Test runner",
						"type":     "system",
						"triggers": []any{"go test ./..."},
					},
				}
				d["integrations"] = []any{}
				d["upstream_resolutions"] = []any{
					map[string]any{
						"name":       "Redis cache",
						"coordinate": "redis:7-alpine",
						"role":       "integration_target",
					},
				}
			},
			wantError: "integrations must not be empty",
		},
		{
			name: "empty test_surface flows fails when integrations declared",
			mutate: func(d map[string]any) {
				// Baseline has 1 integration; emptying flows must fail.
				d["test_surface"] = map[string]any{
					"integration_flows": []any{},
					"e2e_flows":         []any{},
				}
			},
			wantError: "at least one entry in integration_flows or e2e_flows",
		},
		{
			name: "empty test_surface flows valid for pure-library (no human, no integrations)",
			mutate: func(d map[string]any) {
				d["actors"] = []any{
					map[string]any{
						"name":     "Test runner",
						"type":     "system",
						"triggers": []any{"go test ./..."},
					},
				}
				d["integrations"] = []any{}
				d["test_surface"] = map[string]any{
					"integration_flows": []any{},
					"e2e_flows":         []any{},
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := minimalValidArchitecture()
			tt.mutate(d)
			err := ValidateArchitectDeliverable(d)
			switch {
			case tt.wantError == "" && err != nil:
				t.Errorf("expected success, got error: %v", err)
			case tt.wantError != "" && err == nil:
				t.Errorf("expected error containing %q, got success", tt.wantError)
			case tt.wantError != "" && !strings.Contains(err.Error(), tt.wantError):
				t.Errorf("expected error containing %q, got: %v", tt.wantError, err)
			}
		})
	}
}

// TestValidateReviewDeliverable_AutoFillsRejectionType pins the bug
// caught 2026-05-03 on openrouter @easy v4: qwen3-coder-next correctly
// rejected the developer's code 35+ times in a row but consistently
// omitted rejection_type from the submit_work args. The agent saw the
// validator error every time and never adapted (classic example-anchoring
// bias — persona only showed the approved JSON shape). Validator now
// mutates the deliverable to default rejection_type="fixable" rather
// than rejecting, so the loop progresses even when the model forgets.
//
// "fixable" is the safer default because it retries the developer rather
// than terminating the requirement.
func TestValidateReviewDeliverable_AutoFillsRejectionType(t *testing.T) {
	tests := []struct {
		name              string
		input             map[string]any
		wantError         string
		wantRejectionType string
	}{
		{
			name: "rejected with rejection_type missing — auto-fills fixable",
			input: map[string]any{
				"verdict":  "rejected",
				"feedback": "Tests fail at line 42",
			},
			wantRejectionType: "fixable",
		},
		{
			name: "rejected with valid rejection_type — passes through",
			input: map[string]any{
				"verdict":        "rejected",
				"feedback":       "Wrong abstraction throughout",
				"rejection_type": "restructure",
			},
			wantRejectionType: "restructure",
		},
		{
			name: "rejected with invalid rejection_type — still errors, field unchanged",
			input: map[string]any{
				"verdict":        "rejected",
				"feedback":       "...",
				"rejection_type": "bogus",
			},
			wantError:         "rejection_type \"bogus\" is invalid",
			wantRejectionType: "bogus",
		},
		{
			name: "approved with no rejection_type — no auto-fill",
			input: map[string]any{
				"verdict":  "approved",
				"feedback": "All good",
			},
			wantRejectionType: "",
		},
		{
			name: "needs_changes does NOT auto-fill rejection_type",
			input: map[string]any{
				"verdict":  "needs_changes",
				"feedback": "Tweak the names",
			},
			wantRejectionType: "",
		},
		{
			name: "rejected without feedback still errors before auto-fill kicks in",
			input: map[string]any{
				"verdict": "rejected",
			},
			wantError: "feedback is required when verdict is rejected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateReviewDeliverable(tt.input)
			switch {
			case tt.wantError == "" && err != nil:
				t.Errorf("unexpected error: %v", err)
			case tt.wantError != "" && err == nil:
				t.Errorf("expected error containing %q, got success", tt.wantError)
			case tt.wantError != "" && !strings.Contains(err.Error(), tt.wantError):
				t.Errorf("expected error containing %q, got: %v", tt.wantError, err)
			}
			gotRejType, _ := tt.input["rejection_type"].(string)
			if gotRejType != tt.wantRejectionType {
				t.Errorf("rejection_type after validation = %q, want %q", gotRejType, tt.wantRejectionType)
			}
		})
	}
}

// ADR-035 audit site D.6 — pin the named-quirk attribution.
// ValidateReviewDeliverable's auto-fill of missing rejection_type is a
// deliberate tolerance (the existing pre-D.6 behavior); the new
// requirement is that every fire is observable via counter + Warn log.
// This test asserts the counter increments only when the auto-fill
// branch runs — not on the explicit-rejection_type or wrong-value
// branches.
func TestValidateReviewDeliverable_AutoFillFiresCounter(t *testing.T) {
	tests := []struct {
		name          string
		input         map[string]any
		wantFireDelta int64
	}{
		{
			name: "rejected + missing rejection_type — fires once",
			input: map[string]any{
				"verdict":  "rejected",
				"feedback": "Tests fail",
			},
			wantFireDelta: 1,
		},
		{
			name: "rejected + valid rejection_type — no fire",
			input: map[string]any{
				"verdict":        "rejected",
				"feedback":       "Wrong abstraction",
				"rejection_type": "restructure",
			},
			wantFireDelta: 0,
		},
		{
			name: "rejected + invalid rejection_type — no fire (validator errors instead)",
			input: map[string]any{
				"verdict":        "rejected",
				"feedback":       "...",
				"rejection_type": "bogus",
			},
			wantFireDelta: 0,
		},
		{
			name: "approved — no fire (auto-fill only applies on rejected)",
			input: map[string]any{
				"verdict":  "approved",
				"feedback": "good",
			},
			wantFireDelta: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := QuirkStats()[QuirkReviewMissingRejectionType]
			_ = ValidateReviewDeliverable(tt.input)
			after := QuirkStats()[QuirkReviewMissingRejectionType]
			delta := after - before
			if delta != tt.wantFireDelta {
				t.Errorf("QuirkReviewMissingRejectionType fire delta = %d, want %d", delta, tt.wantFireDelta)
			}
		})
	}
}

// QuirkStats() must include the known quirk even when fire count is 0
// — symmetric with workflow/jsonutil/Stats.
func TestQuirkStats_IncludesKnownQuirk(t *testing.T) {
	got := QuirkStats()
	if _, ok := got[QuirkReviewMissingRejectionType]; !ok {
		t.Errorf("QuirkStats() missing entry for %q", QuirkReviewMissingRejectionType)
	}
}

func TestExpectedFieldsHint_ArchitectureMatchesNewSchema(t *testing.T) {
	// The hint is what an LLM sees in the empty-deliverable error message.
	// It must reflect the trimmed required-fields contract, not the historical
	// "all six required" form, otherwise the LLM gets contradictory guidance.
	hint := ExpectedFieldsHint("architecture")
	for _, required := range []string{"actors", "integrations", "test_surface"} {
		if !strings.Contains(hint, required) {
			t.Errorf("hint must mention %q (required field): %q", required, hint)
		}
	}
	// Optional fields can stay out of the hint to keep it readable; not pinned.
}

// TestValidateIntegrationUpstreamPairing pins the Rule 7a structural
// check added in response to smoke 5 (mavlink-hard 2026-06-01) where
// gemini-pro Winston repeatedly produced integrations[].name that did
// not match any upstream_resolutions[].name. The validator only fires
// when BOTH arrays are non-empty — a pure-library architecture is
// allowed to leave upstream_resolutions empty.
func TestValidateIntegrationUpstreamPairing(t *testing.T) {
	tests := []struct {
		name      string
		doc       map[string]any
		wantError string // empty = should pass
	}{
		{
			name: "both arrays empty passes",
			doc:  map[string]any{},
		},
		{
			name: "integrations only, no upstream_resolutions passes (pure-library architecture)",
			doc: map[string]any{
				"integrations": []any{
					map[string]any{"name": "HTTP API", "direction": "inbound", "protocol": "http"},
				},
			},
		},
		{
			name: "integration name matched in upstream_resolutions passes",
			doc: map[string]any{
				"integrations": []any{
					map[string]any{"name": "MAVLink Network", "direction": "outbound", "protocol": "udp"},
				},
				"upstream_resolutions": []any{
					map[string]any{
						"name":       "MAVLink Network",
						"coordinate": "github.com/mavlink/mavlink-go@v0.7.0",
						"role":       "integration_target",
					},
				},
			},
		},
		{
			name: "integration without matching upstream_resolution fails (smoke 5 root cause)",
			doc: map[string]any{
				"integrations": []any{
					map[string]any{"name": "MAVLink Network", "direction": "outbound", "protocol": "udp"},
					map[string]any{"name": "OGC Connected Systems API", "direction": "inbound", "protocol": "http"},
				},
				"upstream_resolutions": []any{
					// Only one of the two integrations has a resolution.
					map[string]any{
						"name":       "MAVLink Network",
						"coordinate": "github.com/mavlink/mavlink-go@v0.7.0",
						"role":       "integration_target",
					},
				},
			},
			wantError: "integrations[1].name=\"OGC Connected Systems API\" has no matching upstream_resolutions",
		},
		{
			name: "renamed upstream_resolution (smoke 5 round-2 pattern) fails with both names in error message",
			doc: map[string]any{
				"integrations": []any{
					map[string]any{"name": "MAVLink Network", "direction": "outbound", "protocol": "udp"},
				},
				"upstream_resolutions": []any{
					// Architect renamed the resolution instead of matching the integration name.
					map[string]any{
						"name":       "Raw MAVLink Endpoint",
						"coordinate": "github.com/mavlink/mavlink-go@v0.7.0",
						"role":       "integration_target",
					},
				},
			},
			wantError: "integrations[0].name=\"MAVLink Network\" has no matching upstream_resolutions",
		},
		{
			name: "integration_target upstream without matching integration fails",
			doc: map[string]any{
				"integrations": []any{
					map[string]any{"name": "HTTP API", "direction": "inbound", "protocol": "http"},
				},
				"upstream_resolutions": []any{
					map[string]any{
						"name":       "HTTP API",
						"coordinate": "stdlib",
						"role":       "runtime_dep",
					},
					map[string]any{
						"name":       "Postgres",
						"coordinate": "postgres:16",
						"role":       "integration_target",
					},
				},
			},
			wantError: "upstream_resolutions[].name=\"Postgres\" has role=\"integration_target\" but no matching integrations",
		},
		{
			name: "non-integration_target upstream (runtime_dep) without matching integration passes",
			doc: map[string]any{
				"integrations": []any{
					map[string]any{"name": "HTTP API", "direction": "inbound", "protocol": "http"},
				},
				"upstream_resolutions": []any{
					map[string]any{
						"name":       "HTTP API",
						"coordinate": "stdlib",
						"role":       "runtime_dep",
					},
					// Lombok is a build_dep with no matching integration — allowed.
					map[string]any{
						"name":       "Lombok",
						"coordinate": "org.projectlombok:lombok:1.18.30",
						"role":       "build_dep",
					},
				},
			},
		},
		{
			name: "upstream_resolution missing name fails with index",
			doc: map[string]any{
				"integrations": []any{
					map[string]any{"name": "HTTP API", "direction": "inbound", "protocol": "http"},
				},
				"upstream_resolutions": []any{
					map[string]any{"coordinate": "stdlib"}, // missing name
				},
			},
			wantError: "upstream_resolutions[0] missing required name",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateIntegrationUpstreamPairing(tc.doc)
			if tc.wantError == "" {
				if err != nil {
					t.Errorf("want no error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.wantError)
			}
			if !strings.Contains(err.Error(), tc.wantError) {
				t.Errorf("error %q missing substring %q", err.Error(), tc.wantError)
			}
		})
	}
}

// TestValidateRequirementsDeliverable pins the #267 contract: every requirement
// MUST carry a non-empty acceptance_criteria array. The ADR-051 requirements
// review (R-req) demands the field; enforcing it at this deterministic boundary
// is what makes the gate convergeable — without it a real-LLM plan with
// missing acceptance_criteria loops R-req to revision-cap exhaustion (the field
// has no producer slot to populate) and the whole plan dead-rejects.
func TestValidateRequirementsDeliverable(t *testing.T) {
	tests := []struct {
		name      string
		input     map[string]any
		wantError string // substring; empty means should succeed
	}{
		{
			name: "valid with acceptance_criteria",
			input: map[string]any{
				"requirements": []any{
					map[string]any{
						"title":               "Hello endpoint returns JSON greeting",
						"description":         "The /hello endpoint returns a JSON greeting.",
						"acceptance_criteria": []any{"GET /hello returns HTTP 200", "body has a 'message' field"},
					},
				},
			},
		},
		{
			name: "missing acceptance_criteria",
			input: map[string]any{
				"requirements": []any{
					map[string]any{"title": "x", "description": "y"},
				},
			},
			wantError: "acceptance_criteria is required",
		},
		{
			name: "empty acceptance_criteria array",
			input: map[string]any{
				"requirements": []any{
					map[string]any{"title": "x", "description": "y", "acceptance_criteria": []any{}},
				},
			},
			wantError: "acceptance_criteria is required",
		},
		{
			name: "missing title",
			input: map[string]any{
				"requirements": []any{
					map[string]any{"description": "y", "acceptance_criteria": []any{"c"}},
				},
			},
			wantError: "title is required",
		},
		{
			name:      "empty requirements array",
			input:     map[string]any{"requirements": []any{}},
			wantError: "requirements is required",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRequirementsDeliverable(tc.input)
			if tc.wantError == "" {
				if err != nil {
					t.Fatalf("want success, got error %q", err.Error())
				}
				return
			}
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.wantError)
			}
			if !strings.Contains(err.Error(), tc.wantError) {
				t.Errorf("error %q missing substring %q", err.Error(), tc.wantError)
			}
		})
	}
}
