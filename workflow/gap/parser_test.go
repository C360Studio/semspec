package gap

import (
	"strings"
	"testing"
)

func TestParse_SingleGap(t *testing.T) {
	content := `# Design Document

This is some content.

<gap>
  <topic>api.semstreams</topic>
  <question>Does LoopInfo include workflow_slug?</question>
  <context>Need to know available fields</context>
  <urgency>high</urgency>
</gap>

More content after the gap.`

	result := Parse(content)

	if !result.HasGaps {
		t.Error("Expected HasGaps to be true")
	}

	if len(result.Gaps) != 1 {
		t.Fatalf("Expected 1 gap, got %d", len(result.Gaps))
	}

	gap := result.Gaps[0]
	if gap.Topic != "api.semstreams" {
		t.Errorf("Topic = %q, want %q", gap.Topic, "api.semstreams")
	}
	if gap.Question != "Does LoopInfo include workflow_slug?" {
		t.Errorf("Question = %q, want %q", gap.Question, "Does LoopInfo include workflow_slug?")
	}
	if gap.Context != "Need to know available fields" {
		t.Errorf("Context = %q, want %q", gap.Context, "Need to know available fields")
	}
	if gap.Urgency != "high" {
		t.Errorf("Urgency = %q, want %q", gap.Urgency, "high")
	}

	// Check cleaned output doesn't contain gap block
	if strings.Contains(result.CleanedOutput, "<gap>") {
		t.Error("CleanedOutput should not contain <gap> tags")
	}
	if !strings.Contains(result.CleanedOutput, "This is some content") {
		t.Error("CleanedOutput should preserve other content")
	}
	if !strings.Contains(result.CleanedOutput, "More content after the gap") {
		t.Error("CleanedOutput should preserve content after gap")
	}
}

func TestParse_MultipleGaps(t *testing.T) {
	content := `# Design

<gap>
  <topic>api.authentication</topic>
  <question>What auth mechanism to use?</question>
</gap>

Some design content.

<gap>
  <topic>architecture.database</topic>
  <question>PostgreSQL or SQLite?</question>
  <urgency>blocking</urgency>
</gap>

Final content.`

	result := Parse(content)

	if !result.HasGaps {
		t.Error("Expected HasGaps to be true")
	}

	if len(result.Gaps) != 2 {
		t.Fatalf("Expected 2 gaps, got %d", len(result.Gaps))
	}

	// First gap
	if result.Gaps[0].Topic != "api.authentication" {
		t.Errorf("Gap 0 Topic = %q, want %q", result.Gaps[0].Topic, "api.authentication")
	}
	if result.Gaps[0].Urgency != "normal" { // Default urgency
		t.Errorf("Gap 0 Urgency = %q, want %q", result.Gaps[0].Urgency, "normal")
	}

	// Second gap
	if result.Gaps[1].Topic != "architecture.database" {
		t.Errorf("Gap 1 Topic = %q, want %q", result.Gaps[1].Topic, "architecture.database")
	}
	if result.Gaps[1].Urgency != "blocking" {
		t.Errorf("Gap 1 Urgency = %q, want %q", result.Gaps[1].Urgency, "blocking")
	}
}

func TestParse_NoGaps(t *testing.T) {
	content := `# Design Document

This is a complete design with no knowledge gaps.

## Components
Everything is well understood.`

	result := Parse(content)

	if result.HasGaps {
		t.Error("Expected HasGaps to be false")
	}

	if len(result.Gaps) != 0 {
		t.Errorf("Expected 0 gaps, got %d", len(result.Gaps))
	}

	if result.CleanedOutput != content {
		t.Error("CleanedOutput should equal original content when no gaps")
	}
}

func TestParse_MinimalGap(t *testing.T) {
	// Gap with only question (minimum required field)
	content := `Content before
<gap>
  <question>What is the answer?</question>
</gap>
Content after`

	result := Parse(content)

	if !result.HasGaps {
		t.Error("Expected HasGaps to be true")
	}

	if len(result.Gaps) != 1 {
		t.Fatalf("Expected 1 gap, got %d", len(result.Gaps))
	}

	gap := result.Gaps[0]
	if gap.Question != "What is the answer?" {
		t.Errorf("Question = %q, want %q", gap.Question, "What is the answer?")
	}
	if gap.Topic != "" {
		t.Errorf("Topic should be empty, got %q", gap.Topic)
	}
	if gap.Urgency != "normal" {
		t.Errorf("Urgency should default to 'normal', got %q", gap.Urgency)
	}
}

func TestParse_EmptyGap(t *testing.T) {
	// Gap with no question should be ignored
	content := `Content
<gap>
  <topic>something</topic>
</gap>
More content`

	result := Parse(content)

	// HasGaps is true because we found gap blocks
	if !result.HasGaps {
		t.Error("Expected HasGaps to be true (blocks were found)")
	}

	// But no valid gaps (no question)
	if len(result.Gaps) != 0 {
		t.Errorf("Expected 0 valid gaps (no question), got %d", len(result.Gaps))
	}
}

func TestParse_CaseInsensitive(t *testing.T) {
	content := `<GAP>
  <TOPIC>test.topic</TOPIC>
  <QUESTION>Is this case insensitive?</QUESTION>
</GAP>`

	result := Parse(content)

	if !result.HasGaps {
		t.Error("Expected HasGaps to be true")
	}

	if len(result.Gaps) != 1 {
		t.Fatalf("Expected 1 gap, got %d", len(result.Gaps))
	}

	if result.Gaps[0].Topic != "test.topic" {
		t.Errorf("Topic = %q, want %q", result.Gaps[0].Topic, "test.topic")
	}
}

func TestParse_InvalidUrgency(t *testing.T) {
	content := `<gap>
  <question>Test question</question>
  <urgency>invalid_value</urgency>
</gap>`

	result := Parse(content)

	if len(result.Gaps) != 1 {
		t.Fatalf("Expected 1 gap, got %d", len(result.Gaps))
	}

	// Should default to normal for invalid urgency
	if result.Gaps[0].Urgency != "normal" {
		t.Errorf("Urgency = %q, want %q (default)", result.Gaps[0].Urgency, "normal")
	}
}

func TestHasGaps(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "has gap",
			content: "text <gap><question>?</question></gap> more",
			want:    true,
		},
		{
			name:    "no gap",
			content: "just regular text",
			want:    false,
		},
		{
			name:    "gap-like but not gap",
			content: "the gap in knowledge is large",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasGaps(tt.content)
			if got != tt.want {
				t.Errorf("HasGaps() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCountGaps(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name    string
		content string
		want    int
	}{
		{
			name:    "no gaps",
			content: "regular content",
			want:    0,
		},
		{
			name:    "one gap",
			content: "<gap><question>?</question></gap>",
			want:    1,
		},
		{
			name:    "three gaps",
			content: "<gap><question>1</question></gap><gap><question>2</question></gap><gap><question>3</question></gap>",
			want:    3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.CountGaps(tt.content)
			if got != tt.want {
				t.Errorf("CountGaps() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCleanWhitespace(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "multiple newlines",
			input: "a\n\n\n\n\nb",
			want:  "a\n\nb",
		},
		{
			name:  "leading/trailing whitespace",
			input: "  \n\n  content  \n\n  ",
			want:  "content",
		},
		{
			name:  "already clean",
			input: "a\n\nb",
			want:  "a\n\nb",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanWhitespace(tt.input)
			if got != tt.want {
				t.Errorf("cleanWhitespace() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParse_RealWorldExample(t *testing.T) {
	content := `# Design: Add Authentication

## Technical Approach

We'll implement JWT-based authentication using the existing auth package.

<gap>
  <topic>api.semstreams.loop-info</topic>
  <question>Does the LoopInfo struct include workflow_slug and workflow_step fields?</question>
  <context>Need to track which workflow step a loop is associated with for proper state management</context>
  <urgency>high</urgency>
</gap>

## Components Affected

| Component | Change Type | Description |
|-----------|-------------|-------------|
| auth/handler.go | modified | Add JWT validation |
| middleware/auth.go | added | New auth middleware |

## Security Considerations

<gap>
  <topic>security.tokens</topic>
  <question>Should JWT tokens be stored in localStorage or httpOnly cookies?</question>
  <context>Need to balance security with ease of implementation</context>
  <urgency>blocking</urgency>
</gap>

We need to ensure tokens are securely stored and transmitted.`

	result := Parse(content)

	if !result.HasGaps {
		t.Error("Expected HasGaps to be true")
	}

	if len(result.Gaps) != 2 {
		t.Fatalf("Expected 2 gaps, got %d", len(result.Gaps))
	}

	// Verify first gap
	if result.Gaps[0].Topic != "api.semstreams.loop-info" {
		t.Errorf("Gap 0 Topic = %q, want %q", result.Gaps[0].Topic, "api.semstreams.loop-info")
	}
	if result.Gaps[0].Urgency != "high" {
		t.Errorf("Gap 0 Urgency = %q, want %q", result.Gaps[0].Urgency, "high")
	}

	// Verify second gap
	if result.Gaps[1].Topic != "security.tokens" {
		t.Errorf("Gap 1 Topic = %q, want %q", result.Gaps[1].Topic, "security.tokens")
	}
	if result.Gaps[1].Urgency != "blocking" {
		t.Errorf("Gap 1 Urgency = %q, want %q", result.Gaps[1].Urgency, "blocking")
	}

	// Verify cleaned output preserves document structure
	if !strings.Contains(result.CleanedOutput, "# Design: Add Authentication") {
		t.Error("CleanedOutput should preserve title")
	}
	if !strings.Contains(result.CleanedOutput, "## Components Affected") {
		t.Error("CleanedOutput should preserve section headers")
	}
	if strings.Contains(result.CleanedOutput, "<gap>") {
		t.Error("CleanedOutput should not contain gap tags")
	}
}
