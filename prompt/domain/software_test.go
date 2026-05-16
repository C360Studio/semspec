package domain

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/prompt"
)

func TestSoftwareFragments(t *testing.T) {
	fragments := Software()
	if len(fragments) == 0 {
		t.Fatal("expected non-empty software fragments")
	}

	// Check all fragments have IDs
	ids := make(map[string]bool)
	for _, f := range fragments {
		if f.ID == "" {
			t.Error("fragment with empty ID")
		}
		if ids[f.ID] {
			t.Errorf("duplicate fragment ID: %s", f.ID)
		}
		ids[f.ID] = true
	}

	// Verify key fragments exist
	required := []string{
		"software.developer.system-base",
		"software.developer.tool-directive",
		"software.developer.role-context",
		"software.shared.submit-work-directive",
		"software.shared.prior-work-directive",
		"software.planner.system-base",
		"software.plan-reviewer.system-base",
		"software.reviewer.system-base",
		"software.requirement-generator.system-base",
		"software.scenario-generator.system-base",
		"software.task-decomposer.system-base",
		"software.provider.ollama-tool-enforcement",
	}
	for _, id := range required {
		if !ids[id] {
			t.Errorf("missing required fragment: %s", id)
		}
	}
}

func TestSoftwareDeveloperAssembly(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)
	r.Register(prompt.ToolGuidanceFragment(prompt.DefaultToolGuidance()))

	a := prompt.NewAssembler(r)
	result := a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleDeveloper,
		Provider:       prompt.ProviderAnthropic,
		AvailableTools: []string{"bash", "submit_work", "graph_search"},
		SupportsTools:  true,
	})

	if !strings.Contains(result.SystemMessage, "developer implementing code changes") {
		t.Error("expected developer identity in system message")
	}
	if !strings.Contains(result.SystemMessage, "bash") {
		t.Error("expected tool directive mentioning bash")
	}
	if !strings.Contains(result.SystemMessage, "<identity>") {
		t.Error("expected XML formatting for Anthropic provider")
	}
	// Bug-#4 pin: the developer must be told scope is mandatory and that
	// files_modified must respect scope.do_not_touch / scope.exclude.
	// Caught 2026-05-03 on openrouter @easy /health where the dev burned
	// ~568K tokens then submitted refresh-token code in a do-not-touch
	// auth file no one had asked it to edit.
	if !strings.Contains(result.SystemMessage, "Scope is mandatory") {
		t.Error("expected 'Scope is mandatory' rule in developer workspace-contract fragment")
	}
	if !strings.Contains(result.SystemMessage, "scope.do_not_touch") {
		t.Error("expected explicit reference to scope.do_not_touch in developer prompt")
	}
	// v10 hallucination wedge pin: the developer must be told that read-only
	// bash (cat/ls/grep/find) does not modify the worktree, and that the
	// pre-reviewer git status check rejects claim/observation mismatches.
	// Caught 2026-05-03 on openrouter @easy /health where the dev ran only
	// `cat main.go` × 3 and submitted confident prose about implementing a
	// /health endpoint, never writing anything.
	wantHallucinationPins := []string{
		"Reading a file is not modifying it",
		"BEFORE the validator or reviewer runs",
		"cat > path",
	}
	for _, want := range wantHallucinationPins {
		if !strings.Contains(result.SystemMessage, want) {
			t.Errorf("expected hallucination-wedge guidance %q in workspace-contract", want)
		}
	}

	// Take-23 pin: dev created a .go file in an existing dir and declared
	// the wrong package name (matched dir-name instead of the existing
	// sibling's `package X`), then imported it bare instead of via the
	// project's module path. Five TDD cycles wasted on the same compile
	// error. Persona must instruct the dev to read a sibling file before
	// declaring package/namespace and read the module manifest before
	// writing imports. Language-AGNOSTIC framing — Go gotchas in the
	// persona pollute prompts for Python/Node/etc projects, so the
	// guidance names manifests for multiple languages and lets the dev
	// pick the right one.
	wantSiblingReadPins := []string{
		"CREATING A FILE IN AN EXISTING DIRECTORY",
		"head -5", // the bash one-liner that catches it
		"copy that declaration verbatim",
		"go.mod",            // Go manifest mention
		"package.json",      // Node manifest mention
		"pyproject.toml",    // Python manifest mention
		"never a bare path", // the take-23 specific failure shape
	}
	for _, want := range wantSiblingReadPins {
		if !strings.Contains(result.SystemMessage, want) {
			t.Errorf("expected sibling-read guidance %q in role-context (take-23 fix)", want)
		}
	}

	// Anti-pin: the persona must NOT name Go-specific failure modes that
	// don't apply to Python/Node/etc projects. The user (2026-05-08)
	// caught and rejected a Go-specific draft of this fragment that
	// would have polluted every dispatch with internal/auth-style
	// examples regardless of project language.
	wantAbsent := []string{
		"package internal", // would name a Go-specific package error
		"internal/auth",    // would name a Go-specific stdlib reservation
		"is not in std",    // Go compiler error string
	}
	for _, banned := range wantAbsent {
		if strings.Contains(result.SystemMessage, banned) {
			t.Errorf("developer persona contains Go-specific text %q — should be language-agnostic so Python/Node/etc dispatches aren't polluted", banned)
		}
	}

	// Empty WorktreePath → no path banner (graceful fallback).
	if strings.Contains(result.SystemMessage, "Your worktree path:") {
		t.Error("workspace-contract fragment should NOT render path banner when WorktreePath is empty")
	}
}

// TestSoftwareDeveloperAssembly_WorktreePath_BannerRendered pins the
// 2026-05-12 path-confusion fix (A2 from
// .semspec/semspec-plan-2026-05-12.md). When execution-manager threads
// exec.WorktreePath into TaskContext, the workspace-contract fragment
// must lead with an explicit "Your worktree path: X" / "DO NOT cd
// /workspace" banner. Sonnet's hybrid @hard take 16 behavior — cd into
// /workspace and cat-write — needs the explicit warning at the head
// of the contract, not buried in the "If file write seems to have
// succeeded but git status shows nothing..." paragraph downstream.
func TestSoftwareDeveloperAssembly_WorktreePath_BannerRendered(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)
	r.Register(prompt.ToolGuidanceFragment(prompt.DefaultToolGuidance()))

	a := prompt.NewAssembler(r)
	result := a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleDeveloper,
		Provider:       prompt.ProviderAnthropic,
		AvailableTools: []string{"bash", "submit_work"},
		SupportsTools:  true,
		TaskContext: &prompt.TaskContext{
			WorktreePath: "/workspace/.semspec/worktrees/node-abc123/",
		},
	})

	wantPins := []string{
		"Your worktree path: /workspace/.semspec/worktrees/node-abc123/",
		"DO NOT `cd /workspace` to write files",
		"parent fixture root, NOT your worktree",
		"Reading from `/workspace`, `/sources/`",
	}
	for _, want := range wantPins {
		if !strings.Contains(result.SystemMessage, want) {
			t.Errorf("workspace-contract banner missing %q; got system message head:\n%s", want, result.SystemMessage[:min(800, len(result.SystemMessage))])
		}
	}

	// Order check: the path banner must appear BEFORE the "Honest
	// reporting is mandatory" block — that's the whole point of the
	// banner (anchor the model's mental model first, then constraints).
	bannerIdx := strings.Index(result.SystemMessage, "Your worktree path:")
	honestIdx := strings.Index(result.SystemMessage, "Honest reporting is mandatory:")
	if bannerIdx < 0 || honestIdx < 0 {
		t.Fatalf("required markers not both present (banner=%d honest=%d)", bannerIdx, honestIdx)
	}
	if bannerIdx >= honestIdx {
		t.Errorf("path banner must appear BEFORE 'Honest reporting' (banner@%d, honest@%d)", bannerIdx, honestIdx)
	}
}

func TestSoftwarePlannerAssembly(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)

	a := prompt.NewAssembler(r)
	result := a.Assemble(&prompt.AssemblyContext{
		Role:     prompt.RolePlanner,
		Provider: prompt.ProviderOpenAI,
	})

	if !strings.Contains(result.SystemMessage, "planner exploring a problem space") {
		t.Error("expected planner identity")
	}
	if !strings.Contains(result.SystemMessage, "## Identity") {
		t.Error("expected markdown formatting for OpenAI")
	}
	// Should not contain developer-only fragments
	if strings.Contains(result.SystemMessage, "You MUST use bash to create or modify implementation") {
		t.Error("planner should not see builder tool directive")
	}
	// Bug-#1 pin: the planner must be told that scope paths are real
	// filesystem paths, not graph entity IDs. Without this, the planner
	// pastes graph IDs (dashed) into scope.include and every downstream
	// agent routes to a non-existent path. Caught 2026-05-03 on openrouter
	// @easy /health where scope listed "internal-auth/auth.go" instead of
	// "internal/auth/auth.go" and the run burned ~570K tokens producing
	// zero working code.
	if !strings.Contains(result.SystemMessage, "scope paths are filesystem paths, not graph entity IDs") {
		t.Error("expected scope-path-vs-graph-ID rule in planner output-format fragment")
	}
	// Pins the v5 regression fix: planner over-included unrelated files
	// (internal/auth/auth_test.go) when only /health was needed, just because
	// they appeared in the Project Files inventory. The output-format fragment
	// must teach scope = RELEVANCE, not inventory, with both correct and
	// wrong examples for example-anchoring small/mid models.
	if !strings.Contains(result.SystemMessage, "scope is about RELEVANCE, not inventory") {
		t.Error("expected RELEVANCE-not-inventory rule in planner output-format fragment")
	}
	if !strings.Contains(result.SystemMessage, "WRONG scope.include") {
		t.Error("expected WRONG scope.include negative example in planner output-format fragment")
	}
	// scope.create field pin: planner persona must show the create field
	// in the example so the model knows new files go there. v7 escalated
	// because the planner kept putting main_test.go in include and the
	// reviewer flagged it as hallucinated.
	if !strings.Contains(result.SystemMessage, `"create":`) {
		t.Error("expected scope.create field shown in planner output-format example")
	}
	if !strings.Contains(result.SystemMessage, "scope.include is for files that ALREADY EXIST") {
		t.Error("expected explicit include-vs-create distinction rule")
	}
}

func TestSoftwareReviewerAssembly(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)

	a := prompt.NewAssembler(r)
	result := a.Assemble(&prompt.AssemblyContext{
		Role:     prompt.RoleReviewer,
		Provider: prompt.ProviderOllama,
	})

	if !strings.Contains(result.SystemMessage, "code reviewer") {
		t.Error("expected reviewer identity")
	}
	if !strings.Contains(result.SystemMessage, "READ-ONLY access via bash") {
		t.Error("expected read-only notice in reviewer prompt")
	}
	// Bug-#5 pin: reviewer feedback must be scope-aware when rejecting on
	// files_modified mismatch. Bare "no files modified" feedback is
	// non-actionable and produced four identical rejections in a row on
	// the 2026-05-03 openrouter @easy /health run.
	if !strings.Contains(result.SystemMessage, "Scope-Aware Feedback") {
		t.Error("expected 'Scope-Aware Feedback' rule in reviewer role-context")
	}
	if !strings.Contains(result.SystemMessage, "non-actionable") {
		t.Error("expected explicit non-actionable warning in reviewer prompt")
	}
	// Bucket-#4 pin: reviewer output-format MUST show the rejected-verdict
	// JSON shape with rejection_type as a first-class field, not just
	// mention it in prose. Caught 2026-05-03 on openrouter @easy v4 where
	// qwen3-coder-next anchored on the prior 2-key approved-only example
	// and submitted verdict=rejected without rejection_type 35 times in a
	// row, burning the 50-iter cap. The example is the load-bearing piece
	// for example-anchoring small/mid models.
	if !strings.Contains(result.SystemMessage, `"verdict": "rejected"`) {
		t.Error("expected REJECTED JSON example in reviewer output-format fragment")
	}
	if !strings.Contains(result.SystemMessage, `"rejection_type": "fixable"`) {
		t.Error("expected rejection_type shown as a populated field in REJECTED example")
	}
}

func TestSoftwarePlanReviewerAssembly(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)

	a := prompt.NewAssembler(r)
	result := a.Assemble(&prompt.AssemblyContext{
		Role:     prompt.RolePlanReviewer,
		Provider: prompt.ProviderAnthropic,
	})

	if !strings.Contains(result.SystemMessage, "plan reviewer") {
		t.Error("expected plan reviewer identity")
	}
	if !strings.Contains(result.SystemMessage, "needs_changes") {
		t.Error("expected verdict criteria in plan reviewer prompt")
	}
	// scope.create awareness pin: reviewer must NOT flag scope.create
	// entries as hallucinated paths. Caught 2026-05-03 v7 where the
	// reviewer rejected main_test.go three times and even hallucinated
	// a scope.create field by name in the suggestion before it was a
	// real field.
	if !strings.Contains(result.SystemMessage, "Files in scope.create are explicit creation-intent") {
		t.Error("expected reviewer awareness of scope.create field")
	}
	// Pins the bug-#2 leverage-point fix from the 2026-05-03 openrouter @easy run:
	// reviewer must encode plan defects as findings, not only in summary.
	// Drop this rule and the verdict normalization stops being self-consistent.
	if !strings.Contains(result.SystemMessage, "findings drive the verdict") {
		t.Error("expected explicit 'findings drive the verdict' rule in plan-reviewer output-format fragment")
	}
	if !strings.Contains(result.SystemMessage, `severity="error"`) {
		t.Error("expected explicit severity=error guidance for plan defects")
	}
}

// TestSoftwareGapDetectionRemoved verifies gap detection is NOT in prompts
// (removed — Q&A system handles questions via ask_question tool).
func TestSoftwareGapDetectionRemoved(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)

	a := prompt.NewAssembler(r)
	result := a.Assemble(&prompt.AssemblyContext{Role: prompt.RolePlanner})

	if strings.Contains(result.SystemMessage, "Knowledge Gaps") {
		t.Error("gap detection should NOT be in prompts (removed)")
	}
}

func TestSoftwareSubmitWorkDirective(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)

	a := prompt.NewAssembler(r)

	// Non-developer roles should get the shared submit_work directive.
	for _, role := range []prompt.Role{
		prompt.RolePlanReviewer,
		prompt.RoleReviewer,
		prompt.RoleArchitect,
		prompt.RoleRequirementGenerator,
		prompt.RoleScenarioGenerator,
	} {
		result := a.Assemble(&prompt.AssemblyContext{Role: role, Provider: prompt.ProviderOllama})
		if !strings.Contains(result.SystemMessage, "MUST call the submit_work function") {
			t.Errorf("role %s should have submit_work directive", role)
		}
	}

	// Developer has its own tool directive — should NOT get the shared one.
	result := a.Assemble(&prompt.AssemblyContext{Role: prompt.RoleDeveloper, Provider: prompt.ProviderOllama})
	if strings.Contains(result.SystemMessage, "MUST call the submit_work function") {
		t.Error("developer should not get shared submit_work directive (has its own)")
	}
}

// TestSoftwareOrientationGraphFirst pins the dial-#6 fix: the orientation
// fragment must hard-direct agents to graph_summary when graph tools are
// available (the 2026-04-28 Gemini @easy run made 0 graph_* calls because
// the prompt said "graph_summary OR a few bash commands"). Personas without
// graph_summary in their allowlist get the legacy bash-only orientation.
func TestSoftwareOrientationGraphFirst(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)
	a := prompt.NewAssembler(r)

	withGraph := a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleDeveloper,
		Provider:       prompt.ProviderOpenAI,
		AvailableTools: []string{"bash", "submit_work", "graph_summary", "graph_search", "graph_query"},
	})
	// Reasons-not-rules orientation: explain why graph indexing matters and
	// distinguish entity IDs from filesystem paths. The earlier "Iteration 1
	// MUST call graph_summary" framing produced cargo-cult behavior on small
	// models (architect at qwen3:14b@temp0.6 passed entity IDs to bash as
	// paths four iterations in a row, never recovered). Reasons let small
	// models pick the right tool from understanding instead of compliance.
	if !strings.Contains(withGraph.SystemMessage, "Semantic Knowledge Graph") {
		t.Error("orientation must establish that the graph is an SKG of THIS workspace (not generic)")
	}
	if !strings.Contains(withGraph.SystemMessage, "semsource") {
		t.Error("orientation must name semsource as the curator so agents know the graph is live and authoritative")
	}
	if !strings.Contains(withGraph.SystemMessage, "shared, durable memory") {
		t.Error("orientation must establish the graph as cross-agent shared memory — that's the why-they-should-care")
	}
	if !strings.Contains(withGraph.SystemMessage, "graph indexes the workspace") {
		t.Error("graph-equipped persona should explain WHY the graph matters (indexing), not prescribe MUST")
	}
	if !strings.Contains(withGraph.SystemMessage, "Entity IDs are graph keys") {
		t.Error("orientation must distinguish entity IDs from filesystem paths to prevent cargo-culting IDs into bash args")
	}
	if strings.Contains(withGraph.SystemMessage, "graph_summary or a few bash commands") {
		t.Error("the soft 'or a few bash commands' phrasing must not survive — that's exactly what produced the 0-graph-call run")
	}
	if strings.Contains(withGraph.SystemMessage, "Iteration 1 MUST call graph_summary") {
		t.Error("the prescriptive 'MUST call' framing is the Goodhart trap that produced entity-ID-as-bash-path cargo-culting")
	}

	withoutGraph := a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleReviewer,
		Provider:       prompt.ProviderOpenAI,
		AvailableTools: []string{"bash", "submit_work"},
	})
	if strings.Contains(withoutGraph.SystemMessage, "graph_summary") {
		t.Error("personas without graph_summary in their allowlist must not be told to call it")
	}
	if !strings.Contains(withoutGraph.SystemMessage, "Orient yourself briefly") {
		t.Error("non-graph personas should still get bash-orientation guidance")
	}
}

// TestSoftwareGraphErrorEscapeHatches pins the take-30 fix: graph-equipped
// personas must see explicit guidance to NARROW (not retry) on
// response-too-large errors, FALL BACK on empty graph_search results, and
// INTROSPECT (not guess) on graph_query syntax errors. Take 30 wedged
// because qwen-thinking re-issued the same broad graph_search query 3+
// times after each "response too large (102401 bytes)" error until the
// RepeatToolFailure detector tripped — the inline error message named the
// fix but the model didn't act on it, so the guidance is pinned in the
// persona where the model is anchored. Also asserts the bash-fallback
// directive ("two failed graph calls of the same shape") so the agent
// switches tools instead of looping.
func TestSoftwareGraphErrorEscapeHatches(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)
	a := prompt.NewAssembler(r)

	withGraph := a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleDeveloper,
		Provider:       prompt.ProviderOpenAI,
		AvailableTools: []string{"bash", "submit_work", "graph_summary", "graph_search", "graph_query"},
	})

	mustContain := []string{
		"response too large",
		"Narrow it",
		"entity_id",
		"hop count",
		"Two failed graph calls of the same shape",
		"switch tools",
		"introspect:true",
	}
	for _, want := range mustContain {
		if !strings.Contains(withGraph.SystemMessage, want) {
			t.Errorf("graph-error escape hatches missing %q from developer persona", want)
		}
	}

	// Personas without graph tools must NOT receive the graph-error stanza
	// — it would be dead weight and could confuse small models that don't
	// have the tools the guidance references.
	withoutGraph := a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleReviewer,
		Provider:       prompt.ProviderOpenAI,
		AvailableTools: []string{"bash", "submit_work"},
	})
	mustNotContain := []string{
		"response too large",
		"Two failed graph calls",
	}
	for _, want := range mustNotContain {
		if strings.Contains(withoutGraph.SystemMessage, want) {
			t.Errorf("non-graph persona must not receive graph-error escape hatches; found %q", want)
		}
	}
}

// TestSoftwareGraphResultOrientation pins the take-33 fix (gemini @hard
// 2026-05-10): graph-equipped personas must see the world-model framing for
// graph search results — entities tagged [project]/[dependency]/[doc] are
// indexed facts from real source repos, not strings; an empty result is
// itself signal about what the indexed repos do or don't reference. The
// goal is to enrich the agent's reasoning about graph evidence vs its
// training prior, NOT to direct procedural behavior ("if X then do Y" was
// rejected as crimping reasoning). Take 33 burned 5 TDD cycles fabricating
// fictional Maven coords ("org.opensensorhub:opensensorhub-core:0.2.0-SNAPSHOT")
// after graph_search surfaced the correct hint "org.sensorhub [project]"
// and the agent ignored it.
func TestSoftwareGraphResultOrientation(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)
	a := prompt.NewAssembler(r)

	withGraph := a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleDeveloper,
		Provider:       prompt.ProviderOpenAI,
		AvailableTools: []string{"bash", "submit_work", "graph_summary", "graph_search", "graph_query"},
	})

	mustContain := []string{
		"Indexed graph entities",
		"[project]",
		"[dependency]",
		"[doc]",
		"facts at index time",
		"aren't in pretraining",
		"Silence is also signal",
	}
	for _, want := range mustContain {
		if !strings.Contains(withGraph.SystemMessage, want) {
			t.Errorf("graph-results orientation missing %q from developer persona", want)
		}
	}

	// Goodhart guard: this orientation must NOT carry procedural directives
	// that would crimp the agent's reasoning. If a future edit reintroduces
	// MUST/before-X-do-Y framing, this test fails so the regression is
	// surfaced at PR time. The whole point of this fragment is enrichment,
	// not compliance.
	mustNotContain := []string{
		"You MUST call graph_query",
		"Before writing external coordinates",
		"Before you write coordinates",
	}
	for _, banned := range mustNotContain {
		if strings.Contains(withGraph.SystemMessage, banned) {
			t.Errorf("graph-results orientation reintroduced procedural directive %q (Goodhart guard)", banned)
		}
	}

	// Personas without graph tools must NOT receive this stanza — it
	// references tools they don't have and would be dead weight.
	withoutGraph := a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleReviewer,
		Provider:       prompt.ProviderOpenAI,
		AvailableTools: []string{"bash", "submit_work"},
	})
	if strings.Contains(withoutGraph.SystemMessage, "Indexed graph entities") {
		t.Errorf("non-graph persona must not receive graph-results orientation")
	}
}

// TestSoftwareUpstreamSourcesOrientation pins the bash-on-/sources fragment
// (gemini @hard 2026-05-10 take 1, req.3 Maven coord fabrication). semsource
// indexes Java AST + markdown but not pom.xml as queryable triples; mounting
// the semsource clone tree read-only into the sandbox at /sources/<namespace>/
// closes the gap. World-model framing only — the fragment must teach the
// agent that two lenses exist (graph for structure, bash for file contents)
// without prescribing a procedural "if X then Y" lookup that crimps judgment.
func TestSoftwareUpstreamSourcesOrientation(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)
	a := prompt.NewAssembler(r)

	withGraphAndBash := a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleDeveloper,
		Provider:       prompt.ProviderOpenAI,
		AvailableTools: []string{"bash", "submit_work", "graph_summary", "graph_search", "graph_query"},
	})

	mustContain := []string{
		"/sources/<namespace>/",
		"semsource",
		"read-only",
		"pom.xml",
		"AST/docs lens drops",
		"reference material",
		"Don't copy whole directories",
	}
	for _, want := range mustContain {
		if !strings.Contains(withGraphAndBash.SystemMessage, want) {
			t.Errorf("upstream-sources orientation missing %q from developer persona", want)
		}
	}

	// Goodhart guard: world-model framing only. Procedural "before X do Y"
	// patterns would crimp the agent's reasoning across contexts. Asserts
	// against the fragment content directly so legitimate uses of similar
	// phrases in other fragments don't false-positive.
	var fragment string
	for _, f := range Software() {
		if f.ID == "software.orientation.upstream-sources" {
			fragment = f.Content
			break
		}
	}
	if fragment == "" {
		t.Fatal("software.orientation.upstream-sources fragment not found")
	}
	mustNotContain := []string{
		"You MUST",
		"you must",
		"Before writing",
		"Before adding",
		"Always cat",
	}
	for _, banned := range mustNotContain {
		if strings.Contains(fragment, banned) {
			t.Errorf("upstream-sources fragment reintroduced procedural directive %q (Goodhart guard)", banned)
		}
	}

	// Personas without bash OR without graph tools should not receive this
	// stanza — it references both lenses; missing either makes the framing
	// dead weight.
	noGraph := a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleDeveloper,
		Provider:       prompt.ProviderOpenAI,
		AvailableTools: []string{"bash", "submit_work"},
	})
	if strings.Contains(noGraph.SystemMessage, "/sources/<namespace>/") {
		t.Errorf("persona without graph tools must not receive upstream-sources orientation")
	}
	noBash := a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleReviewer,
		Provider:       prompt.ProviderOpenAI,
		AvailableTools: []string{"submit_work", "graph_summary"},
	})
	if strings.Contains(noBash.SystemMessage, "/sources/<namespace>/") {
		t.Errorf("persona without bash must not receive upstream-sources orientation")
	}
}

// TestSoftwareUrlGuessingOrientation pins the take-5 fix: an agent that
// has both http_request and web_search must see the orientation framing
// them as complementary tools (web_search discovers URLs, http_request
// fetches from known URLs). Take 5's wedge was the agent probing Maven
// repository URLs with constructed guesses (dead-Bintray pattern) and
// hitting 3+ HTTP 404s — tool-error-loop detected the repeat pattern
// but didn't point at the recovery (web_search). World-model framing
// only — Goodhart guard.
func TestSoftwareUrlGuessingOrientation(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)
	a := prompt.NewAssembler(r)

	withBoth := a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleDeveloper,
		Provider:       prompt.ProviderOpenAI,
		AvailableTools: []string{"bash", "submit_work", "http_request", "web_search", "graph_search", "graph_query", "graph_summary"},
	})

	mustContain := []string{
		"URL discovery vs URL fetching",
		"web_search is for finding URLs you don't already know",
		"http_request is for fetching from URLs you do know",
		"the URL was a guess",
		"web_search produces the actual current URL",
	}
	for _, want := range mustContain {
		if !strings.Contains(withBoth.SystemMessage, want) {
			t.Errorf("url-guessing orientation missing %q from developer persona", want)
		}
	}

	// Goodhart guard against procedural directives. Asserts against the
	// fragment content directly so legitimate uses of similar phrases
	// elsewhere don't false-positive.
	var fragment string
	for _, f := range Software() {
		if f.ID == "software.orientation.url-guessing" {
			fragment = f.Content
			break
		}
	}
	if fragment == "" {
		t.Fatal("software.orientation.url-guessing fragment not found")
	}
	mustNotContain := []string{
		"You MUST",
		"you must",
		"Always web_search",
		"Before http_request",
		"must immediately",
	}
	for _, banned := range mustNotContain {
		if strings.Contains(fragment, banned) {
			t.Errorf("url-guessing fragment reintroduced procedural directive %q (Goodhart guard)", banned)
		}
	}

	// Scope: agent missing either http_request OR web_search must NOT
	// receive the orientation — pairs of tools the agent doesn't have
	// would be confusing dead weight.
	noHTTP := a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleDeveloper,
		Provider:       prompt.ProviderOpenAI,
		AvailableTools: []string{"bash", "submit_work", "web_search", "graph_summary"},
	})
	if strings.Contains(noHTTP.SystemMessage, "URL discovery vs URL fetching") {
		t.Errorf("agent without http_request must not receive url-guessing orientation")
	}
	noWebSearch := a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleDeveloper,
		Provider:       prompt.ProviderOpenAI,
		AvailableTools: []string{"bash", "submit_work", "http_request", "graph_summary"},
	})
	if strings.Contains(noWebSearch.SystemMessage, "URL discovery vs URL fetching") {
		t.Errorf("agent without web_search must not receive url-guessing orientation")
	}
}

// TestSoftwareToolErrorLoopEscapeHatch pins the take-1 fix (gemini @hard
// 2026-05-10 req.5): the developer agent burned all 50 iterations in a tight
// bash-mvn loop, never calling submit_work to surface the obstacle. Mirrors
// the shape of TestSoftwareGraphErrorEscapeHatches: world-model framing
// (repeated failures = structural obstacle, submit_work is always available,
// iteration budget is finite) anchored in the persona. The Goodhart guard
// fails if a future edit reintroduces procedural directives like "MUST call
// submit_work after 3 failures" — those crimp judgment in different contexts
// and the whole point of the fragment is enrichment, not compliance.
func TestSoftwareToolErrorLoopEscapeHatch(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)
	a := prompt.NewAssembler(r)

	withTools := a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleDeveloper,
		Provider:       prompt.ProviderOpenAI,
		AvailableTools: []string{"bash", "submit_work", "graph_summary", "graph_search", "graph_query"},
	})

	mustContain := []string{
		"three or more times",
		"structural",
		"submit_work is the escape",
		"obstacle summary",
		"iteration budget",
		"diagnostic",
		// Generalisation markers (take 5 2026-05-10): the fragment must
		// frame the wedge as tool-agnostic, not bash-specific. Take 5
		// surfaced an http_request 404-chase wedge that the bash-only
		// trigger missed entirely. If a future refactor narrows the
		// fragment back to bash, these assertions catch it.
		"any tool",
		"HTTP non-2xx",
		"across any tool",
	}
	for _, want := range mustContain {
		if !strings.Contains(withTools.SystemMessage, want) {
			t.Errorf("tool-error-loop escape hatch missing %q from developer persona", want)
		}
	}

	// Goodhart guard: this orientation must NOT carry procedural directives
	// that would crimp the agent's reasoning. World-model framing only — if
	// a future edit reintroduces MUST/before-X-do-Y shapes, this test fails
	// so the regression is surfaced at PR time. Asserts against the fragment
	// content directly (not the full system message) because legitimate
	// terminal directives in other fragments use "You MUST call submit_work
	// when your task is complete" — that's a different concept (terminal
	// completion, not loop-escape).
	var loopFragment string
	for _, f := range Software() {
		if f.ID == "software.orientation.tool-error-loop" {
			loopFragment = f.Content
			break
		}
	}
	if loopFragment == "" {
		t.Fatal("software.orientation.tool-error-loop fragment not found")
	}
	mustNotContain := []string{
		"You MUST",
		"you must",
		"After 3 failures",
		"Before retrying",
		"must immediately",
	}
	for _, banned := range mustNotContain {
		if strings.Contains(loopFragment, banned) {
			t.Errorf("tool-error-loop fragment reintroduced procedural directive %q (Goodhart guard)", banned)
		}
	}

	// Reviewer persona also has bash + submit_work, so the fragment SHOULD
	// reach reviewers — the wedge can recur in any TDD-loop role.
	reviewer := a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleReviewer,
		Provider:       prompt.ProviderOpenAI,
		AvailableTools: []string{"bash", "submit_work"},
	})
	if !strings.Contains(reviewer.SystemMessage, "submit_work is the escape") {
		t.Errorf("reviewer persona with bash+submit_work must receive tool-error-loop orientation")
	}

	// Generalised trigger (2026-05-10): the fragment must reach any agent
	// with submit_work, regardless of whether bash is in the toolset.
	// Verifies the trigger broadened correctly from
	//   HasTool("bash") && HasTool("submit_work")
	// to just
	//   HasTool("submit_work")
	//
	// The {http_request, graph_query, submit_work} combination below is
	// synthetic — every role in tool_filter.DefaultToolFilters() currently
	// includes bash, so no real production agent has this exact toolset.
	// The role label is incidental; the assembler keys off AvailableTools.
	// We construct a no-bash set explicitly to assert the trigger fires
	// without bash present (which is the take-5 http_request-only wedge
	// surface generalised — a future answerer/researcher-class agent
	// without bash would benefit from the same orientation).
	noBashWithSubmit := a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RolePlanner,
		Provider:       prompt.ProviderOpenAI,
		AvailableTools: []string{"http_request", "graph_query", "submit_work"},
	})
	if !strings.Contains(noBashWithSubmit.SystemMessage, "submit_work is the escape") {
		t.Errorf("agent with submit_work but no bash must receive tool-error-loop orientation (generalised trigger)")
	}

	// A persona without submit_work (e.g. a read-only role) must NOT receive
	// this stanza — it would reference an escape the agent doesn't have.
	noSubmit := a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleDeveloper,
		Provider:       prompt.ProviderOpenAI,
		AvailableTools: []string{"bash"},
	})
	if strings.Contains(noSubmit.SystemMessage, "submit_work is the escape") {
		t.Errorf("persona without submit_work must not receive tool-error-loop orientation")
	}
}

// TestSoftwareRequirementGeneratorFilesOwned pins the dial-#1 prompt landing
// in the right persona. The first attempt edited workflow/prompts/
// requirement_generator.go which was dead code and never reached Gemini —
// the live persona is in this domain registry. This test fails if a future
// refactor drops the files_owned guidance.
func TestSoftwareRequirementGeneratorFilesOwned(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)
	a := prompt.NewAssembler(r)

	result := a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleRequirementGenerator,
		Provider:       prompt.ProviderOpenAI,
		AvailableTools: []string{"submit_work", "graph_summary"},
	})

	mustContain := []string{
		"files_owned",
		"depends_on",
		"Partition files across requirements",
		"merge fails",
		"prefer ONE requirement that owns BOTH",
		// Fan-in guidance for shared registration files (main.go, app.tsx, etc.).
		// Without it, qwen3-class models put main.go in every requirement's
		// files_owned in parallel and burn the retry budget on the same mistake.
		"Shared registration files",
		"fan-in",
		"final \"wire-up\" requirement",
		// 3-req example in the output-format fragment must mention the pattern.
		"fan-in pattern",
		"feature requirements DO NOT list main.go",
		// First-conflict-only caveat lets retries see the whole partition,
		// not just the validator's first complaint.
		"only reports the FIRST conflicting pair",
		// 2026-05-04 anti-example for impl + test on the same surface
		// — qwen3-moe regenerated this exact shape after the
		// 2026-05-02 fan-in fix. Worked-example anchor stops the
		// reasoning-from-scratch loop on every retry.
		"ANTI-EXAMPLE",
		"Splitting \"implement\" from \"test\"",
		"Option (a), consolidate",
		"Option (b), depends_on",
		// 2026-05-08 take-23 fix: req-gen had been told only
		// "drawn from the plan's scope.include" — completely ignored
		// scope.create. Result: planner said create internal/health/,
		// req-gen put internal/auth/* into files_owned, dev wrote in
		// the wrong dir for 5 cycles. Persona now must explicitly name
		// BOTH buckets and call out the take-23 failure mode by name.
		"files_owned is drawn from BOTH scope.include AND scope.create",
		"existing files the requirement may MODIFY",
		"new files the plan intends to ADD",
		// Worked example must demonstrate the rule it's teaching:
		// scope.create paths flowing into files_owned alongside
		// scope.include paths.
		"Plan it's working from",
		"scope.create",
	}
	for _, want := range mustContain {
		if !strings.Contains(result.SystemMessage, want) {
			t.Errorf("requirement-generator persona missing %q — dial #1 prompt did not land", want)
		}
	}
}

func TestSoftwareOllamaProviderHint(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)

	a := prompt.NewAssembler(r)

	// Ollama provider should get the Ollama-specific tool enforcement.
	result := a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleReviewer,
		Provider:       prompt.ProviderOllama,
		AvailableTools: []string{"bash", "submit_work"},
	})
	if !strings.Contains(result.SystemMessage, "function-calling tools available") {
		t.Error("Ollama reviewer should get ollama-tool-enforcement hint")
	}

	// Non-Ollama provider should not get it.
	result = a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleReviewer,
		Provider:       prompt.ProviderAnthropic,
		AvailableTools: []string{"bash", "submit_work"},
	})
	if strings.Contains(result.SystemMessage, "function-calling tools available") {
		t.Error("Anthropic reviewer should not get ollama-tool-enforcement hint")
	}
}

// TestSoftwareUserPromptCoverage asserts that every role whose component
// dispatches via the prompt registry (assembled.UserMessage) has a
// CategoryUserPrompt fragment registered. Adding a new component that uses the
// registry path without a user-prompt fragment would otherwise only fail at
// runtime with an empty user message.
func TestSoftwareUserPromptCoverage(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)

	rolesNeedingUserPrompt := []prompt.Role{
		prompt.RolePlanner,
		prompt.RoleRequirementGenerator,
		prompt.RoleScenarioGenerator,
		prompt.RoleArchitect,
		prompt.RolePlanReviewer,
		prompt.RolePlanQAReviewer,
	}

	for _, role := range rolesNeedingUserPrompt {
		if r.UserPromptFragmentFor(role) == nil {
			t.Errorf("role %q has no CategoryUserPrompt fragment registered — components dispatching this role would emit an empty user message", role)
		}
	}
}

func TestSoftwareRetryFragment(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)

	a := prompt.NewAssembler(r)

	// Without feedback — retry directive should not appear
	result := a.Assemble(&prompt.AssemblyContext{
		Role: prompt.RoleDeveloper,
	})
	if strings.Contains(result.SystemMessage, "Previous Feedback") {
		t.Error("retry directive should not appear without feedback")
	}

	// With feedback — retry directive should appear
	result = a.Assemble(&prompt.AssemblyContext{
		Role: prompt.RoleDeveloper,
		TaskContext: &prompt.TaskContext{
			Feedback: "Missing error handling in auth middleware",
		},
	})
	if !strings.Contains(result.SystemMessage, "Missing error handling in auth middleware") {
		t.Error("expected feedback content in retry prompt")
	}
}

// TestSoftwareReviewerRetryFragment pins the prior-cycle awareness fix that
// closes the reviewer-flip-flop wedge from gemini @hard 2026-05-10 take 2.
// Without this fragment, cycle-N reviewer evaluates the developer's
// submission as a fresh review and can flip rejection_type=fixable on
// cycle N-1 into rejection_type=restructure on cycle N — restructure
// escalates IMMEDIATELY and bypasses the remaining TDD budget. The
// developer dutifully addressed the prior cycle's feedback and the system
// has no way to win against a reviewer that changes its mind.
//
// Symmetric to TestSoftwareRetryFragment (developer side) and the
// plan-reviewer prior-round fix in commit ee9972e.
func TestSoftwareReviewerRetryFragment(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)
	a := prompt.NewAssembler(r)

	// Cycle 0 / no retry — the fragment must NOT fire. Cycle 0 has no prior
	// feedback to reference, and rendering this stanza on a fresh review
	// would confuse the model with an empty Feedback section.
	cycle0 := a.Assemble(&prompt.AssemblyContext{
		Role:     prompt.RoleReviewer,
		Provider: prompt.ProviderOpenAI,
		TaskContext: &prompt.TaskContext{
			IsRetry:       false,
			Feedback:      "",
			Iteration:     1,
			MaxIterations: 5,
		},
	})
	if strings.Contains(cycle0.SystemMessage, "PRIOR REVIEW CONTEXT") {
		t.Error("reviewer-retry directive must NOT appear on cycle 0 (fresh review)")
	}

	// Cycle 1+ with prior feedback — the fragment must fire, surface the
	// prior cycle's feedback verbatim, and warn against flip-flopping
	// verdict types.
	priorFeedback := "Tests for read() are missing the timeout-zero branch."
	cycle1 := a.Assemble(&prompt.AssemblyContext{
		Role:     prompt.RoleReviewer,
		Provider: prompt.ProviderOpenAI,
		TaskContext: &prompt.TaskContext{
			IsRetry:       true,
			Feedback:      priorFeedback,
			Iteration:     2,
			MaxIterations: 5,
		},
	})

	mustContain := []string{
		// Frames the prompt as a retry, not a fresh review.
		"PRIOR REVIEW CONTEXT",
		"retry",
		// Renders the prior feedback verbatim so reviewer can see what
		// the developer was asked to address.
		priorFeedback,
		// Names the flip-flop wedge explicitly. Cycle-0 fixable →
		// cycle-1 restructure is the exact shape that wedged take 2.
		"restructure",
		"flip-flop",
		// The escalation cost — reviewer must know restructure
		// bypasses the remaining TDD budget.
		"bypasses the remaining TDD",
		// The corrective path when the reviewer realises their prior
		// call was wrong. Saying so transparently with fixable+corrected
		// guidance is the right shape; flipping to restructure is not.
		"fixable",
		"transparently",
	}
	for _, want := range mustContain {
		if !strings.Contains(cycle1.SystemMessage, want) {
			t.Errorf("reviewer-retry directive missing %q on cycle 1\nfull system message:\n%s",
				want, cycle1.SystemMessage)
		}
	}

	// Goodhart guards: this fragment must NOT push the reviewer into
	// over-correction. If a future edit weakens reviewer judgment with
	// "always approve" language, this test catches it at PR time — the
	// goal is to prevent FLIP-FLOPPING, not to neutralise the reviewer.
	// Assert against the fragment content directly so legitimate
	// "MUST submit_work" terminal directives in other fragments don't
	// false-positive.
	var fragContent string
	for _, f := range Software() {
		if f.ID == "software.reviewer.retry-directive" {
			if f.ContentFunc == nil {
				t.Fatal("software.reviewer.retry-directive is content-only; expected ContentFunc for Feedback templating")
			}
			fragContent = f.ContentFunc(&prompt.AssemblyContext{
				TaskContext: &prompt.TaskContext{
					IsRetry:       true,
					Feedback:      priorFeedback,
					Iteration:     2,
					MaxIterations: 5,
				},
			})
			break
		}
	}
	if fragContent == "" {
		t.Fatal("software.reviewer.retry-directive fragment not found")
	}
	mustNotContain := []string{
		"always approve",
		"You MUST approve",
		"you must approve",
		"never reject",
		"do not reject",
	}
	for _, banned := range mustNotContain {
		if strings.Contains(fragContent, banned) {
			t.Errorf("reviewer-retry fragment carries over-correction language %q (Goodhart guard — fragment must prevent flip-flop, not neutralise judgment)",
				banned)
		}
	}

	// Cycle-1 retry with EMPTY prior feedback — the fragment must NOT fire.
	// IsRetry true with Feedback empty means the prior cycle wasn't a
	// review rejection (e.g. parse retry, infrastructure retry); rendering
	// "you reviewed this last cycle and rejected with this feedback:"
	// followed by nothing would be worse than nothing.
	cycle1Empty := a.Assemble(&prompt.AssemblyContext{
		Role:     prompt.RoleReviewer,
		Provider: prompt.ProviderOpenAI,
		TaskContext: &prompt.TaskContext{
			IsRetry:       true,
			Feedback:      "",
			Iteration:     2,
			MaxIterations: 5,
		},
	})
	if strings.Contains(cycle1Empty.SystemMessage, "PRIOR REVIEW CONTEXT") {
		t.Error("reviewer-retry directive must NOT appear when prior feedback is empty (would render a malformed stanza)")
	}

	// Cross-role check: this fragment is specifically for the TDD
	// code-reviewer (RoleReviewer). plan-reviewer + scenario-reviewer
	// have their own prior-round rendering (writePlanReviewerPriorRound
	// + ScenarioReviewContext.RetryFeedback). If a future refactor
	// accidentally bleeds this fragment to those roles, they'd carry
	// duplicate prior-feedback stanzas — fix that here instead.
	planReviewer := a.Assemble(&prompt.AssemblyContext{
		Role:     prompt.RolePlanReviewer,
		Provider: prompt.ProviderOpenAI,
		TaskContext: &prompt.TaskContext{
			IsRetry:       true,
			Feedback:      priorFeedback,
			Iteration:     2,
			MaxIterations: 5,
		},
	})
	if strings.Contains(planReviewer.SystemMessage, "PRIOR REVIEW CONTEXT") {
		t.Error("plan-reviewer must use its own prior-round fragment, not the code-reviewer retry-directive")
	}
}
