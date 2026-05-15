package domain

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/prompt"
)

// TestRenderPlannerPrompt_ProjectFileTreeInjection pins the bug-#7 fix from
// the 2026-05-03 /health postmortem: when a project file tree is provided,
// it MUST appear at the top of the user prompt with the grounding rule, on
// both fresh and revision paths. Greenfield-safe: empty tree is silently
// omitted.
func TestRenderPlannerPrompt_ProjectFileTreeInjection(t *testing.T) {
	tests := []struct {
		name        string
		ctx         *prompt.PlannerPromptContext
		mustContain []string
		mustNotHave []string
	}{
		{
			name: "fresh creation with tree",
			ctx: &prompt.PlannerPromptContext{
				Title:           "Add /health endpoint",
				ProjectFileTree: "main.go\ngo.mod\ninternal/auth/auth.go",
			},
			mustContain: []string{
				"## Project Files",
				"git ls-files",
				"main.go\ngo.mod\ninternal/auth/auth.go",
				"Hallucinating directories",
				"**Title:** Add /health endpoint",
			},
		},
		{
			name: "revision with tree",
			ctx: &prompt.PlannerPromptContext{
				IsRevision:       true,
				PreviousPlanJSON: `{"goal":"X"}`,
				RevisionPrompt:   "## REVISION REQUEST\n\nFix scope.",
				ProjectFileTree:  "main.go\ngo.mod",
			},
			mustContain: []string{
				"## Project Files",
				"main.go\ngo.mod",
				"## Your Previous Plan Output",
				"## REVISION REQUEST",
			},
		},
		{
			name: "fresh creation without tree (greenfield)",
			ctx: &prompt.PlannerPromptContext{
				Title:           "Bootstrap new service",
				ProjectFileTree: "",
			},
			mustContain: []string{
				"**Title:** Bootstrap new service",
			},
			mustNotHave: []string{
				"## Project Files",
				"git ls-files",
			},
		},
		{
			name: "revision without tree",
			ctx: &prompt.PlannerPromptContext{
				IsRevision:     true,
				RevisionPrompt: "## REVISION REQUEST\n\nFix scope.",
			},
			mustContain: []string{
				"## REVISION REQUEST",
			},
			mustNotHave: []string{
				"## Project Files",
			},
		},
		{
			name: "tree appears BEFORE title (so model reads grounding first)",
			ctx: &prompt.PlannerPromptContext{
				Title:           "Add /health",
				ProjectFileTree: "main.go",
			},
			mustContain: []string{},
			// Order assertion below.
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderPlannerPrompt(tt.ctx)
			for _, want := range tt.mustContain {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q\n--- got ---\n%s", want, got)
				}
			}
			for _, banned := range tt.mustNotHave {
				if strings.Contains(got, banned) {
					t.Errorf("output should NOT contain %q\n--- got ---\n%s", banned, got)
				}
			}
		})
	}

	// Order check: tree must precede the title section.
	t.Run("tree precedes title", func(t *testing.T) {
		got := renderPlannerPrompt(&prompt.PlannerPromptContext{
			Title:           "Add /health",
			ProjectFileTree: "main.go",
		})
		treeIdx := strings.Index(got, "## Project Files")
		titleIdx := strings.Index(got, "**Title:**")
		if treeIdx < 0 || titleIdx < 0 || treeIdx > titleIdx {
			t.Errorf("tree (%d) must precede title (%d)\n--- got ---\n%s", treeIdx, titleIdx, got)
		}
	})
}

// TestRenderPlannerPrompt_RevisionRepeatsScopeSchema pins the v8 fix:
// the revision-flow user prompt must repeat the scope.include vs
// scope.create rule and explicitly warn against panic-dumping the
// project tree into scope.include. Caught 2026-05-03 v8 where the
// planner couldn't adopt scope.create across three revisions and
// eventually dumped every visible path into scope.include.
func TestRenderPlannerPrompt_RevisionRepeatsScopeSchema(t *testing.T) {
	got := renderPlannerPrompt(&prompt.PlannerPromptContext{
		IsRevision:       true,
		PreviousPlanJSON: `{"goal":"X","scope":{"include":["main.go"]}}`,
		RevisionPrompt:   "## REVISION REQUEST\n\nFindings:\n- missing test file in scope",
	})
	mustContain := []string{
		"Scope Schema Reminder",
		"scope.create",
		"NEVER in scope.include",
		"Do NOT enlarge scope to satisfy unrelated criticism",
	}
	for _, s := range mustContain {
		if !strings.Contains(got, s) {
			t.Errorf("revision prompt missing %q\n--- got ---\n%s", s, got)
		}
	}
}

func TestRenderRequirementGeneratorPrompt_FreshGeneration(t *testing.T) {
	got := renderRequirementGeneratorPrompt(&prompt.RequirementGeneratorContext{
		Title:           "Add /goodbye endpoint",
		Goal:            "Implement a /goodbye HTTP endpoint that returns JSON.",
		Context:         "The service currently has /hello but no /goodbye.",
		ScopeInclude:    []string{"api/app.py", "api/test_app.py"},
		ScopeExclude:    []string{"docs/"},
		ScopeDoNotTouch: []string{"README.md"},
	})

	mustContain := []string{
		"## Plan to Decompose",
		"**Title**: Add /goodbye endpoint",
		"**Goal**: Implement a /goodbye HTTP endpoint",
		"**Context**: The service currently has /hello",
		"**Scope Include**: api/app.py, api/test_app.py",
		"**Scope Exclude**: docs/",
		"**Do Not Touch**: README.md",
		"Extract testable requirements from the above plan",
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Errorf("rendered prompt missing %q\n--- prompt ---\n%s", want, got)
		}
	}

	mustNotContain := []string{
		"Existing Approved Requirements",
		"Rejected Requirements",
		"Previous Attempt Failed",
		"Previous Review Findings",
	}
	for _, dont := range mustNotContain {
		if strings.Contains(got, dont) {
			t.Errorf("fresh-generation prompt should not contain %q\n--- prompt ---\n%s", dont, got)
		}
	}
}

func TestRenderRequirementGeneratorPrompt_PartialRegen(t *testing.T) {
	got := renderRequirementGeneratorPrompt(&prompt.RequirementGeneratorContext{
		Title: "Add /goodbye endpoint",
		Goal:  "Implement a /goodbye HTTP endpoint that returns JSON.",
		ExistingRequirements: []prompt.ExistingRequirementSummary{
			{
				ID:         "requirement.foo.1",
				Title:      "Goodbye endpoint returns JSON",
				Status:     "active",
				FilesOwned: []string{"api/handlers/goodbye.go"},
				DependsOn:  []string{"requirement.foo.2"},
			},
			{
				// Inactive requirements must NOT be surfaced to the LLM —
				// matches the legacy builder's status filter so the agent
				// can't accidentally depend on a deprecated requirement.
				ID:     "requirement.foo.deprecated",
				Title:  "Old approach",
				Status: "deprecated",
			},
		},
		ReplaceRequirementIDs: []string{"requirement.foo.3", "requirement.foo.4"},
		RejectionReasons: map[string]string{
			"requirement.foo.3": "scope was too broad",
			// requirement.foo.4 has no reason → falls through to placeholder
		},
	})

	mustContain := []string{
		"## Existing Approved Requirements",
		"requirement.foo.1",
		"Goodbye endpoint returns JSON",
		"files_owned: api/handlers/goodbye.go",
		"depends_on: requirement.foo.2",
		"## Rejected Requirements",
		"requirement.foo.3: rejected because: scope was too broad",
		"requirement.foo.4: rejected because: no reason provided",
		"Generate ONLY replacement requirements for the rejected IDs above",
		"do NOT claim a path already in any kept requirement",
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Errorf("partial-regen prompt missing %q\n--- prompt ---\n%s", want, got)
		}
	}
	if strings.Contains(got, "requirement.foo.deprecated") {
		t.Errorf("inactive requirement leaked into prompt — status filter regressed\n--- prompt ---\n%s", got)
	}
	if strings.Contains(got, "Old approach") {
		t.Errorf("inactive requirement title leaked\n--- prompt ---\n%s", got)
	}
}

func TestRenderRequirementGeneratorPrompt_PreviousErrorAndReviewFindings(t *testing.T) {
	got := renderRequirementGeneratorPrompt(&prompt.RequirementGeneratorContext{
		Title:          "x",
		Goal:           "y",
		PreviousError:  "could not parse JSON: unexpected token",
		ReviewFindings: "- requirement 2 missing scenarios",
	})
	if !strings.Contains(got, "## Previous Attempt Failed") {
		t.Errorf("previous-error section missing")
	}
	if !strings.Contains(got, "could not parse JSON: unexpected token") {
		t.Errorf("previous error text missing")
	}
	if !strings.Contains(got, "## Previous Review Findings") {
		t.Errorf("review-findings section missing")
	}
	if !strings.Contains(got, "requirement 2 missing scenarios") {
		t.Errorf("review findings text missing")
	}
}

// TestAssemblerEndToEnd_RequirementGenerator pins the full pipeline: register
// the Software() fragments, set the typed RequirementGenerator context, and
// confirm Assemble produces the expected user message via the registry path.
// This is the contract the requirement-generator component is about to
// migrate to.
func TestAssemblerEndToEnd_RequirementGenerator(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)
	a := prompt.NewAssembler(r)

	out := a.Assemble(&prompt.AssemblyContext{
		Role:           prompt.RoleRequirementGenerator,
		Provider:       prompt.ProviderOpenAI,
		AvailableTools: []string{"submit_work", "graph_summary"},
		RequirementGenerator: &prompt.RequirementGeneratorContext{
			Title: "Add /goodbye endpoint",
			Goal:  "Return a goodbye message as JSON",
		},
	})

	if out.RenderError != nil {
		t.Fatalf("unexpected RenderError: %v", out.RenderError)
	}
	if out.UserPromptID != "software.requirement-generator.user-prompt" {
		t.Errorf("UserPromptID = %q, want the registry's requirement-generator user-prompt fragment", out.UserPromptID)
	}
	if !strings.Contains(out.UserMessage, "Add /goodbye endpoint") {
		t.Errorf("user message missing plan title: %q", out.UserMessage)
	}
	if !strings.Contains(out.SystemMessage, "files_owned") {
		t.Errorf("system message missing dial-#1 partition guidance — fragment ordering regressed")
	}
	// Belt-and-suspenders: user-message content must NOT leak into system
	// message (the whole reason CategoryUserPrompt exists).
	if strings.Contains(out.SystemMessage, "Add /goodbye endpoint") {
		t.Errorf("user-prompt content leaked into system message — assembler bug")
	}
}

// TestRenderPlanReviewerPrompt_R1PhaseBoundaries pins the take-19 fix from the
// 2026-05-08 OpenRouter @easy run: llama-3.3-70b's reviewer rejected a
// well-formed /health plan two rounds in a row, demanding "specific details
// about the implementation" — implementation form is downstream-phase work,
// not a plan-gate concern. The R1 completeness block now leads with a
// Phase boundaries stanza and anchors criterion #2 to a downstream-test
// framing ("could a requirement-generator derive at least one testable
// requirement"). Drop either and weak reviewer models start manufacturing
// implementation-detail concerns at the plan gate again.
func TestRenderPlanReviewerPrompt_R1PhaseBoundaries(t *testing.T) {
	out := renderPlanReviewerPrompt(&prompt.PlanReviewerPromptContext{
		Slug:        "abc123",
		PlanContent: `{"goal":"x","context":"y","scope":{}}`,
		Round:       1,
	})

	mustContain := []string{
		"Phase boundaries",
		"goal + context + scope",
		"requirements and architecture phases that run AFTER this review",
		"could derive at least one testable requirement",
	}
	for _, want := range mustContain {
		if !strings.Contains(out, want) {
			t.Errorf("R1 prompt missing pinned string %q\nGot:\n%s", want, out)
		}
	}
}

// TestRenderPlanReviewerPrompt_R2UntouchedByR1Edits guards against accidentally
// dragging the R1 phase-boundary stanza into R2 (which reviews requirements +
// scenarios + architecture and SHOULD demand implementation-adjacent rigor).
func TestRenderPlanReviewerPrompt_R2UntouchedByR1Edits(t *testing.T) {
	out := renderPlanReviewerPrompt(&prompt.PlanReviewerPromptContext{
		Slug:        "abc123",
		PlanContent: `{"goal":"x"}`,
		Round:       2,
	})

	if strings.Contains(out, "Phase boundaries") {
		t.Error("R2 prompt should NOT carry the R1 phase-boundary stanza — R2 reviews downstream artifacts")
	}
	if !strings.Contains(out, "Round 2") {
		t.Errorf("R2 prompt missing its own header\nGot:\n%s", out)
	}
}

// TestRenderPlanReviewerPrompt_R2IncludesUpstreamResolutionCriterion locks
// in criterion 7a (added 2026-05-15 alongside ArchitectureDocument.
// UpstreamResolutions). Without it the architect's new schema would land
// without an enforcement gate — architect could ignore upstream_resolutions
// and the dev would still wedge on re-discovery. The criterion's signature
// phrases ("Upstream resolution discipline", "upstream_resolutions",
// "coordinate", "citation", "back-link") are what the model anchors on
// when emitting Path B-shape findings; pinning them here catches a
// well-meaning rewrite that softens the rule into uselessness.
func TestRenderPlanReviewerPrompt_R2IncludesUpstreamResolutionCriterion(t *testing.T) {
	out := renderPlanReviewerPrompt(&prompt.PlanReviewerPromptContext{
		Slug:        "abc123",
		PlanContent: `{"goal":"x"}`,
		Round:       2,
	})

	required := []string{
		"7a.",                              // criterion number
		"Upstream resolution discipline",   // criterion title
		"upstream_resolutions",             // the schema field reviewer enforces
		"coordinate",                       // structural-completeness check
		"source_ref",                       // structural-completeness check
		"citation",                         // structural-completeness check on APISurface
		"Bidirectional invariant",          // back-link rule
		"upstream_refs",                    // bidirectional partner field
		"Goodhart guard",                   // anti-pad rule
	}
	for _, want := range required {
		if !strings.Contains(out, want) {
			t.Errorf("R2 reviewer prompt missing %q (criterion 7a regressed?)\nFull prompt:\n%s", want, out)
		}
	}
}

// TestRenderPlanReviewerPrompt_PriorRoundInjectsFindings pins the take-22
// fix: on revision rounds (ReviewIteration > 0), the reviewer must see its
// own previous findings + iteration context. Without this the reviewer is
// stateless across rounds and a non-deterministic model can re-fire the
// same complaint shape even when the planner addressed it (the take-22
// 1/8 wedge). First-round (ReviewIteration=0) MUST omit the section.
func TestRenderPlanReviewerPrompt_PriorRoundInjectsFindings(t *testing.T) {
	tests := []struct {
		name        string
		ctx         *prompt.PlanReviewerPromptContext
		mustContain []string
		mustNotHave []string
	}{
		{
			name: "first review round — no prior-round section",
			ctx: &prompt.PlanReviewerPromptContext{
				Slug:             "abc123",
				PlanContent:      `{"goal":"x"}`,
				Round:            1,
				ReviewIteration:  0,
				PreviousFindings: "",
			},
			mustNotHave: []string{
				"## Previous Review Round",
				"<previous-review",
				"This is review iteration",
			},
		},
		{
			name: "revision round — prior findings rendered with iteration + budget",
			ctx: &prompt.PlanReviewerPromptContext{
				Slug:                "abc123",
				PlanContent:         `{"goal":"x"}`,
				Round:               1,
				ReviewIteration:     1,
				MaxReviewIterations: 3,
				PreviousFindings:    "- [error] goal-clarity: lacks specifics on health endpoint",
			},
			mustContain: []string{
				"## Previous Review Round (this is a revision)",
				"This is review iteration 2 of 3",
				"approve this round, even if you can imagine further improvements",
				"Re-rejecting on the same complaint shape",
				`<previous-review trust="semspec-internal">`,
				"goal-clarity: lacks specifics on health endpoint",
				"</previous-review>",
			},
		},
		{
			name: "revision round with no max — iteration without ceiling",
			ctx: &prompt.PlanReviewerPromptContext{
				Slug:                "abc123",
				PlanContent:         `{"goal":"x"}`,
				Round:               1,
				ReviewIteration:     1,
				MaxReviewIterations: 0,
				PreviousFindings:    "- [error] something",
			},
			mustContain: []string{
				"This is review iteration 2.",
			},
			mustNotHave: []string{
				"of 0", // never render the noisy "of 0" framing
			},
		},
		{
			name: "ReviewIteration > 0 but findings empty — degrade gracefully",
			ctx: &prompt.PlanReviewerPromptContext{
				Slug:             "abc123",
				PlanContent:      `{"goal":"x"}`,
				Round:            1,
				ReviewIteration:  1,
				PreviousFindings: "   ", // whitespace-only
			},
			mustNotHave: []string{
				"## Previous Review Round",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := renderPlanReviewerPrompt(tt.ctx)
			for _, want := range tt.mustContain {
				if !strings.Contains(out, want) {
					t.Errorf("missing pinned string %q\nGot:\n%s", want, out)
				}
			}
			for _, banned := range tt.mustNotHave {
				if strings.Contains(out, banned) {
					t.Errorf("unexpected string %q present\nGot:\n%s", banned, out)
				}
			}
		})
	}
}

// TestRenderPlanReviewerPrompt_PriorRoundBeforeContent guards the ordering:
// prior-round context must appear BEFORE the plan content so the reviewer
// reads "I previously rejected this for X" before re-evaluating the plan.
func TestRenderPlanReviewerPrompt_PriorRoundBeforeContent(t *testing.T) {
	out := renderPlanReviewerPrompt(&prompt.PlanReviewerPromptContext{
		Slug:                "abc123",
		PlanContent:         `{"goal":"verify ordering"}`,
		Round:               1,
		ReviewIteration:     1,
		MaxReviewIterations: 3,
		PreviousFindings:    "- [error] something",
	})

	priorIdx := strings.Index(out, "## Previous Review Round")
	planIdx := strings.Index(out, "## Plan to Review")
	if priorIdx < 0 || planIdx < 0 {
		t.Fatalf("expected both sections present\nGot:\n%s", out)
	}
	if priorIdx >= planIdx {
		t.Errorf("prior-round must appear before plan content; prior@%d plan@%d", priorIdx, planIdx)
	}
}

// TestRenderPlanReviewerPrompt_ProjectFileTreeInjection pins the take-20 fix:
// when the plan-reviewer is given a ground-truth file tree, it MUST appear
// before plan content with reviewer-appropriate framing ("verify scope.include
// against this list", not the planner's "any path you put in scope MUST appear
// here"). Without this section the role-context's path-check rule fires
// against ground truth the reviewer never received and weak models default
// to flagging real files as hallucinated.
func TestRenderPlanReviewerPrompt_ProjectFileTreeInjection(t *testing.T) {
	tests := []struct {
		name        string
		ctx         *prompt.PlanReviewerPromptContext
		mustContain []string
		mustNotHave []string
	}{
		{
			name: "tree present — section rendered with reviewer framing",
			ctx: &prompt.PlanReviewerPromptContext{
				Slug:            "abc123",
				PlanContent:     `{"goal":"x","scope":{"include":["main.go"]}}`,
				Round:           1,
				ProjectFileTree: "main.go\ngo.mod\ninternal/auth/auth.go",
			},
			mustContain: []string{
				"## Project Files",
				"main.go\ngo.mod\ninternal/auth/auth.go",
				"Use this list to verify the plan's scope.include paths",
				"do NOT flag it as hallucinated",
				"Paths in scope.create are creation-intent declarations",
			},
		},
		{
			name: "tree empty — section silently omitted (greenfield-safe)",
			ctx: &prompt.PlanReviewerPromptContext{
				Slug:            "abc123",
				PlanContent:     `{"goal":"x"}`,
				Round:           1,
				ProjectFileTree: "",
			},
			mustNotHave: []string{
				"## Project Files",
				"do NOT flag it as hallucinated",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := renderPlanReviewerPrompt(tt.ctx)
			for _, want := range tt.mustContain {
				if !strings.Contains(out, want) {
					t.Errorf("missing pinned string %q\nGot:\n%s", want, out)
				}
			}
			for _, banned := range tt.mustNotHave {
				if strings.Contains(out, banned) {
					t.Errorf("unexpected string %q present\nGot:\n%s", banned, out)
				}
			}
		})
	}
}

// TestRenderPlanReviewerPrompt_TreeBeforePlanContent guards that the file
// tree (when present) appears BEFORE the plan content — the reviewer must
// read ground truth before judging the planner's scope claims.
func TestRenderPlanReviewerPrompt_TreeBeforePlanContent(t *testing.T) {
	out := renderPlanReviewerPrompt(&prompt.PlanReviewerPromptContext{
		Slug:            "abc123",
		PlanContent:     `{"goal":"verify ordering"}`,
		Round:           1,
		ProjectFileTree: "main.go",
	})

	treeIdx := strings.Index(out, "## Project Files")
	planIdx := strings.Index(out, "## Plan to Review")
	if treeIdx < 0 || planIdx < 0 {
		t.Fatalf("expected both sections present\nGot:\n%s", out)
	}
	if treeIdx >= planIdx {
		t.Errorf("file tree must appear before plan content; tree@%d plan@%d", treeIdx, planIdx)
	}
}

// TestRenderRequirementGeneratorPrompt_ProjectFileTreeInjection mirrors the
// plan-reviewer take-20 fix one layer up: the requirement-generator persona
// repeatedly tells the model to draw files_owned from real paths and warns
// against "inventing fake file splits". Without a ground-truth tree, weak
// models still hallucinate idiomatic-looking paths (api/handlers/*.go on a
// project with no api/ directory). When ProjectFileTree is set, the renderer
// surfaces it BEFORE the plan section with framing tied to the files_owned
// rule. Empty input silently omits the section so greenfield is unaffected.
func TestRenderRequirementGeneratorPrompt_ProjectFileTreeInjection(t *testing.T) {
	tests := []struct {
		name        string
		ctx         *prompt.RequirementGeneratorContext
		mustContain []string
		mustNotHave []string
	}{
		{
			name: "tree present — surfaced with files_owned framing",
			ctx: &prompt.RequirementGeneratorContext{
				Title:           "Add /health",
				Goal:            "expose service health",
				ProjectFileTree: "main.go\ninternal/auth/auth.go",
			},
			mustContain: []string{
				"## Project Files",
				"main.go\ninternal/auth/auth.go",
				"Use this list when filling files_owned",
				"Do NOT invent paths that look idiomatic",
			},
		},
		{
			name: "tree empty — section omitted (greenfield-safe)",
			ctx: &prompt.RequirementGeneratorContext{
				Title:           "Bootstrap service",
				Goal:            "create new service",
				ProjectFileTree: "",
			},
			mustNotHave: []string{
				"## Project Files",
				"Use this list when filling files_owned",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := renderRequirementGeneratorPrompt(tt.ctx)
			for _, want := range tt.mustContain {
				if !strings.Contains(out, want) {
					t.Errorf("missing pinned string %q\nGot:\n%s", want, out)
				}
			}
			for _, banned := range tt.mustNotHave {
				if strings.Contains(out, banned) {
					t.Errorf("unexpected string %q present\nGot:\n%s", banned, out)
				}
			}
		})
	}
}

// TestRenderRequirementGeneratorPrompt_TreeBeforePlanSection guards that the
// file tree (when present) appears BEFORE the plan-decompose section — the
// generator should ground itself in real paths before partitioning files.
func TestRenderRequirementGeneratorPrompt_TreeBeforePlanSection(t *testing.T) {
	out := renderRequirementGeneratorPrompt(&prompt.RequirementGeneratorContext{
		Title:           "ordering check",
		Goal:            "verify ordering",
		ProjectFileTree: "main.go",
	})

	treeIdx := strings.Index(out, "## Project Files")
	planIdx := strings.Index(out, "## Plan to Decompose")
	if treeIdx < 0 || planIdx < 0 {
		t.Fatalf("expected both sections present\nGot:\n%s", out)
	}
	if treeIdx >= planIdx {
		t.Errorf("file tree must appear before plan section; tree@%d plan@%d", treeIdx, planIdx)
	}
}

// TestRenderScenarioGeneratorPrompt_PlanContextRendered pins a silent-data-loss
// bug found in the 2026-05-08 audit: scenario-generator's PlanContext field
// was set by the producer (plan_watcher.go) but never read by the renderer.
// The model never saw the plan's "why" — only goal + requirement details +
// architecture. When the planner's context says something like "no existing
// endpoints, this is greenfield", losing that line means the scenario-
// generator can write scenarios that assume existing surface area.
func TestRenderScenarioGeneratorPrompt_PlanContextRendered(t *testing.T) {
	tests := []struct {
		name        string
		ctx         *prompt.ScenarioGeneratorPromptContext
		mustContain []string
		mustNotHave []string
	}{
		{
			name: "context present — surfaced in user prompt",
			ctx: &prompt.ScenarioGeneratorPromptContext{
				PlanTitle:              "Add /health",
				PlanGoal:               "expose service health",
				PlanContext:            "no existing endpoints; this is a greenfield service",
				RequirementID:          "req-1",
				RequirementTitle:       "/health endpoint",
				RequirementDescription: "GET /health returns JSON",
			},
			mustContain: []string{
				"**Goal:** expose service health",
				"**Context:** no existing endpoints; this is a greenfield service",
				"## Requirement: /health endpoint",
			},
		},
		{
			name: "context empty — section silently omitted, no '**Context:**' header",
			ctx: &prompt.ScenarioGeneratorPromptContext{
				PlanTitle:        "Add /health",
				PlanGoal:         "expose service health",
				PlanContext:      "",
				RequirementTitle: "/health endpoint",
			},
			mustNotHave: []string{
				"**Context:**",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := renderScenarioGeneratorPrompt(tt.ctx)
			for _, want := range tt.mustContain {
				if !strings.Contains(out, want) {
					t.Errorf("missing pinned string %q\nGot:\n%s", want, out)
				}
			}
			for _, banned := range tt.mustNotHave {
				if strings.Contains(out, banned) {
					t.Errorf("unexpected string %q present\nGot:\n%s", banned, out)
				}
			}
		})
	}
}

// TestRenderTaskDecomposerPrompt_IncludesCompletenessSignal pins the
// take-11 fix: the renderer surfaces requirement title, description,
// scope, prereqs, and the scenario-coverage rule. The completeness rule
// itself is in the persona fragment (system-base), not the user prompt;
// this test focuses on the per-dispatch context bake-in.
func TestRenderTaskDecomposerPrompt_IncludesCoreContext(t *testing.T) {
	out := renderTaskDecomposerPrompt(&prompt.DecomposerPromptContext{
		RequirementTitle:       "Implement Meshtastic driver",
		RequirementDescription: "OSH driver that bridges Meshtastic frames to the CS-API",
		ScopeInclude:           []string{"src/main/java/io/opensensorhub/drivers/meshtastic", "build.gradle"},
		ScopeDoNotTouch:        []string{"main.go"},
		Scenarios: []prompt.DecomposerScenario{
			{ID: "sc-frame-parse", Given: "a Meshtastic frame fixture", When: "ingested", Then: []string{"one CS-API message emitted"}},
		},
	})
	mustContain := []string{
		"Implement Meshtastic driver",
		"OSH driver",
		"src/main/java/io/opensensorhub/drivers/meshtastic",
		"sc-frame-parse",
		"scenario_ids array",
		"Do not touch",
	}
	for _, want := range mustContain {
		if !strings.Contains(out, want) {
			t.Errorf("renderTaskDecomposerPrompt missing %q\nfull:\n%s", want, out)
		}
	}
}

func TestRenderTaskDecomposerPrompt_RetryFeedbackOnFirstLine(t *testing.T) {
	out := renderTaskDecomposerPrompt(&prompt.DecomposerPromptContext{
		RequirementTitle: "Try again",
		RetryFeedback:    "previous attempt emitted empty nodes array",
	})
	// Retry feedback should appear before the requirement section so the
	// LLM sees the prior failure first.
	if !strings.HasPrefix(out, "RETRY") {
		t.Errorf("retry feedback should prefix prompt; got first 80 chars: %s", clip(out, 80))
	}
	if !strings.Contains(out, "previous attempt emitted empty nodes array") {
		t.Errorf("renderTaskDecomposerPrompt missing retry-feedback content\nfull:\n%s", out)
	}
}

// TestRenderRecoveryAgentPrompt_IncludesEvidence covers the user prompt
// for RoleRecoveryAgent. Ported from the legacy
// processor/recovery-agent/result_test.go::TestBuildUserPromptIncludesContext
// when the recovery-agent dispatch was wired through the assembler
// 2026-05-11.
func TestRenderRecoveryAgentPrompt_IncludesEvidence(t *testing.T) {
	out := renderRecoveryAgentPrompt(&prompt.RecoveryPromptContext{
		Layer:               "phase_local",
		Slug:                "my-plan",
		TaskID:              "task-42",
		LoopID:              "loop-xyz",
		EscalationReason:    "fixable rejections exceeded TDD cycle budget",
		LastFailureFeedback: "Test failure: NullPointerException at line 17",
		TrajectorySteps:     []string{"model_call(planner)", "tool_call(bash) → ls", "tool_call(graph_search) → no hits"},
	})

	mustContain := []string{
		"phase_local",
		"my-plan",
		"task-42",
		"loop-xyz",
		"fixable rejections exceeded TDD cycle budget",
		"NullPointerException",
		"tool_call(bash)",
		"submit_work",
	}
	for _, want := range mustContain {
		if !strings.Contains(out, want) {
			t.Errorf("expected prompt to contain %q\nfull prompt:\n%s", want, out)
		}
	}
}

func TestRenderRecoveryAgentPrompt_EmptyTrajectoryFallback(t *testing.T) {
	out := renderRecoveryAgentPrompt(&prompt.RecoveryPromptContext{
		Layer:            "phase_local",
		Slug:             "no-traj",
		EscalationReason: "iter=50 budget exhausted",
	})
	if !strings.Contains(out, "no trajectory available") {
		t.Errorf("expected fallback notice when trajectory is empty\nfull prompt:\n%s", out)
	}
}

// clip is a local helper for test diagnostics — keeps error messages
// bounded when the rendered output is multi-KB.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
