package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPlanReviewFinding_CategoryField(t *testing.T) {
	finding := PlanReviewFinding{
		SOPID:    "completeness.goal",
		SOPTitle: "Goal Clarity",
		Severity: "error",
		Status:   "violation",
		Category: "completeness",
		Issue:    "Goal is too vague",
	}

	data, err := json.Marshal(finding)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed PlanReviewFinding
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed.Category != "completeness" {
		t.Errorf("Category = %q, want 'completeness'", parsed.Category)
	}
}

func TestPlanReviewFinding_CategoryOmittedWhenEmpty(t *testing.T) {
	finding := PlanReviewFinding{
		SOPID:    "sop.test",
		SOPTitle: "Test SOP",
		Severity: "error",
		Status:   "violation",
	}

	data, err := json.Marshal(finding)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if strings.Contains(string(data), "category") {
		t.Error("category should be omitted when empty (omitempty tag)")
	}
}

func TestNormalizeVerdict(t *testing.T) {
	errorFinding := PlanReviewFinding{
		SOPID: "scope.path-validation", SOPTitle: "Scope Path Validation",
		Severity: "error", Status: "violation", Category: "completeness",
		Issue: "Scope references non-existent path 'internal-auth'",
	}
	warningFinding := PlanReviewFinding{
		SOPID: "sop.style", SOPTitle: "Style",
		Severity: "warning", Status: "violation",
	}
	compliantFinding := PlanReviewFinding{
		SOPID: "sop.test", SOPTitle: "Test",
		Severity: "info", Status: "compliant",
	}

	tests := []struct {
		name         string
		inputVerdict string
		findings     []PlanReviewFinding
		want         string
	}{
		{
			name:         "approved with error finding gets upgraded to needs_changes",
			inputVerdict: "approved",
			findings:     []PlanReviewFinding{errorFinding},
			want:         "needs_changes",
		},
		{
			name:         "approved with no error findings stays approved",
			inputVerdict: "approved",
			findings:     []PlanReviewFinding{compliantFinding},
			want:         "approved",
		},
		{
			name:         "approved with only warning findings stays approved",
			inputVerdict: "approved",
			findings:     []PlanReviewFinding{warningFinding},
			want:         "approved",
		},
		{
			name:         "needs_changes with no error findings gets downgraded to approved",
			inputVerdict: "needs_changes",
			findings:     []PlanReviewFinding{compliantFinding},
			want:         "approved",
		},
		{
			name:         "needs_changes with error findings stays needs_changes",
			inputVerdict: "needs_changes",
			findings:     []PlanReviewFinding{errorFinding},
			want:         "needs_changes",
		},
		{
			name:         "approved with empty findings stays approved",
			inputVerdict: "approved",
			findings:     nil,
			want:         "approved",
		},
		{
			name:         "needs_changes with empty findings gets downgraded to approved",
			inputVerdict: "needs_changes",
			findings:     nil,
			want:         "approved",
		},
		{
			name:         "approved with mixed findings (one error) gets upgraded",
			inputVerdict: "approved",
			findings:     []PlanReviewFinding{compliantFinding, warningFinding, errorFinding},
			want:         "needs_changes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &PlanReviewResult{
				Verdict:  tt.inputVerdict,
				Findings: tt.findings,
			}
			r.NormalizeVerdict()
			if r.Verdict != tt.want {
				t.Errorf("NormalizeVerdict(): verdict = %q, want %q", r.Verdict, tt.want)
			}
		})
	}
}

// TestFormatFindings_PreservesAllSignal locks in the take-9 fix: every
// diagnostic field that the next-round generator can pin its work to must
// survive serialization to the user prompt. Drop any of phase / target_id
// / evidence / category and small models lose the thread of what to fix.
func TestFormatFindings_PreservesAllSignal(t *testing.T) {
	tests := []struct {
		name    string
		finding PlanReviewFinding
		// expects is a list of substrings that MUST appear in the
		// formatted output, in any order. Each represents one piece of
		// signal a next-round generator depends on.
		expects []string
	}{
		{
			name: "completeness violation with full diagnostic shape",
			finding: PlanReviewFinding{
				Severity:   "error",
				Status:     "violation",
				Category:   "completeness",
				Phase:      "requirements",
				TargetID:   "scenario.X.1.1",
				SOPTitle:   "n/a",
				Issue:      "Scenario X.1.1 requires status=\"healthy\" but requirement specifies status=\"ok\"",
				Evidence:   "Scenario X.1.1 expects status=\"healthy\", while requirement.X.1 and the goal specify status=\"ok\"",
				Suggestion: "Update scenario X.1.1 to expect status=\"ok\"",
			},
			expects: []string{
				"[ERROR]",
				"category=completeness",
				"phase=requirements",
				"target=scenario.X.1.1",
				"Issue: Scenario X.1.1 requires status=\"healthy\"",
				"Evidence: Scenario X.1.1 expects status=\"healthy\"",
				"Suggestion: Update scenario X.1.1",
			},
		},
		{
			name: "sop violation uses SOPTitle in header",
			finding: PlanReviewFinding{
				Severity: "error",
				Status:   "violation",
				Category: "sop",
				Phase:    "plan",
				TargetID: "plan.X",
				SOPID:    "scope.path-validation",
				SOPTitle: "Scope Path Validation",
				Issue:    "Scope references non-existent path 'internal-auth'",
			},
			expects: []string{
				"[ERROR]",
				"Scope Path Validation",
				"phase=plan",
				"target=plan.X",
				"Issue: Scope references non-existent path",
			},
		},
		{
			name: "warning violation still emits structured header",
			finding: PlanReviewFinding{
				Severity: "warning",
				Status:   "violation",
				Category: "completeness",
				Phase:    "scenarios",
				TargetID: "scenario.X.1.2",
				SOPTitle: "n/a",
				Issue:    "Actor not referenced",
			},
			expects: []string{
				"[WARNING]",
				"category=completeness",
				"phase=scenarios",
				"target=scenario.X.1.2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &PlanReviewResult{
				Findings: []PlanReviewFinding{tt.finding},
			}
			got := r.FormatFindings()
			for _, want := range tt.expects {
				if !strings.Contains(got, want) {
					t.Errorf("FormatFindings() missing %q in output:\n%s", want, got)
				}
			}
		})
	}
}

// TestFormatFindings_HeaderFallback verifies the bullet header stays
// meaningful even when SOPTitle is missing entirely (older payload
// shapes, parser quirks). category alone is enough to anchor the model.
func TestFormatFindings_HeaderFallback(t *testing.T) {
	r := &PlanReviewResult{
		Findings: []PlanReviewFinding{
			{Severity: "error", Status: "violation", Category: "completeness"},
		},
	}
	got := r.FormatFindings()
	if !strings.Contains(got, "category=completeness") {
		t.Errorf("FormatFindings() should fall back to category= header when SOPTitle empty:\n%s", got)
	}
}

// TestPlanReviewFinding_ActionFieldsRoundTrip locks in the take-24 fix
// (2026-05-14): structured Action / TargetField / TargetValue must
// survive JSON marshal+unmarshal so the reviewer's committed
// remediation direction is durable across the wire and KV persistence.
// Bidirectional prose suggestions are how take-24 escalated; structured
// fields are how the new fix prevents recurrence.
func TestPlanReviewFinding_ActionFieldsRoundTrip(t *testing.T) {
	finding := PlanReviewFinding{
		SOPID:       "completeness.scope-arch-alignment",
		SOPTitle:    "Scope/Arch Alignment",
		Severity:    "error",
		Status:      "violation",
		Category:    "completeness",
		Phase:       "requirements",
		TargetID:    "requirement.X.2",
		Action:      "add",
		TargetField: "scope.create",
		TargetValue: "src/main/java/org/sensorhub/driver/meshtastic/MeshtasticConnection.java",
		Issue:       "ARCH-001 names Connection.java but plan.scope.create lacks it",
		Suggestion:  "Add MeshtasticConnection.java to scope.create",
		Evidence:    "ARCH-001 components: [Driver, Connection, Message]",
	}

	data, err := json.Marshal(finding)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed PlanReviewFinding
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed.Action != "add" {
		t.Errorf("Action = %q, want 'add'", parsed.Action)
	}
	if parsed.TargetField != "scope.create" {
		t.Errorf("TargetField = %q, want 'scope.create'", parsed.TargetField)
	}
	if !strings.Contains(parsed.TargetValue, "MeshtasticConnection.java") {
		t.Errorf("TargetValue lost on round-trip: %q", parsed.TargetValue)
	}
}

// TestPlanReviewFinding_ActionFieldsOmittedWhenEmpty verifies the
// omitempty tags work — older payloads (compliant findings, warnings
// the reviewer didn't direct, parsing edge cases) should round-trip
// without the new fields appearing in the JSON. Without omitempty,
// every persisted finding would gain three empty strings and the
// downstream JSON parsers in lessons / triples would have to handle
// them everywhere.
func TestPlanReviewFinding_ActionFieldsOmittedWhenEmpty(t *testing.T) {
	finding := PlanReviewFinding{
		SOPID:    "sop.test",
		SOPTitle: "Test SOP",
		Severity: "info",
		Status:   "compliant",
	}
	data, err := json.Marshal(finding)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{`"action"`, `"target_field"`, `"target_value"`} {
		if strings.Contains(string(data), key) {
			t.Errorf("expected %s to be omitted when empty, got: %s", key, data)
		}
	}
}

// TestFormatFindings_ActionDirectiveRenderedFirst verifies the new
// rendering order — when Action is set, the directive line appears
// BEFORE Issue/Evidence/Suggestion. The regen LLM consumes findings
// top-down; putting the committed action first means even if the model
// only attends to the first sub-line it still gets the right direction.
func TestFormatFindings_ActionDirectiveRenderedFirst(t *testing.T) {
	r := &PlanReviewResult{
		Findings: []PlanReviewFinding{
			{
				Severity:    "error",
				Status:      "violation",
				Category:    "completeness",
				Phase:       "requirements",
				TargetID:    "requirement.X.2",
				Action:      "add",
				TargetField: "scope.create",
				TargetValue: "src/main/java/org/sensorhub/driver/meshtastic/MeshtasticConnection.java",
				Issue:       "Plan.scope.create is missing the Connection class file",
				Suggestion:  "Update scope.create to include the connection class",
			},
		},
	}
	got := r.FormatFindings()

	// Directive line must be present, with uppercase verb + backtick-
	// quoted endpoints. Models tokenize backticks as a unit so the
	// path stays a single citation.
	wantDirective := "Action: ADD `src/main/java/org/sensorhub/driver/meshtastic/MeshtasticConnection.java` TO `scope.create`"
	if !strings.Contains(got, wantDirective) {
		t.Errorf("FormatFindings() missing action directive %q in output:\n%s", wantDirective, got)
	}

	// Order: action must precede issue. If issue prints first, the
	// regen LLM may anchor on the prose "missing the Connection class
	// file" and pick the wrong direction (remove from scope to make
	// it consistent with itself).
	actionIdx := strings.Index(got, "Action: ADD")
	issueIdx := strings.Index(got, "Issue:")
	if actionIdx < 0 || issueIdx < 0 || actionIdx > issueIdx {
		t.Errorf("Action directive should appear BEFORE Issue line.\nactionIdx=%d issueIdx=%d\noutput:\n%s", actionIdx, issueIdx, got)
	}
}

// TestFormatFindings_ActionDirectiveDegradesGracefully verifies
// partial-population fallbacks — older findings that have Action but
// not TargetField (or vice versa) still produce a usable directive.
// This matters during the migration window when persisted findings
// pre-date the new fields.
func TestFormatFindings_ActionDirectiveDegradesGracefully(t *testing.T) {
	cases := []struct {
		name     string
		finding  PlanReviewFinding
		wantText string
	}{
		{
			name: "action+value only (no field)",
			finding: PlanReviewFinding{
				Severity: "error", Status: "violation", Category: "completeness",
				Action: "remove", TargetValue: "duplicate-task-X",
			},
			wantText: "Action: REMOVE `duplicate-task-X`",
		},
		{
			name: "action+field only (no value)",
			finding: PlanReviewFinding{
				Severity: "error", Status: "violation", Category: "completeness",
				Action: "add", TargetField: "scope.include",
			},
			wantText: "Action: ADD in `scope.include`",
		},
		{
			name: "rename uses arrow value notation",
			finding: PlanReviewFinding{
				Severity: "error", Status: "violation", Category: "completeness",
				Action: "rename", TargetField: "requirement.X.2.title", TargetValue: "old → new",
			},
			wantText: "Action: RENAME `old → new` IN `requirement.X.2.title`",
		},
		{
			name: "unknown verb still uppercases",
			finding: PlanReviewFinding{
				Severity: "error", Status: "violation", Category: "completeness",
				Action: "consolidate", TargetField: "tasks", TargetValue: "X.1,X.2",
			},
			wantText: "Action: CONSOLIDATE `X.1,X.2` IN `tasks`",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &PlanReviewResult{Findings: []PlanReviewFinding{tc.finding}}
			got := r.FormatFindings()
			if !strings.Contains(got, tc.wantText) {
				t.Errorf("FormatFindings() missing %q in output:\n%s", tc.wantText, got)
			}
		})
	}
}

// TestFormatFindings_NoActionDirectiveWhenNotSet locks in backward
// compatibility: findings with no Action field MUST NOT emit an empty
// "Action:" line. Without this guard, every legacy finding would
// produce noise that confuses regen ("Action:" with no verb).
func TestFormatFindings_NoActionDirectiveWhenNotSet(t *testing.T) {
	r := &PlanReviewResult{
		Findings: []PlanReviewFinding{
			{
				Severity: "error", Status: "violation", Category: "completeness",
				Issue: "Goal is too vague", Suggestion: "Add specifics",
			},
		},
	}
	got := r.FormatFindings()
	if strings.Contains(got, "Action:") {
		t.Errorf("FormatFindings() should not emit Action line when Action is empty:\n%s", got)
	}
	// Issue/Suggestion should still render normally.
	if !strings.Contains(got, "Issue: Goal is too vague") {
		t.Errorf("expected Issue line in output:\n%s", got)
	}
}

func TestErrorFindings_IncludesCompleteness(t *testing.T) {
	result := &PlanReviewResult{
		Verdict: "needs_changes",
		Summary: "Completeness issues found",
		Findings: []PlanReviewFinding{
			{
				SOPID:    "sop.test",
				SOPTitle: "Test SOP",
				Severity: "warning",
				Status:   "compliant",
				Category: "sop",
			},
			{
				SOPID:    "completeness.goal",
				SOPTitle: "Goal Clarity",
				Severity: "error",
				Status:   "violation",
				Category: "completeness",
				Issue:    "Goal is too vague",
			},
		},
	}

	errors := result.ErrorFindings()
	if len(errors) != 1 {
		t.Fatalf("ErrorFindings() count = %d, want 1", len(errors))
	}
	if errors[0].Category != "completeness" {
		t.Errorf("ErrorFindings()[0].Category = %q, want 'completeness'", errors[0].Category)
	}
}
