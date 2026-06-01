package workflow

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestStoryStatusIsValid(t *testing.T) {
	cases := []struct {
		s    StoryStatus
		want bool
	}{
		{StoryStatusPending, true},
		{StoryStatusReady, true},
		{StoryStatusExecuting, true},
		{StoryStatusComplete, true},
		{StoryStatusFailed, true},
		{StoryStatus(""), false},
		{StoryStatus("bogus"), false},
	}
	for _, tc := range cases {
		t.Run(string(tc.s), func(t *testing.T) {
			if got := tc.s.IsValid(); got != tc.want {
				t.Errorf("StoryStatus(%q).IsValid() = %v, want %v", tc.s, got, tc.want)
			}
		})
	}
}

func TestStoryStatusCanTransitionTo(t *testing.T) {
	cases := []struct {
		from, to StoryStatus
		want     bool
	}{
		// Happy path
		{StoryStatusPending, StoryStatusReady, true},
		{StoryStatusReady, StoryStatusExecuting, true},
		{StoryStatusExecuting, StoryStatusComplete, true},
		// Failure path
		{StoryStatusPending, StoryStatusFailed, true},
		{StoryStatusReady, StoryStatusFailed, true},
		{StoryStatusExecuting, StoryStatusFailed, true},
		// Recovery loops
		{StoryStatusComplete, StoryStatusReady, true},
		{StoryStatusFailed, StoryStatusPending, true},
		// Rejected jumps
		{StoryStatusPending, StoryStatusExecuting, false},
		{StoryStatusReady, StoryStatusComplete, false},
		{StoryStatusComplete, StoryStatusExecuting, false},
		{StoryStatusFailed, StoryStatusComplete, false},
		// Bogus source
		{StoryStatus("bogus"), StoryStatusReady, false},
	}
	for _, tc := range cases {
		name := string(tc.from) + "→" + string(tc.to)
		t.Run(name, func(t *testing.T) {
			if got := tc.from.CanTransitionTo(tc.to); got != tc.want {
				t.Errorf("%s = %v, want %v", name, got, tc.want)
			}
		})
	}
}

func TestTaskStatusIsValid(t *testing.T) {
	cases := []struct {
		s    TaskStatus
		want bool
	}{
		{TaskStatusPending, true},
		{TaskStatusDispatched, true},
		{TaskStatusComplete, true},
		{TaskStatusFailed, true},
		{TaskStatus(""), false},
		{TaskStatus("bogus"), false},
	}
	for _, tc := range cases {
		t.Run(string(tc.s), func(t *testing.T) {
			if got := tc.s.IsValid(); got != tc.want {
				t.Errorf("TaskStatus(%q).IsValid() = %v, want %v", tc.s, got, tc.want)
			}
		})
	}
}

func TestTaskStatusCanTransitionTo(t *testing.T) {
	cases := []struct {
		from, to TaskStatus
		want     bool
	}{
		{TaskStatusPending, TaskStatusDispatched, true},
		{TaskStatusDispatched, TaskStatusComplete, true},
		{TaskStatusDispatched, TaskStatusFailed, true},
		{TaskStatusDispatched, TaskStatusPending, true},
		{TaskStatusComplete, TaskStatusPending, true},
		{TaskStatusFailed, TaskStatusPending, true},
		// Rejected
		{TaskStatusPending, TaskStatusComplete, false},
		{TaskStatusComplete, TaskStatusDispatched, false},
		{TaskStatusFailed, TaskStatusComplete, false},
	}
	for _, tc := range cases {
		name := string(tc.from) + "→" + string(tc.to)
		t.Run(name, func(t *testing.T) {
			if got := tc.from.CanTransitionTo(tc.to); got != tc.want {
				t.Errorf("%s = %v, want %v", name, got, tc.want)
			}
		})
	}
}

// TestStoryJSONRoundTrip ensures the Story wire shape survives marshal +
// unmarshal with omitempty hygiene on optional fields.
func TestStoryJSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	prepared := now.Add(5 * time.Minute)
	in := Story{
		ID:            "story.x.1.1",
		RequirementID: "req.x.1",
		Title:         "MAVSDK Lifecycle Bootstrap",
		Intent:        "Boot mavsdk_server and observe HEARTBEAT.",
		Components:    []string{"mavsdk-server-lifecycle"},
		FilesOwned:    []string{"src/Lifecycle.java"},
		DependsOn:     []string{},
		Tasks: []Task{
			{ID: "task.x.1.1.1", StoryID: "story.x.1.1", Description: "Write failing test",
				CreatedAt: now, UpdatedAt: now},
		},
		Status:     StoryStatusReady,
		PreparedBy: "sarah",
		PreparedAt: &prepared,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Story
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.ID != in.ID || out.RequirementID != in.RequirementID || out.Title != in.Title {
		t.Errorf("identity fields drifted: in=%+v out=%+v", in, out)
	}
	if out.Status != StoryStatusReady {
		t.Errorf("status drifted: %q", out.Status)
	}
	if out.PreparedAt == nil || !out.PreparedAt.Equal(prepared) {
		t.Errorf("prepared_at drifted: %v", out.PreparedAt)
	}
	if len(out.Tasks) != 1 || out.Tasks[0].ID != "task.x.1.1.1" {
		t.Errorf("tasks drifted: %+v", out.Tasks)
	}
}

// TestStoryJSONOmitemptyOnZero ensures that a freshly-generated Story (Status
// empty, no PreparedAt, no Tasks) marshals without "status":"" / null fields
// that would poison plan-reviewer asymmetry checks (b7r50o9ov pattern).
func TestStoryJSONOmitemptyOnZero(t *testing.T) {
	in := Story{ID: "story.x.1.1", RequirementID: "req.x.1", Title: "Untouched"}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	for _, banned := range []string{
		`"status"`, `"prepared_by"`, `"prepared_at"`, `"recovery_hint"`,
		`"intent"`, `"components"`, `"files_owned"`, `"depends_on"`, `"tasks"`,
	} {
		if strings.Contains(s, banned) {
			t.Errorf("zero-value Story emitted %s; want omitempty: %s", banned, s)
		}
	}
}

// TestTaskJSONOmitemptyOnZero is the symmetric check for Task.
func TestTaskJSONOmitemptyOnZero(t *testing.T) {
	in := Task{ID: "task.x.1.1.1", StoryID: "story.x.1.1", Description: "Write test"}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	for _, banned := range []string{`"status"`, `"depends_on"`} {
		if strings.Contains(s, banned) {
			t.Errorf("zero-value Task emitted %s; want omitempty: %s", banned, s)
		}
	}
}

// TestComponentDefImplementationFieldsOmitEmpty ensures the new ADR-043
// fields don't appear on legacy components persisted before PR 2 wired
// the schema enforcement.
func TestComponentDefImplementationFieldsOmitEmpty(t *testing.T) {
	c := ComponentDef{Name: "legacy", Responsibility: "old", Dependencies: []string{}}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	for _, banned := range []string{`"implementation_files"`, `"capabilities"`} {
		if strings.Contains(s, banned) {
			t.Errorf("legacy ComponentDef emitted %s; want omitempty: %s", banned, s)
		}
	}
}

// TestComponentDefRoundTripsADR043Fields verifies that when the new fields
// are populated they survive marshal + unmarshal.
func TestComponentDefRoundTripsADR043Fields(t *testing.T) {
	in := ComponentDef{
		Name:                "lifecycle",
		Responsibility:      "boot mavsdk",
		Dependencies:        []string{},
		ImplementationFiles: []string{"src/Lifecycle.java", "README.md"},
		Capabilities:        []string{"mavsdk-bootstrap"},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out ComponentDef
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.ImplementationFiles) != 2 || out.ImplementationFiles[0] != "src/Lifecycle.java" {
		t.Errorf("implementation_files drifted: %v", out.ImplementationFiles)
	}
	if len(out.Capabilities) != 1 || out.Capabilities[0] != "mavsdk-bootstrap" {
		t.Errorf("capabilities drifted: %v", out.Capabilities)
	}
}

// TestScenarioStoryIDOmitemptyAndRoundTrip verifies the StoryID field
// stays omitempty on legacy scenarios but survives a round-trip when set.
func TestScenarioStoryIDOmitemptyAndRoundTrip(t *testing.T) {
	legacy := Scenario{ID: "s.x.1.1", RequirementID: "req.x.1", Given: "g", When: "w", Then: []string{"t"}}
	b, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), `"story_id"`) {
		t.Errorf("legacy Scenario emitted story_id; want omitempty: %s", string(b))
	}

	tagged := legacy
	tagged.StoryID = "story.x.1.1"
	b, err = json.Marshal(tagged)
	if err != nil {
		t.Fatalf("marshal tagged: %v", err)
	}
	if !strings.Contains(string(b), `"story_id":"story.x.1.1"`) {
		t.Errorf("tagged Scenario missing story_id: %s", string(b))
	}
	var out Scenario
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal tagged: %v", err)
	}
	if out.StoryID != "story.x.1.1" {
		t.Errorf("story_id drifted: %q", out.StoryID)
	}
}

func TestPlanStoryHelpers(t *testing.T) {
	plan := Plan{
		ID:   "plan.x",
		Slug: "x",
		Requirements: []Requirement{
			{ID: "req.x.1"},
			{ID: "req.x.2"},
		},
		Stories: []Story{
			{ID: "story.x.1.1", RequirementID: "req.x.1", Title: "A",
				Tasks: []Task{
					{ID: "task.x.1.1.1", StoryID: "story.x.1.1", Description: "t1"},
					{ID: "task.x.1.1.2", StoryID: "story.x.1.1", Description: "t2"},
				}},
			{ID: "story.x.1.2", RequirementID: "req.x.1", Title: "B"},
			{ID: "story.x.2.1", RequirementID: "req.x.2", Title: "C",
				Tasks: []Task{{ID: "task.x.2.1.1", StoryID: "story.x.2.1", Description: "t3"}}},
		},
		Scenarios: []Scenario{
			{ID: "scen.1", RequirementID: "req.x.1", StoryID: "story.x.1.1"},
			{ID: "scen.2", RequirementID: "req.x.1", StoryID: "story.x.1.1"},
			{ID: "scen.3", RequirementID: "req.x.2", StoryID: "story.x.2.1"},
		},
	}

	got, idx := plan.FindStory("story.x.1.2")
	if got == nil || idx != 1 || got.Title != "B" {
		t.Errorf("FindStory(story.x.1.2) = %+v, %d; want second story", got, idx)
	}
	if g, i := plan.FindStory("missing"); g != nil || i != -1 {
		t.Errorf("FindStory(missing) = %+v, %d; want nil, -1", g, i)
	}

	storiesForReq1 := plan.StoriesForRequirement("req.x.1")
	if len(storiesForReq1) != 2 {
		t.Fatalf("StoriesForRequirement(req.x.1) returned %d stories, want 2", len(storiesForReq1))
	}
	if storiesForReq1[0].ID != "story.x.1.1" || storiesForReq1[1].ID != "story.x.1.2" {
		t.Errorf("StoriesForRequirement returned wrong order: %+v", storiesForReq1)
	}

	task, si, ti := plan.FindTask("task.x.1.1.2")
	if task == nil || si != 0 || ti != 1 {
		t.Errorf("FindTask(task.x.1.1.2) = %+v, %d, %d; want second task of first story", task, si, ti)
	}
	if tk, _, _ := plan.FindTask("task.x.2.1.1"); tk == nil || tk.Description != "t3" {
		t.Errorf("FindTask cross-story lookup failed: %+v", tk)
	}
	if tk, si2, ti2 := plan.FindTask("missing"); tk != nil || si2 != -1 || ti2 != -1 {
		t.Errorf("FindTask(missing) = %+v, %d, %d; want nil, -1, -1", tk, si2, ti2)
	}

	scens := plan.ScenariosForStory("story.x.1.1")
	if len(scens) != 2 {
		t.Errorf("ScenariosForStory returned %d, want 2: %+v", len(scens), scens)
	}
}

func TestStatusPreparingStoriesIsValidAndInProgress(t *testing.T) {
	if !StatusPreparingStories.IsValid() {
		t.Errorf("StatusPreparingStories not in IsValid set")
	}
	if !StatusPreparingStories.IsInProgress() {
		t.Errorf("StatusPreparingStories should be IsInProgress (Sarah is running)")
	}
}

func TestStatusPreparingStoriesTransitions(t *testing.T) {
	cases := []struct {
		from, to Status
		want     bool
	}{
		{StatusArchitectureGenerated, StatusPreparingStories, true},
		{StatusPreparingStories, StatusReadyForExecution, true},
		{StatusPreparingStories, StatusArchitectureGenerated, true},
		{StatusPreparingStories, StatusRejected, true},
		// Legacy architecture→scenarios path still works
		{StatusArchitectureGenerated, StatusScenariosGenerated, true},
		// Reject jumps
		{StatusPreparingStories, StatusImplementing, false},
		{StatusPreparingStories, StatusCreated, false},
	}
	for _, tc := range cases {
		name := string(tc.from) + "→" + string(tc.to)
		t.Run(name, func(t *testing.T) {
			if got := tc.from.CanTransitionTo(tc.to); got != tc.want {
				t.Errorf("%s = %v, want %v", name, got, tc.want)
			}
		})
	}
}
