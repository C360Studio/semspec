package planreviewer

import (
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestMergeStoryFindings_NoStoriesIsNoop(t *testing.T) {
	plan := &workflow.Plan{
		Slug:         "legacy",
		Requirements: []workflow.Requirement{{ID: "r1"}},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}
	mergeStoryFindings(plan, result)
	if len(result.Findings) != 0 {
		t.Errorf("expected 0 findings for plan without stories, got %d: %+v", len(result.Findings), result.Findings)
	}
	if result.Verdict != "approved" {
		t.Errorf("expected verdict preserved, got %q", result.Verdict)
	}
}

func TestMergeStoryFindings_RequirementOrphan(t *testing.T) {
	plan := &workflow.Plan{
		Slug:         "orphan-req",
		Requirements: []workflow.Requirement{{ID: "r1"}},
		Stories: []workflow.Story{
			{ID: "s1", RequirementIDs: []string{"ghost"}, ComponentName: "placeholder-component", Title: "T",
				FilesOwned: []string{"src/x.go"},
				Tasks:      []workflow.Task{{ID: "t1", StoryID: "s1", Description: "x"}}},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}
	mergeStoryFindings(plan, result)
	if !hasFinding(result.Findings, "story.requirement_orphan") {
		t.Errorf("expected story.requirement_orphan, got: %+v", result.Findings)
	}
	if result.Verdict != "needs_changes" {
		t.Errorf("expected verdict bumped to needs_changes, got %q", result.Verdict)
	}
}

func TestMergeStoryFindings_UnresolvedComponents(t *testing.T) {
	plan := &workflow.Plan{
		Slug:         "unresolved-comp",
		Requirements: []workflow.Requirement{{ID: "r1"}},
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				{Name: "auth-service", ImplementationFiles: []string{"src/auth.go"}, Capabilities: []string{"auth"}},
			},
		},
		Stories: []workflow.Story{
			{ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "placeholder-component", Title: "T",
				Components: []string{"auth-service", "ghost-component"},
				FilesOwned: []string{"src/x.go"},
				Tasks:      []workflow.Task{{ID: "t1", StoryID: "s1", Description: "x"}}},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}
	mergeStoryFindings(plan, result)
	if !hasFinding(result.Findings, "story.unresolved_components") {
		t.Errorf("expected story.unresolved_components, got: %+v", result.Findings)
	}
}

func TestMergeStoryFindings_MissingFilesOwned(t *testing.T) {
	plan := &workflow.Plan{
		Slug:         "missing-files",
		Requirements: []workflow.Requirement{{ID: "r1"}},
		Stories: []workflow.Story{
			{ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "placeholder-component", Title: "T", Status: workflow.StoryStatusReady,
				Tasks: []workflow.Task{{ID: "t1", StoryID: "s1", Description: "x"}}},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}
	mergeStoryFindings(plan, result)
	if !hasFinding(result.Findings, "story.missing_files_owned") {
		t.Errorf("expected story.missing_files_owned, got: %+v", result.Findings)
	}
}

func TestMergeStoryFindings_DocsOnlyFiles(t *testing.T) {
	plan := &workflow.Plan{
		Slug:         "docs-only",
		Requirements: []workflow.Requirement{{ID: "r1"}},
		Stories: []workflow.Story{
			{ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "placeholder-component", Title: "T", Status: workflow.StoryStatusReady,
				FilesOwned: []string{"README.md", "docs/x.md"},
				Tasks:      []workflow.Task{{ID: "t1", StoryID: "s1", Description: "x"}}},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}
	mergeStoryFindings(plan, result)
	if !hasFinding(result.Findings, "story.docs_only_files_owned") {
		t.Errorf("expected story.docs_only_files_owned, got: %+v", result.Findings)
	}
}

func TestMergeStoryFindings_MissingFilesAndDocsOnlyDontDoubleFire(t *testing.T) {
	plan := &workflow.Plan{
		Slug:         "no-double",
		Requirements: []workflow.Requirement{{ID: "r1"}},
		Stories: []workflow.Story{
			{ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "placeholder-component", Title: "T", Status: workflow.StoryStatusReady,
				FilesOwned: nil,
				Tasks:      []workflow.Task{{ID: "t1", StoryID: "s1", Description: "x"}}},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}
	mergeStoryFindings(plan, result)
	for _, f := range result.Findings {
		if f.SOPID == "story.docs_only_files_owned" {
			t.Errorf("empty files_owned should not also fire docs_only, got: %+v", f)
		}
	}
}

func TestMergeStoryFindings_MissingTasks(t *testing.T) {
	plan := &workflow.Plan{
		Slug:         "no-tasks",
		Requirements: []workflow.Requirement{{ID: "r1"}},
		Stories: []workflow.Story{
			{ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "placeholder-component", Title: "T", Status: workflow.StoryStatusReady,
				FilesOwned: []string{"src/x.go"},
				Tasks:      nil},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}
	mergeStoryFindings(plan, result)
	if !hasFinding(result.Findings, "task.missing_within_story") {
		t.Errorf("expected task.missing_within_story, got: %+v", result.Findings)
	}
}

func TestMergeStoryFindings_StoryDependsOnOrphan(t *testing.T) {
	plan := &workflow.Plan{
		Slug:         "story-orphan",
		Requirements: []workflow.Requirement{{ID: "r1"}},
		Stories: []workflow.Story{
			{ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "placeholder-component", Title: "T", Status: workflow.StoryStatusReady,
				FilesOwned: []string{"src/x.go"},
				DependsOn:  []string{"ghost-story"},
				Tasks:      []workflow.Task{{ID: "t1", StoryID: "s1", Description: "x"}}},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}
	mergeStoryFindings(plan, result)
	if !hasFinding(result.Findings, "story.depends_on_orphan") {
		t.Errorf("expected story.depends_on_orphan, got: %+v", result.Findings)
	}
}

func TestMergeStoryFindings_StoryDependsOnCycle(t *testing.T) {
	plan := &workflow.Plan{
		Slug:         "story-cycle",
		Requirements: []workflow.Requirement{{ID: "r1"}},
		Stories: []workflow.Story{
			{ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "placeholder-component", Title: "T1", Status: workflow.StoryStatusReady,
				FilesOwned: []string{"src/x.go"},
				DependsOn:  []string{"s2"},
				Tasks:      []workflow.Task{{ID: "t1", StoryID: "s1", Description: "x"}}},
			{ID: "s2", RequirementIDs: []string{"r1"}, ComponentName: "placeholder-component-2", Title: "T2", Status: workflow.StoryStatusReady,
				FilesOwned: []string{"src/y.go"},
				DependsOn:  []string{"s1"},
				Tasks:      []workflow.Task{{ID: "t2", StoryID: "s2", Description: "y"}}},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}
	mergeStoryFindings(plan, result)
	if !hasFinding(result.Findings, "story.depends_on_cycle") {
		t.Errorf("expected story.depends_on_cycle, got: %+v", result.Findings)
	}
}

func TestMergeStoryFindings_TaskDependsOnCycle(t *testing.T) {
	plan := &workflow.Plan{
		Slug:         "task-cycle",
		Requirements: []workflow.Requirement{{ID: "r1"}},
		Stories: []workflow.Story{
			{ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "placeholder-component", Title: "T", Status: workflow.StoryStatusReady,
				FilesOwned: []string{"src/x.go"},
				Tasks: []workflow.Task{
					{ID: "t1", StoryID: "s1", Description: "x", DependsOn: []string{"t2"}},
					{ID: "t2", StoryID: "s1", Description: "y", DependsOn: []string{"t1"}},
				}},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}
	mergeStoryFindings(plan, result)
	if !hasFinding(result.Findings, "task.depends_on_cycle") {
		t.Errorf("expected task.depends_on_cycle, got: %+v", result.Findings)
	}
}

func TestMergeStoryFindings_PendingStoryReadinessRulesSkipped(t *testing.T) {
	// A story still in StoryStatusPending hasn't gone through Sarah's gate
	// yet — readiness-gated invariants (files_owned non-empty, tasks
	// non-empty) must not fire. Cross-entity checks (requirement orphan,
	// component resolution) still apply.
	plan := &workflow.Plan{
		Slug:         "pending",
		Requirements: []workflow.Requirement{{ID: "r1"}},
		Stories: []workflow.Story{
			{ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "placeholder-component", Title: "T", Status: workflow.StoryStatusPending,
				FilesOwned: nil,
				Tasks:      nil},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}
	mergeStoryFindings(plan, result)
	for _, f := range result.Findings {
		if f.SOPID == "story.missing_files_owned" || f.SOPID == "task.missing_within_story" {
			t.Errorf("pending story should not trip readiness-gated rule %s: %+v", f.SOPID, f)
		}
	}
}

func TestMergeStoryFindings_HappyPath(t *testing.T) {
	plan := &workflow.Plan{
		Slug:         "happy",
		Requirements: []workflow.Requirement{{ID: "r1"}},
		Architecture: &workflow.ArchitectureDocument{
			ComponentBoundaries: []workflow.ComponentDef{
				{Name: "auth-service", ImplementationFiles: []string{"src/auth.go"}, Capabilities: []string{"auth"}},
			},
		},
		Stories: []workflow.Story{
			{ID: "s1", RequirementIDs: []string{"r1"}, ComponentName: "placeholder-component", Title: "T", Status: workflow.StoryStatusReady,
				Components: []string{"auth-service"},
				FilesOwned: []string{"src/auth.go"},
				Tasks: []workflow.Task{
					{ID: "t1", StoryID: "s1", Description: "tests"},
					{ID: "t2", StoryID: "s1", Description: "impl", DependsOn: []string{"t1"}},
				}},
		},
	}
	result := &workflow.PlanReviewResult{Verdict: "approved"}
	mergeStoryFindings(plan, result)
	if len(result.Findings) != 0 {
		t.Errorf("expected 0 findings on conformant story set, got %d: %+v", len(result.Findings), result.Findings)
	}
	if result.Verdict != "approved" {
		t.Errorf("expected verdict preserved, got %q", result.Verdict)
	}
}

func hasFinding(findings []workflow.PlanReviewFinding, sopID string) bool {
	for _, f := range findings {
		if f.SOPID == sopID {
			return true
		}
	}
	return false
}
