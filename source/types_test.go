package source

import (
	"testing"

	vocab "github.com/c360studio/semspec/vocabulary/source"
	"github.com/stretchr/testify/assert"
)

func TestAnalysisResult_IsValid(t *testing.T) {
	tests := []struct {
		name   string
		result *AnalysisResult
		want   bool
	}{
		{
			name:   "nil result",
			result: nil,
			want:   false,
		},
		{
			name:   "empty result",
			result: &AnalysisResult{},
			want:   false,
		},
		{
			name:   "with category only",
			result: &AnalysisResult{Category: "sop"},
			want:   true,
		},
		{
			name: "full result",
			result: &AnalysisResult{
				Category:     "sop",
				AppliesTo:    []string{"*.go"},
				Severity:     "error",
				Summary:      "Test",
				Requirements: []string{"rule1"},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.result.IsValid()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAnalysisResult_CategoryType(t *testing.T) {
	tests := []struct {
		category string
		want     vocab.DocCategoryType
	}{
		{"sop", vocab.DocCategorySOP},
		{"spec", vocab.DocCategorySpec},
		{"datasheet", vocab.DocCategoryDatasheet},
		{"reference", vocab.DocCategoryReference},
		{"api", vocab.DocCategoryAPI},
		{"unknown", vocab.DocCategoryReference}, // defaults to reference
		{"", vocab.DocCategoryReference},
	}

	for _, tt := range tests {
		t.Run(tt.category, func(t *testing.T) {
			a := &AnalysisResult{Category: tt.category}
			got := a.CategoryType()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAnalysisResult_SeverityType(t *testing.T) {
	tests := []struct {
		severity string
		want     vocab.DocSeverityType
	}{
		{"error", vocab.DocSeverityError},
		{"warning", vocab.DocSeverityWarning},
		{"info", vocab.DocSeverityInfo},
		{"unknown", vocab.DocSeverityInfo}, // defaults to info
		{"", vocab.DocSeverityInfo},
	}

	for _, tt := range tests {
		t.Run(tt.severity, func(t *testing.T) {
			a := &AnalysisResult{Category: "sop", Severity: tt.severity}
			got := a.SeverityType()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDocument_HasFrontmatter(t *testing.T) {
	t.Run("with frontmatter", func(t *testing.T) {
		d := &Document{
			Frontmatter: map[string]any{"key": "value"},
		}
		assert.True(t, d.HasFrontmatter())
	})

	t.Run("without frontmatter", func(t *testing.T) {
		d := &Document{}
		assert.False(t, d.HasFrontmatter())
	})

	t.Run("empty frontmatter", func(t *testing.T) {
		d := &Document{
			Frontmatter: map[string]any{},
		}
		assert.False(t, d.HasFrontmatter())
	})
}

func TestDocument_FrontmatterAsAnalysis_TypeConversions(t *testing.T) {
	t.Run("string slice for applies_to", func(t *testing.T) {
		d := &Document{
			Frontmatter: map[string]any{
				"category":   "sop",
				"applies_to": []string{"*.go", "*.ts"},
			},
		}
		a := d.FrontmatterAsAnalysis()
		assert.NotNil(t, a)
		assert.Equal(t, []string{"*.go", "*.ts"}, a.AppliesTo)
	})

	t.Run("any slice for requirements", func(t *testing.T) {
		d := &Document{
			Frontmatter: map[string]any{
				"category":     "sop",
				"requirements": []any{"rule1", "rule2"},
			},
		}
		a := d.FrontmatterAsAnalysis()
		assert.NotNil(t, a)
		assert.Equal(t, []string{"rule1", "rule2"}, a.Requirements)
	})

	t.Run("string slice for requirements", func(t *testing.T) {
		d := &Document{
			Frontmatter: map[string]any{
				"category":     "sop",
				"requirements": []string{"rule1", "rule2"},
			},
		}
		a := d.FrontmatterAsAnalysis()
		assert.NotNil(t, a)
		assert.Equal(t, []string{"rule1", "rule2"}, a.Requirements)
	})

	t.Run("mixed valid and invalid types in array", func(t *testing.T) {
		d := &Document{
			Frontmatter: map[string]any{
				"category":   "sop",
				"applies_to": []any{"*.go", 123, "*.ts"}, // 123 should be skipped
			},
		}
		a := d.FrontmatterAsAnalysis()
		assert.NotNil(t, a)
		assert.Equal(t, []string{"*.go", "*.ts"}, a.AppliesTo)
	})
}
