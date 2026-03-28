package agentgraph_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/c360studio/semspec/agentgraph"
)

// hexInstance matches exactly 16 lowercase hex characters — the compact
// instance segment produced by workflow.HashInstanceID.
var hexInstance = regexp.MustCompile(`^[0-9a-f]{16}$`)

// assertEntityIDShape verifies that an entity ID:
//   - has exactly 6 dot-separated parts
//   - starts with the expected 5-part type prefix
//   - has an instance segment that is exactly 16 lowercase hex chars
func assertEntityIDShape(t *testing.T, funcName, gotID, wantPrefix string) {
	t.Helper()

	parts := strings.Split(gotID, ".")
	if len(parts) != 6 {
		t.Errorf("%s produced %d parts, want 6: %q", funcName, len(parts), gotID)
		return
	}

	prefix := strings.Join(parts[:5], ".")
	if prefix != wantPrefix {
		t.Errorf("%s prefix = %q, want %q", funcName, prefix, wantPrefix)
	}

	instance := parts[5]
	if !hexInstance.MatchString(instance) {
		t.Errorf("%s instance segment = %q, want 16 lowercase hex chars", funcName, instance)
	}
}

func TestLoopEntityID(t *testing.T) {
	t.Parallel()

	loopIDs := []struct {
		name   string
		loopID string
	}{
		{name: "simple alphanumeric loop ID", loopID: "abc123"},
		{name: "uuid-style loop ID", loopID: "550e8400-e29b-41d4-a716-446655440000"},
		{name: "single character loop ID", loopID: "1"},
	}

	prefix := agentgraph.LoopTypePrefix()
	for _, tc := range loopIDs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := agentgraph.LoopEntityID(tc.loopID)
			assertEntityIDShape(t, "LoopEntityID", got, prefix)
		})
	}
}

func TestLoopEntityID_SixParts(t *testing.T) {
	t.Parallel()

	got := agentgraph.LoopEntityID("myloop")
	parts := strings.Split(got, ".")
	if len(parts) != 6 {
		t.Errorf("LoopEntityID produced %d parts, want 6: %q", len(parts), got)
	}
}

func TestLoopEntityID_Deterministic(t *testing.T) {
	t.Parallel()

	id := "loop-deterministic"
	first := agentgraph.LoopEntityID(id)
	second := agentgraph.LoopEntityID(id)
	if first != second {
		t.Errorf("LoopEntityID(%q) is non-deterministic: %q vs %q", id, first, second)
	}
}

func TestLoopEntityID_DifferentIDsProduceDifferentEntityIDs(t *testing.T) {
	t.Parallel()

	ids := []string{"loop-1", "loop-2", "loop-3", "alpha", "beta"}
	seen := make(map[string]string)
	for _, loopID := range ids {
		eid := agentgraph.LoopEntityID(loopID)
		if prev, conflict := seen[eid]; conflict {
			t.Errorf("LoopEntityID collision: loopIDs %q and %q both produced %q", prev, loopID, eid)
		}
		seen[eid] = loopID
	}
}

func TestTaskEntityID(t *testing.T) {
	t.Parallel()

	taskIDs := []struct {
		name   string
		taskID string
	}{
		{name: "simple task ID", taskID: "task-001"},
		{name: "numeric task ID", taskID: "42"},
	}

	prefix := agentgraph.TaskTypePrefix()
	for _, tc := range taskIDs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := agentgraph.TaskEntityID(tc.taskID)
			assertEntityIDShape(t, "TaskEntityID", got, prefix)
		})
	}
}

func TestTaskEntityID_SixParts(t *testing.T) {
	t.Parallel()

	got := agentgraph.TaskEntityID("mytask")
	parts := strings.Split(got, ".")
	if len(parts) != 6 {
		t.Errorf("TaskEntityID produced %d parts, want 6: %q", len(parts), got)
	}
}

func TestDAGEntityID(t *testing.T) {
	t.Parallel()

	dagIDs := []struct {
		name  string
		dagID string
	}{
		{name: "simple dag ID", dagID: "dag-001"},
		{name: "uuid-style dag ID", dagID: "abcdef123456"},
	}

	prefix := agentgraph.DAGTypePrefix()
	for _, tc := range dagIDs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := agentgraph.DAGEntityID(tc.dagID)
			assertEntityIDShape(t, "DAGEntityID", got, prefix)
		})
	}
}

func TestLoopAndTaskEntityIDs_AreDistinct(t *testing.T) {
	t.Parallel()

	const sharedID = "same-id"
	loopEID := agentgraph.LoopEntityID(sharedID)
	taskEID := agentgraph.TaskEntityID(sharedID)
	if loopEID == taskEID {
		t.Errorf("LoopEntityID and TaskEntityID should differ for the same instance ID, both returned %q", loopEID)
	}
}

func TestLoopTaskDAGEntityIDs_AreAllDistinct(t *testing.T) {
	t.Parallel()

	const sharedID = "same-id"
	loopEID := agentgraph.LoopEntityID(sharedID)
	taskEID := agentgraph.TaskEntityID(sharedID)
	dagEID := agentgraph.DAGEntityID(sharedID)

	ids := []string{loopEID, taskEID, dagEID}
	seen := make(map[string]bool)
	for _, id := range ids {
		if seen[id] {
			t.Errorf("entity ID collision: %q produced by multiple entity ID functions", id)
		}
		seen[id] = true
	}
}

func TestLoopTypePrefix(t *testing.T) {
	t.Parallel()

	want := "semspec.local.agent.loop.loop"
	got := agentgraph.LoopTypePrefix()
	if got != want {
		t.Errorf("LoopTypePrefix() = %q, want %q", got, want)
	}
}

func TestTaskTypePrefix(t *testing.T) {
	t.Parallel()

	want := "semspec.local.agent.loop.task"
	got := agentgraph.TaskTypePrefix()
	if got != want {
		t.Errorf("TaskTypePrefix() = %q, want %q", got, want)
	}
}

func TestLoopTypePrefix_MatchesLoopEntityIDPrefix(t *testing.T) {
	t.Parallel()

	prefix := agentgraph.LoopTypePrefix()
	eid := agentgraph.LoopEntityID("some-loop")
	if !strings.HasPrefix(eid, prefix+".") {
		t.Errorf("LoopEntityID(%q) = %q does not start with LoopTypePrefix %q + \".\"", "some-loop", eid, prefix)
	}
}

func TestTaskTypePrefix_MatchesTaskEntityIDPrefix(t *testing.T) {
	t.Parallel()

	prefix := agentgraph.TaskTypePrefix()
	eid := agentgraph.TaskEntityID("some-task")
	if !strings.HasPrefix(eid, prefix+".") {
		t.Errorf("TaskEntityID(%q) = %q does not start with TaskTypePrefix %q + \".\"", "some-task", eid, prefix)
	}
}

func TestParseEntityID(t *testing.T) {
	t.Parallel()

	// With hashed instance IDs, ParseEntityID returns the 16-hex-char hash as the
	// instance — not the original input. Tests verify structural correctness
	// (ok=true and instance is a valid 16-char hex string) rather than
	// round-tripping to the original input ID.
	tests := []struct {
		name     string
		entityID string
		wantOK   bool
	}{
		{
			name:     "valid loop entity ID",
			entityID: agentgraph.LoopEntityID("abc123"),
			wantOK:   true,
		},
		{
			name:     "valid task entity ID",
			entityID: agentgraph.TaskEntityID("task-001"),
			wantOK:   true,
		},
		{
			name:     "valid DAG entity ID",
			entityID: agentgraph.DAGEntityID("dag-xyz"),
			wantOK:   true,
		},
		{
			name:     "malformed: too few parts",
			entityID: "semspec.local.agent",
			wantOK:   false,
		},
		{
			name:     "malformed: empty string",
			entityID: "",
			wantOK:   false,
		},
		{
			name:     "malformed: seven parts",
			entityID: "a.b.c.d.e.f.g",
			wantOK:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			instance, ok := agentgraph.ParseEntityID(tc.entityID)
			if ok != tc.wantOK {
				t.Fatalf("ParseEntityID(%q) ok = %v, want %v", tc.entityID, ok, tc.wantOK)
			}
			if tc.wantOK && !hexInstance.MatchString(instance) {
				t.Errorf("ParseEntityID(%q) instance = %q, want 16 lowercase hex chars", tc.entityID, instance)
			}
		})
	}
}

func TestValidateInstanceID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{name: "valid simple ID", id: "abc123", wantErr: false},
		{name: "valid hyphenated ID", id: "loop-abc-123", wantErr: false},
		{name: "valid UUID-style", id: "550e8400-e29b-41d4-a716-446655440000", wantErr: false},
		{name: "empty string", id: "", wantErr: true},
		{name: "contains dot", id: "has.dot", wantErr: true},
		{name: "multiple dots", id: "a.b.c", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := agentgraph.ValidateInstanceID(tc.id)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateInstanceID(%q) error = %v, wantErr %v", tc.id, err, tc.wantErr)
			}
		})
	}
}

// TestSystemPrefixesAreDistinct verifies that loop, roster, and team systems
// produce non-overlapping prefixes, preventing cross-system entity collisions.
func TestSystemPrefixesAreDistinct(t *testing.T) {
	t.Parallel()

	prefixes := []string{
		agentgraph.LoopTypePrefix(),
		agentgraph.TaskTypePrefix(),
		agentgraph.DAGTypePrefix(),
		agentgraph.AgentTypePrefix(),
		agentgraph.ReviewTypePrefix(),
		agentgraph.ErrorCategoryTypePrefix(),
		agentgraph.TeamTypePrefix(),
	}

	seen := make(map[string]bool)
	for _, p := range prefixes {
		if seen[p] {
			t.Errorf("type prefix collision: %q appears more than once", p)
		}
		seen[p] = true
	}
}

// TestAgentEntityID verifies roster agent IDs use the roster system and produce
// a valid 16-hex-char hashed instance segment.
func TestAgentEntityID(t *testing.T) {
	t.Parallel()

	got := agentgraph.AgentEntityID("agent-42")
	assertEntityIDShape(t, "AgentEntityID", got, agentgraph.AgentTypePrefix())
}

// TestErrorCategoryEntityID verifies errcat IDs use the roster system and
// produce a valid 16-hex-char hashed instance segment.
func TestErrorCategoryEntityID(t *testing.T) {
	t.Parallel()

	got := agentgraph.ErrorCategoryEntityID("missing-tests")
	assertEntityIDShape(t, "ErrorCategoryEntityID", got, agentgraph.ErrorCategoryTypePrefix())
}

// TestTeamEntityID verifies team IDs use the team system and produce
// a valid 16-hex-char hashed instance segment.
func TestTeamEntityID(t *testing.T) {
	t.Parallel()

	got := agentgraph.TeamEntityID("alpha")
	assertEntityIDShape(t, "TeamEntityID", got, agentgraph.TeamTypePrefix())
}

// TestTeamInsightEntityID verifies insight IDs use the team system and produce
// a valid 16-hex-char hashed instance segment derived from both teamID and insightID.
func TestTeamInsightEntityID(t *testing.T) {
	t.Parallel()

	got := agentgraph.TeamInsightEntityID("alpha", "ins-1")
	assertEntityIDShape(t, "TeamInsightEntityID", got, agentgraph.TeamInsightTypePrefix())
}

// TestTeamInsightEntityID_DifferentInputsProduceDifferentIDs verifies that
// different (teamID, insightID) pairs produce different entity IDs.
func TestTeamInsightEntityID_DifferentInputsProduceDifferentIDs(t *testing.T) {
	t.Parallel()

	pairs := [][2]string{
		{"alpha", "ins-1"},
		{"alpha", "ins-2"},
		{"beta", "ins-1"},
	}

	seen := make(map[string][2]string)
	for _, pair := range pairs {
		eid := agentgraph.TeamInsightEntityID(pair[0], pair[1])
		if prev, conflict := seen[eid]; conflict {
			t.Errorf("TeamInsightEntityID collision: (%q,%q) and (%q,%q) both produced %q",
				prev[0], prev[1], pair[0], pair[1], eid)
		}
		seen[eid] = pair
	}
}
