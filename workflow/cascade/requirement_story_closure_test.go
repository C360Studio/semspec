package cascade

import (
	"reflect"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestExpandRequirementStoryClosure_WidensThroughMNStoryCoverage(t *testing.T) {
	reqs := []workflow.Requirement{
		{ID: "telemetry"},
		{ID: "control"},
		{ID: "async"},
		{ID: "consumer", DependsOn: []string{"async"}},
		{ID: "unrelated"},
	}
	stories := []workflow.Story{
		{ID: "story.mapper", RequirementIDs: []string{"telemetry", "control", "async"}},
		{ID: "story.unrelated", RequirementIDs: []string{"unrelated"}},
	}

	got := ExpandRequirementStoryClosure(reqs, stories, []string{"telemetry"})
	want := []string{"async", "consumer", "control", "telemetry"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExpandRequirementStoryClosure() = %v, want %v", got, want)
	}
}
