package storypreparer

import (
	"reflect"
	"testing"

	"github.com/c360studio/semspec/tools/schemaparity"
	"github.com/c360studio/semspec/tools/terminal"
)

// TestStoriesSchemaStructParity guards the story-preparer (Sarah) deliverable
// against the schema↔struct drift class (#125/#126/#267). The model's
// submit_work payload parses into the unexported positionalStoryInput /
// positionalTaskInput DTOs, so the guard lives in this package — terminal
// exposes only the canonical schema via SchemaForDeliverable.
func TestStoriesSchemaStructParity(t *testing.T) {
	props, err := schemaparity.Props(terminal.SchemaForDeliverable("stories"))
	if err != nil {
		t.Fatal(err)
	}
	storyItems, err := schemaparity.ItemsProps(props, "stories")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range schemaparity.Bidirectional("positionalStoryInput", reflect.TypeOf(positionalStoryInput{}), storyItems) {
		t.Error(v)
	}
	taskItems, err := schemaparity.ItemsProps(storyItems, "tasks")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range schemaparity.Bidirectional("positionalTaskInput", reflect.TypeOf(positionalTaskInput{}), taskItems) {
		t.Error(v)
	}
}
