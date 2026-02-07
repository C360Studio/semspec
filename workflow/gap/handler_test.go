package gap

import (
	"testing"
)

func TestHandler_DetectOnly(t *testing.T) {
	// DetectOnly doesn't need NATS, just uses the parser
	h := &Handler{parser: NewParser()}

	content := `# Design
<gap>
  <topic>api.test</topic>
  <question>What is the API format?</question>
  <urgency>high</urgency>
</gap>

Some content here.`

	result := h.DetectOnly(content)

	if !result.HasGaps {
		t.Error("Expected HasGaps to be true")
	}

	if len(result.Gaps) != 1 {
		t.Fatalf("Expected 1 gap, got %d", len(result.Gaps))
	}

	if result.Gaps[0].Urgency != "high" {
		t.Errorf("Urgency = %q, want %q", result.Gaps[0].Urgency, "high")
	}
}

func TestHandler_HasBlockingGaps(t *testing.T) {
	h := &Handler{parser: NewParser()}

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "no gaps",
			content: "regular content",
			want:    false,
		},
		{
			name: "low urgency gap",
			content: `<gap>
  <question>Nice to know?</question>
  <urgency>low</urgency>
</gap>`,
			want: false,
		},
		{
			name: "normal urgency gap",
			content: `<gap>
  <question>Normal question?</question>
  <urgency>normal</urgency>
</gap>`,
			want: false,
		},
		{
			name: "high urgency gap",
			content: `<gap>
  <question>Important question?</question>
  <urgency>high</urgency>
</gap>`,
			want: true,
		},
		{
			name: "blocking urgency gap",
			content: `<gap>
  <question>Blocking question?</question>
  <urgency>blocking</urgency>
</gap>`,
			want: true,
		},
		{
			name: "mixed urgencies - has blocking",
			content: `<gap>
  <question>Low?</question>
  <urgency>low</urgency>
</gap>
<gap>
  <question>Blocking?</question>
  <urgency>blocking</urgency>
</gap>`,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := h.HasBlockingGaps(tt.content)
			if got != tt.want {
				t.Errorf("HasBlockingGaps() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHandler_Summarize(t *testing.T) {
	h := &Handler{parser: NewParser()}

	content := `# Design

<gap>
  <topic>api.users</topic>
  <question>User API format?</question>
  <urgency>normal</urgency>
</gap>

<gap>
  <topic>api.auth</topic>
  <question>Auth mechanism?</question>
  <urgency>high</urgency>
</gap>

<gap>
  <topic>api.users</topic>
  <question>Another user question?</question>
  <urgency>blocking</urgency>
</gap>

<gap>
  <topic>architecture.db</topic>
  <question>Database choice?</question>
  <urgency>low</urgency>
</gap>`

	summary := h.Summarize(content)

	if summary.TotalGaps != 4 {
		t.Errorf("TotalGaps = %d, want 4", summary.TotalGaps)
	}

	if summary.BlockingGaps != 2 {
		t.Errorf("BlockingGaps = %d, want 2 (high + blocking)", summary.BlockingGaps)
	}

	// Should have 3 unique topics (api.users appears twice)
	if len(summary.Topics) != 3 {
		t.Errorf("Topics count = %d, want 3", len(summary.Topics))
	}
}

func TestHandler_Summarize_NoGaps(t *testing.T) {
	h := &Handler{parser: NewParser()}

	summary := h.Summarize("No gaps here")

	if summary.TotalGaps != 0 {
		t.Errorf("TotalGaps = %d, want 0", summary.TotalGaps)
	}

	if summary.BlockingGaps != 0 {
		t.Errorf("BlockingGaps = %d, want 0", summary.BlockingGaps)
	}

	if len(summary.Topics) != 0 {
		t.Errorf("Topics should be empty, got %v", summary.Topics)
	}
}
