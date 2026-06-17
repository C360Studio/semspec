package terminal

import (
	"fmt"
	"log/slog"

	"github.com/c360studio/semspec/workflow/phases"
	"github.com/c360studio/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// QuirkID identifies a named, reviewed deliverable-validator transform
// applied silently to make a structured deliverable conform to the
// downstream contract. Adding a quirk requires a new constant here, a
// counter, a fire site, and a test fixture. ADR-035 audit site D.6.
//
// This is the content-default flavor of the named-quirks list — the
// shape-strip flavor lives in workflow/jsonutil. Both share the
// "tolerance is named, reviewed, idempotent, and per-fire observable"
// discipline. Per-fire telemetry is a `prometheus.Counter` exposed at
// /metrics via RegisterMetrics.
type QuirkID string

const (
	// QuirkReviewMissingRejectionType fires when ValidateReviewDeliverable
	// auto-fills a missing rejection_type as "fixable" on a verdict=rejected
	// deliverable. Real defect documented at validator line ~135 — qwen3
	// reviewers omitted the field 35+ times across one run. The auto-fill
	// is a deliberate tolerance ("fixable" is the recoverable default;
	// "restructure" terminates the requirement) but every fire is
	// loud-logged so operators can track per-(model, prompt_version)
	// fire rates and characterize the quirk for prompt fixes.
	QuirkReviewMissingRejectionType QuirkID = "review_missing_rejection_type"
)

// reviewMissingRejectionTypeCounter is the per-fire Prometheus counter
// exposed at /metrics when RegisterMetrics is called during startup.
// Single Counter today (one quirk in this package); promoted to a
// CounterVec if a second content-default quirk lands here.
//
// Until registered, the counter still accumulates in memory and
// QuirkStats() still reads it — keeps tests and nil-deps construction
// paths working without the metrics service.
var reviewMissingRejectionTypeCounter = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "semspec_review_missing_rejection_type_total",
	Help: "Total fires of the ValidateReviewDeliverable rejection_type auto-fill quirk (ADR-035 D.6). Indicates how often a reviewer LLM submitted verdict=rejected without an explicit rejection_type field, requiring the validator to default to 'fixable'.",
})

// RegisterMetrics registers tools/terminal's quirk counter with the
// given metrics registry so per-fire telemetry surfaces at /metrics.
// Call once during process startup. Idempotent — semstreams'
// MetricsRegistry returns success on duplicate registration.
//
// When reg is nil, this is a no-op — see reviewMissingRejectionTypeCounter
// godoc for the rationale.
func RegisterMetrics(reg *metric.MetricsRegistry) error {
	if reg == nil {
		return nil
	}
	if err := reg.RegisterCounter("terminal", "review_missing_rejection_type_total", reviewMissingRejectionTypeCounter); err != nil {
		return fmt.Errorf("register terminal review-missing-rejection-type counter: %w", err)
	}
	return registerScopeValidatorMetrics(reg)
}

// fireReviewMissingRejectionType increments the counter and emits a
// Warn log. Warn (not Debug like jsonutil's parse-shape quirks)
// because this quirk is rare relative to parse-shape quirks and
// content-default tolerance is more semantically suspect — auto-filling
// a missing field is "we filled this in for you, audit it" rather than
// "we stripped boilerplate."
func fireReviewMissingRejectionType(verdict, filledValue string) {
	reviewMissingRejectionTypeCounter.Inc()
	slog.Default().Warn("Review deliverable quirk auto-filled",
		"quirk", string(QuirkReviewMissingRejectionType),
		"verdict", verdict,
		"filled_value", filledValue,
	)
}

// QuirkStats returns a snapshot of per-quirk fire counters in this
// package. Operators read via debug endpoints or Health(). Counters
// are monotonically increasing — callers compute deltas themselves.
// Reads the counter value via prometheus testutil — works whether or
// not RegisterMetrics has been called.
func QuirkStats() map[QuirkID]int64 {
	return map[QuirkID]int64{
		QuirkReviewMissingRejectionType: int64(testutil.ToFloat64(reviewMissingRejectionTypeCounter)),
	}
}

// DeliverableValidator validates a structured deliverable from submit_work.
// Returns nil if valid, or an error with a specific, actionable message
// that the LLM can use to fix and retry.
type DeliverableValidator func(deliverable map[string]any) error

// deliverableValidators maps deliverable_type values to their validators.
var deliverableValidators = map[string]DeliverableValidator{
	"plan":         ValidatePlanDeliverable,
	"requirements": ValidateRequirementsDeliverable,
	"scenarios":    ValidateScenariosDeliverable,
	"architecture": ValidateArchitectDeliverable,
	"review":       ValidateReviewDeliverable,
	"developer":    ValidateDeveloperDeliverable,
}

// GetDeliverableValidator returns the validator for the given deliverable type.
// Returns nil if no validator is registered (deliverable accepted as-is).
func GetDeliverableValidator(deliverableType string) DeliverableValidator {
	return deliverableValidators[deliverableType]
}

// ExpectedFieldsHint returns a one-line example showing the expected submit_work
// call for the given deliverable type. Used in error messages when arguments are empty.
func ExpectedFieldsHint(deliverableType string) string {
	switch deliverableType {
	case "plan":
		return `Expected JSON: {"goal": "...", "context": "...", "scope": {"include": [...]}}`
	case "requirements":
		return `Expected JSON: {"requirements": [{"title": "...", "description": "..."}]}`
	case "scenarios":
		return `Expected JSON: {"scenarios": [{"title": "...", "given": "...", "when": "...", "then": ["..."]}]}`
	case "architecture":
		return `Expected JSON: {"actors": [{"name":"...","type":"human|system|scheduler|event","triggers":[...]}], "integrations": [{"name":"...","direction":"inbound|outbound|bidirectional","protocol":"..."}], "test_surface": {"integration_flows":[...], "e2e_flows":[...]}}`
	case "review":
		return `Expected JSON: {"verdict": "approved", "feedback": "..."}`
	default:
		return `Expected JSON: {"summary": "...", "files_modified": ["file.go"]}`
	}
}

// ValidatePlanDeliverable validates a plan deliverable from the planner.
// Required fields: goal, context.
func ValidatePlanDeliverable(d map[string]any) error {
	goal, _ := d["goal"].(string)
	if goal == "" {
		return fmt.Errorf("goal is required — provide a specific, actionable goal describing what to build or fix")
	}
	context, _ := d["context"].(string)
	if context == "" {
		return fmt.Errorf("context is required — describe the current state, why this matters, and key constraints")
	}
	return nil
}

// ValidateRequirementsDeliverable validates a requirements // Expected: {"requirements": [{"title": "...", "description": "..."}, ...]}.
func ValidateRequirementsDeliverable(d map[string]any) error {
	reqs, ok := d["requirements"].([]any)
	if !ok || len(reqs) == 0 {
		return fmt.Errorf("requirements is required — provide an array of requirement objects, each with title and description")
	}
	for i, r := range reqs {
		req, ok := r.(map[string]any)
		if !ok {
			return fmt.Errorf("requirements[%d] must be an object with title and description", i)
		}
		title, _ := req["title"].(string)
		if title == "" {
			return fmt.Errorf("requirements[%d].title is required", i)
		}
		desc, _ := req["description"].(string)
		if desc == "" {
			return fmt.Errorf("requirements[%d].description is required", i)
		}
	}
	return nil
}

// ValidateScenariosDeliverable validates a scenarios // Expected: {"scenarios": [{"title": "...", "given": "...", "when": "...", "then": "..."}, ...]}.
func ValidateScenariosDeliverable(d map[string]any) error {
	scenarios, ok := d["scenarios"].([]any)
	if !ok || len(scenarios) == 0 {
		return fmt.Errorf("scenarios is required — provide an array of scenario objects, each with title, given, when, then")
	}
	for i, s := range scenarios {
		sc, ok := s.(map[string]any)
		if !ok {
			return fmt.Errorf("scenarios[%d] must be an object with title, given, when, then", i)
		}
		title, _ := sc["title"].(string)
		if title == "" {
			return fmt.Errorf("scenarios[%d].title is required", i)
		}
		given, _ := sc["given"].(string)
		when, _ := sc["when"].(string)
		// "then" accepts both a string and an array of strings.
		hasThen := false
		if thenStr, ok := sc["then"].(string); ok && thenStr != "" {
			hasThen = true
		} else if thenArr, ok := sc["then"].([]any); ok && len(thenArr) > 0 {
			hasThen = true
		}
		if given == "" || when == "" || !hasThen {
			return fmt.Errorf("scenarios[%d] requires given, when, and then clauses", i)
		}
	}
	return nil
}

// ValidateReviewDeliverable validates a review deliverable from code, scenario, or plan reviewers.
// Required: verdict (approved/rejected/needs_changes).
// When rejected or needs_changes: feedback is required.
// When rejected: rejection_type must be "fixable" or "restructure" — if missing,
// the validator MUTATES the deliverable to default rejection_type="fixable"
// rather than rejecting the submission.
//
// Defense-in-depth for the bucket-#4 wedge caught 2026-05-03 on the openrouter
// @easy v4 run: qwen3-coder-next reviewer correctly rejected the developer's
// code, included verdict="rejected" and feedback, but consistently omitted
// rejection_type. The agent saw the validator error 35+ times across 5
// reviewer loops and never adapted, burning the iteration budget until 50-iter
// cap fired and the task escalated. The persona's JSON example anchored the
// model on a 2-key shape; rejection_type lived in prose only and got ignored.
//
// "fixable" is the safer default when the model omits the field entirely:
// it routes feedback back to the developer for a retry rather than
// terminating the requirement (which "restructure" does). A model that
// genuinely meant to escalate to restructure would be deliberate enough
// to set the field; the model that forgets is presumed to mean "fix it".
//
// This is mutate-and-pass rather than reject-with-warning so the rest of
// the loop sees the corrected shape downstream — DispatchRetry, lesson
// extraction, and persistence all need rejection_type populated. Logging
// the auto-fill leaves a paper trail for operators reviewing why a
// rejection didn't carry an explicit type.
func ValidateReviewDeliverable(d map[string]any) error {
	verdict, _ := d["verdict"].(string)
	if err := phases.ValidateVerdict(verdict); err != nil {
		return err
	}
	feedback, _ := d["feedback"].(string)
	if verdict == "rejected" || verdict == "needs_changes" {
		if feedback == "" {
			return fmt.Errorf("feedback is required when verdict is %s — provide specific, actionable feedback", verdict)
		}
	}
	if verdict == "rejected" {
		rejType, _ := d["rejection_type"].(string)
		validTypes := map[string]bool{"fixable": true, "restructure": true}
		if !validTypes[rejType] {
			if rejType != "" {
				// Caller supplied a value but it's not in the valid set —
				// surface the error so the agent can correct rather than
				// silently overwriting an intent we don't understand.
				return fmt.Errorf("rejection_type %q is invalid when verdict is rejected — must be one of: fixable, restructure", rejType)
			}
			// Field missing entirely — auto-fill to the safer default and
			// let the loop continue. The rest of the pipeline reads
			// rejection_type from this map, so mutating in place is the
			// idiomatic path. ADR-035 audit site D.6: this is the
			// content-default flavor of the named-quirks list — the fire
			// emits a Warn log + counter so per-(model, prompt_version)
			// regression rates stay observable.
			d["rejection_type"] = "fixable"
			fireReviewMissingRejectionType(verdict, "fixable")
		}
	}
	// Validate scenario_verdicts items when present — each must be an object
	// with scenario_id (string) and passed (bool). Gemini sends bare numbers
	// without this guard, causing post-loop parse failures.
	if svRaw, ok := d["scenario_verdicts"]; ok {
		svArr, ok := svRaw.([]any)
		if !ok {
			return fmt.Errorf("scenario_verdicts must be an array of objects, got %T", svRaw)
		}
		for i, item := range svArr {
			obj, ok := item.(map[string]any)
			if !ok {
				return fmt.Errorf("scenario_verdicts[%d] must be an object with scenario_id and passed, got %T", i, item)
			}
			if _, ok := obj["scenario_id"].(string); !ok {
				return fmt.Errorf("scenario_verdicts[%d].scenario_id is required (string)", i)
			}
			if _, ok := obj["passed"].(bool); !ok {
				return fmt.Errorf("scenario_verdicts[%d].passed is required (boolean)", i)
			}
		}
	}
	return nil
}

// ValidateDeveloperDeliverable validates a developer deliverable from submit_work.
// Required: summary (non-empty) AND files_modified (non-empty array of non-empty strings).
//
// Small models occasionally call submit_work with an empty files_modified array,
// claiming "done" without writing any code. That was silently accepted, sent to
// the structural validator, and burned a full TDD cycle on nothing. Rejecting
// here lets the agent fix and retry within the same loop iteration.
func ValidateDeveloperDeliverable(d map[string]any) error {
	summary, _ := d["summary"].(string)
	if summary == "" {
		return fmt.Errorf("summary is required — describe what was implemented, which tests were added, and any notable decisions")
	}
	filesRaw, ok := d["files_modified"]
	if !ok {
		return fmt.Errorf("files_modified is required — provide the list of files you created or modified (e.g. [\"calculator/calc.go\", \"calculator/calc_test.go\"])")
	}
	files, ok := filesRaw.([]any)
	if !ok {
		return fmt.Errorf("files_modified must be an array of file paths, got %T", filesRaw)
	}
	if len(files) == 0 {
		return fmt.Errorf("files_modified must not be empty — if you have nothing to submit, keep working; do not call submit_work until you have written at least one file")
	}
	for i, f := range files {
		path, ok := f.(string)
		if !ok {
			return fmt.Errorf("files_modified[%d] must be a string path, got %T", i, f)
		}
		if path == "" {
			return fmt.Errorf("files_modified[%d] must be a non-empty path", i)
		}
	}
	return nil
}

// ValidateArchitectDeliverable validates an architecture deliverable.
//
// Required fields are the ones DOWNSTREAM CODE consumes:
//
//   - actors[]:        scenario-generator reads these to seed e2e scenarios
//   - integrations[]:  scenario-generator reads these to seed integration scenarios
//   - test_surface:    execution-manager + qa-reviewer use this to judge coverage
//
// Optional fields are documentation that humans read in plan.md but no code
// downstream reads today: technology_choices, component_boundaries, data_flow,
// decisions. They're shape-checked when present so a malformed entry still
// surfaces, but their absence is not an error.
//
// Trimmed 2026-04-30 PM after observing small models (qwen3.5-35b-a3b on
// OpenRouter) wedge during architecture generation. The earlier "all six
// fields required and non-empty" rule was forcing models to invent
// architecture detail for trivial changes; half of those fields were never
// read by any downstream component. See feedback note on schema-vs-consumer
// alignment.
func ValidateArchitectDeliverable(d map[string]any) error {
	// REQUIRED — downstream consumers depend on these.
	if err := validateActors(d); err != nil {
		return err
	}
	if err := validateIntegrations(d); err != nil {
		return err
	}
	if err := validateIntegrationUpstreamPairing(d); err != nil {
		return err
	}
	if err := validateTestSurface(d); err != nil {
		return err
	}

	// OPTIONAL — validate shape if present, accept absence.
	if err := validateOptionalTechChoices(d); err != nil {
		return err
	}
	if err := validateOptionalComponentBoundaries(d); err != nil {
		return err
	}
	if err := validateOptionalDecisions(d); err != nil {
		return err
	}
	// data_flow is just a free-text string; no shape to validate. Absence is fine.
	return nil
}

// validateOptionalTechChoices validates technology_choices entries when
// present. The field is no longer required overall — code downstream reads
// neither the entries nor their fields — but a malformed entry still
// surfaces a clear error to the LLM rather than a silent shape drift.
func validateOptionalTechChoices(d map[string]any) error {
	raw, present := d["technology_choices"]
	if !present {
		return nil
	}
	techChoices, ok := raw.([]any)
	if !ok {
		return fmt.Errorf("technology_choices must be an array of {category, choice, rationale} objects when present")
	}
	for i, tc := range techChoices {
		obj, ok := tc.(map[string]any)
		if !ok {
			return fmt.Errorf("technology_choices[%d] must be an object with category, choice, rationale", i)
		}
		cat, _ := obj["category"].(string)
		choice, _ := obj["choice"].(string)
		rationale, _ := obj["rationale"].(string)
		if cat == "" || choice == "" || rationale == "" {
			return fmt.Errorf("technology_choices[%d] requires category, choice, and rationale strings", i)
		}
	}
	return nil
}

// validateOptionalComponentBoundaries validates component_boundaries entries
// when present. Same trade as validateOptionalTechChoices.
func validateOptionalComponentBoundaries(d map[string]any) error {
	raw, present := d["component_boundaries"]
	if !present {
		return nil
	}
	components, ok := raw.([]any)
	if !ok {
		return fmt.Errorf("component_boundaries must be an array of {name, responsibility, dependencies[]} objects when present")
	}
	for i, cb := range components {
		obj, ok := cb.(map[string]any)
		if !ok {
			return fmt.Errorf("component_boundaries[%d] must be an object with name, responsibility, dependencies", i)
		}
		name, _ := obj["name"].(string)
		resp, _ := obj["responsibility"].(string)
		if name == "" || resp == "" {
			return fmt.Errorf("component_boundaries[%d] requires name and responsibility strings", i)
		}
		if _, hasDeps := obj["dependencies"]; !hasDeps {
			return fmt.Errorf("component_boundaries[%d] requires a dependencies array (may be empty)", i)
		}
	}
	return nil
}

// validateOptionalDecisions validates decisions entries when present. Same
// trade as the other two optional validators — the field is human-readable
// documentation only; no semspec component reads decisions[].id or .title
// programmatically.
func validateOptionalDecisions(d map[string]any) error {
	raw, present := d["decisions"]
	if !present {
		return nil
	}
	decisions, ok := raw.([]any)
	if !ok {
		return fmt.Errorf("decisions must be an array of {id, title, decision, rationale} objects when present")
	}
	for i, dec := range decisions {
		obj, ok := dec.(map[string]any)
		if !ok {
			return fmt.Errorf("decisions[%d] must be an object with id, title, decision, rationale", i)
		}
		id, _ := obj["id"].(string)
		title, _ := obj["title"].(string)
		decision, _ := obj["decision"].(string)
		rationale, _ := obj["rationale"].(string)
		if id == "" || title == "" || decision == "" || rationale == "" {
			return fmt.Errorf("decisions[%d] requires id, title, decision, and rationale strings", i)
		}
	}
	return nil
}

// validateTestSurface validates the test_surface object. Required because
// execution-manager and qa-reviewer both read it: developer agents use the
// integration_flows and e2e_flows to know what to test, qa-reviewer uses
// them to judge whether actual test coverage matches the architectural
// expectation.
//
// Promoted from optional to required 2026-04-30 PM. An architecture without
// a test_surface leaves qa-reviewer with no judgment basis — it can only
// say "tests ran" instead of "tests covered the integration_flows the
// architect declared."
//
// Shape contract:
//   - test_surface key MUST exist with integration_flows[] and e2e_flows[] arrays.
//   - At least ONE of the two arrays must be non-empty when the architecture
//     has anything to exercise — human actors OR integrations[] entries.
//   - Both arrays MAY be empty for pure-library architectures that have
//     neither human actors NOR integrations[] entries.
//
// Relaxed 2026-05-15: previously rejected any test_surface with both flows
// empty regardless of context. Pure-library architectures (e.g. pkg/math.Sub
// addition) have neither external boundaries nor user-visible flows, so
// both empty is the correct shape. The conditional rejection still catches
// lazy-architect cases — any architecture with integrations or human actors
// must have at least one flow.
func validateTestSurface(d map[string]any) error {
	raw, ok := d["test_surface"]
	if !ok {
		return fmt.Errorf("test_surface is required — provide {integration_flows: [...], e2e_flows: [...]} (both may be empty for pure-library architectures with no human actors and no integrations[] entries)")
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("test_surface must be an object with integration_flows and e2e_flows arrays")
	}
	intFlows, _ := obj["integration_flows"].([]any)
	e2eFlows, _ := obj["e2e_flows"].([]any)
	integrations, _ := d["integrations"].([]any)
	if len(intFlows) == 0 && len(e2eFlows) == 0 {
		if hasHumanActor(d) || len(integrations) > 0 {
			return fmt.Errorf("test_surface requires at least one entry in integration_flows or e2e_flows — derive from your integrations[] and actors[]")
		}
		// pure-library architecture: no human actor, no integrations[] —
		// both flows legitimately empty
	}
	if err := validateIntegrationFlows(intFlows); err != nil {
		return err
	}
	return validateE2EFlows(e2eFlows)
}

// validateIntegrationFlows validates each entry in test_surface.integration_flows.
// Empty array is valid (the deliverable may rely solely on e2e_flows).
func validateIntegrationFlows(flows []any) error {
	for i, f := range flows {
		obj, ok := f.(map[string]any)
		if !ok {
			return fmt.Errorf("test_surface.integration_flows[%d] must be an object with name, components_involved[], description", i)
		}
		name, _ := obj["name"].(string)
		desc, _ := obj["description"].(string)
		if name == "" || desc == "" {
			return fmt.Errorf("test_surface.integration_flows[%d] requires name and description strings", i)
		}
		if _, has := obj["components_involved"]; !has {
			return fmt.Errorf("test_surface.integration_flows[%d] requires a components_involved array (may be empty)", i)
		}
	}
	return nil
}

// validateE2EFlows validates each entry in test_surface.e2e_flows.
// Empty array is valid (the deliverable may rely solely on integration_flows).
func validateE2EFlows(flows []any) error {
	for i, f := range flows {
		obj, ok := f.(map[string]any)
		if !ok {
			return fmt.Errorf("test_surface.e2e_flows[%d] must be an object with actor, steps[], success_criteria[]", i)
		}
		actor, _ := obj["actor"].(string)
		if actor == "" {
			return fmt.Errorf("test_surface.e2e_flows[%d] requires an actor string referencing an entry in actors[]", i)
		}
		if _, has := obj["steps"]; !has {
			return fmt.Errorf("test_surface.e2e_flows[%d] requires a steps array describing the actor's actions", i)
		}
		if _, has := obj["success_criteria"]; !has {
			return fmt.Errorf("test_surface.e2e_flows[%d] requires a success_criteria array describing observable post-conditions", i)
		}
	}
	return nil
}

func validateActors(d map[string]any) error {
	validTypes := map[string]bool{"human": true, "system": true, "scheduler": true, "event": true}
	actors, ok := d["actors"].([]any)
	if !ok || len(actors) == 0 {
		return fmt.Errorf("actors is required — provide an array of {name, type, triggers[]} objects describing who or what initiates actions")
	}
	for i, a := range actors {
		obj, ok := a.(map[string]any)
		if !ok {
			return fmt.Errorf("actors[%d] must be an object with name, type, triggers", i)
		}
		name, _ := obj["name"].(string)
		actorType, _ := obj["type"].(string)
		if name == "" || actorType == "" {
			return fmt.Errorf("actors[%d] requires name and type strings", i)
		}
		if !validTypes[actorType] {
			return fmt.Errorf("actors[%d] type must be one of: human, system, scheduler, event (got %q)", i, actorType)
		}
		if _, hasTriggers := obj["triggers"]; !hasTriggers {
			return fmt.Errorf("actors[%d] requires a triggers array", i)
		}
	}
	return nil
}

// validateIntegrations enforces structural shape on the integrations[] array.
//
// integrations[] is REQUIRED as a key (the architect must consider external
// boundaries even if there are none) but MAY be empty for pure-library or
// pure-internal-tool architectures with no external surface. The conditional
// rejection catches the common lazy-architect case: declaring a human actor
// or an integration_target upstream resolution without the corresponding
// integration entry.
//
// Relaxed 2026-05-15: previously rejected any empty array, which forced
// pure-library scenarios (e.g. test/e2e/fixtures/mock-responses/qa-unit*
// adding a Sub function to pkg/math) to declare stretched integrations
// like "Go test toolchain" just to satisfy the validator. Now empty is
// valid when the architecture has neither human actors nor integration_target
// upstream resolutions.
func validateIntegrations(d map[string]any) error {
	validDirections := map[string]bool{"inbound": true, "outbound": true, "bidirectional": true}
	integrations, ok := d["integrations"].([]any)
	if !ok {
		return fmt.Errorf("integrations is required — provide an array of {name, direction, protocol} objects (use [] for pure libraries with no external boundaries)")
	}
	if len(integrations) == 0 {
		if hasHumanActor(d) {
			return fmt.Errorf("integrations must not be empty — architecture declares a human actor, so at least one inbound integration must describe the surface the human reaches (UI, CLI, API, etc.)")
		}
		if hasIntegrationTargetUpstream(d) {
			return fmt.Errorf("integrations must not be empty — architecture declares an upstream_resolutions[] entry with role=integration_target, so the integration that consumes it must appear here")
		}
		return nil
	}
	for i, ig := range integrations {
		obj, ok := ig.(map[string]any)
		if !ok {
			return fmt.Errorf("integrations[%d] must be an object with name, direction, protocol", i)
		}
		name, _ := obj["name"].(string)
		direction, _ := obj["direction"].(string)
		protocol, _ := obj["protocol"].(string)
		if name == "" || direction == "" || protocol == "" {
			return fmt.Errorf("integrations[%d] requires name, direction, and protocol strings", i)
		}
		if !validDirections[direction] {
			return fmt.Errorf("integrations[%d] direction must be one of: inbound, outbound, bidirectional (got %q)", i, direction)
		}
	}
	return nil
}

// validateIntegrationUpstreamPairing enforces the bidirectional name
// invariant between integrations[] and upstream_resolutions[] — Rule 7a
// "Upstream Resolution Discipline" from Winston's persona prompt.
//
// The check fires only when BOTH arrays are non-empty. An architecture
// that legitimately has no external surface (pure-library / stdlib-only)
// can leave upstream_resolutions empty; this validator does not force
// the architect to invent a resolution where there's nothing to resolve.
// But once the architect has authored resolutions, the names MUST match
// across both arrays:
//
//   - Every integration name MUST appear as an upstream_resolutions[].name.
//   - Every upstream_resolutions[] entry with role="integration_target"
//     MUST have a matching integration name.
//
// The rule has lived in Winston's persona prompt since 2026-05-15, but
// real-LLM smokes (mavlink-hard 2026-06-01) reproducibly showed
// gemini-pro Winston either renaming one side or partially populating
// resolutions — burning 3 LLM plan-reviewer rounds for what is a
// deterministic check. Catching it here surfaces the exact mismatched
// name in the architecture-generator's retry feedback so Winston gets a
// precise edit instruction on attempt 2 instead of an LLM-paraphrased
// rejection 3 rounds later.
//
// Matching is exact-string equality — no fuzzy-matching. The persona
// prompt is the one place where authoring discipline lives; the
// operator-friendly mismatch error here is the convergence lever.
func validateIntegrationUpstreamPairing(d map[string]any) error {
	integrations, _ := d["integrations"].([]any)
	resolutions, _ := d["upstream_resolutions"].([]any)

	if len(integrations) == 0 || len(resolutions) == 0 {
		return nil
	}

	resByName := make(map[string]struct{}, len(resolutions))
	for i, r := range resolutions {
		obj, ok := r.(map[string]any)
		if !ok {
			continue
		}
		name, _ := obj["name"].(string)
		if name == "" {
			return fmt.Errorf("upstream_resolutions[%d] missing required name", i)
		}
		resByName[name] = struct{}{}
	}

	for i, ig := range integrations {
		obj, ok := ig.(map[string]any)
		if !ok {
			continue
		}
		name, _ := obj["name"].(string)
		if name == "" {
			continue // validateIntegrations already catches this; don't double-error
		}
		if _, ok := resByName[name]; !ok {
			return fmt.Errorf("integrations[%d].name=%q has no matching upstream_resolutions[] entry — every integration must be paired with an upstream_resolutions[] entry of the same name (Rule 7a). Add the resolution with role=\"integration_target\" and the resolved coordinate + apis citations, OR remove the integration if it is not a real external dependency", i, name)
		}
	}

	for _, r := range resolutions {
		obj, ok := r.(map[string]any)
		if !ok {
			continue
		}
		role, _ := obj["role"].(string)
		if role != "integration_target" {
			continue
		}
		name, _ := obj["name"].(string)
		if name == "" {
			continue
		}
		matched := false
		for _, ig := range integrations {
			io, ok := ig.(map[string]any)
			if !ok {
				continue
			}
			if igName, _ := io["name"].(string); igName == name {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("upstream_resolutions[].name=%q has role=\"integration_target\" but no matching integrations[] entry — every integration_target upstream resolution must be paired with an integration of the same name (Rule 7a). Add the integration with direction + protocol, OR change the role if the dependency is in-process", name)
		}
	}

	return nil
}

// hasHumanActor returns true when actors[] contains at least one entry
// with type=="human". Used by validateIntegrations and validateTestSurface
// to decide whether empty integrations[] / test_surface flow arrays are
// allowed (no human actor → pure-internal architecture).
func hasHumanActor(d map[string]any) bool {
	actors, _ := d["actors"].([]any)
	for _, a := range actors {
		obj, ok := a.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := obj["type"].(string); t == "human" {
			return true
		}
	}
	return false
}

// hasIntegrationTargetUpstream returns true when upstream_resolutions[]
// contains at least one entry with role=="integration_target". Used by
// validateIntegrations to require a corresponding integration entry when
// the architect declares an integration target.
func hasIntegrationTargetUpstream(d map[string]any) bool {
	resolutions, _ := d["upstream_resolutions"].([]any)
	for _, r := range resolutions {
		obj, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if role, _ := obj["role"].(string); role == "integration_target" {
			return true
		}
	}
	return false
}
