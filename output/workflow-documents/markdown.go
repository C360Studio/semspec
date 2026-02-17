package workflowdocuments

import (
	"fmt"
	"sort"
	"strings"
)

// Transformer converts structured document content to markdown.
type Transformer struct{}

// NewTransformer creates a new markdown transformer.
func NewTransformer() *Transformer {
	return &Transformer{}
}

// Transform converts DocumentContent to markdown string.
func (t *Transformer) Transform(content DocumentContent) string {
	var sb strings.Builder

	// Title as H1
	if content.Title != "" {
		sb.WriteString("# ")
		sb.WriteString(content.Title)
		sb.WriteString("\n\n")
	}

	// Process sections in a consistent order
	orderedSections := t.orderSections(content.Sections)

	for _, section := range orderedSections {
		t.writeSection(&sb, section.name, section.value, 2)
	}

	// Add status footer if present
	if content.Status != "" {
		sb.WriteString("---\n\n")
		sb.WriteString("**Status:** ")
		sb.WriteString(content.Status)
		sb.WriteString("\n")
	}

	return sb.String()
}

// sectionEntry holds a section name and value for ordering.
type sectionEntry struct {
	name  string
	value any
}

// orderSections returns sections in a consistent, logical order.
func (t *Transformer) orderSections(sections map[string]any) []sectionEntry {
	// Define preferred section order
	preferredOrder := []string{
		"why",
		"what_changes",
		"what",
		"overview",
		"architecture",
		"components",
		"api",
		"data_model",
		"scenarios",
		"acceptance_criteria",
		"tasks",
		"impact",
		"testing",
		"testing_required",
		"dependencies",
		"risks",
		"timeline",
		"notes",
	}

	orderMap := make(map[string]int)
	for i, name := range preferredOrder {
		orderMap[name] = i
	}

	// Convert sections map to slice
	entries := make([]sectionEntry, 0, len(sections))
	for name, value := range sections {
		entries = append(entries, sectionEntry{name: name, value: value})
	}

	// Sort by preferred order, then alphabetically for unknown sections
	sort.Slice(entries, func(i, j int) bool {
		orderI, okI := orderMap[entries[i].name]
		orderJ, okJ := orderMap[entries[j].name]

		if okI && okJ {
			return orderI < orderJ
		}
		if okI {
			return true
		}
		if okJ {
			return false
		}
		return entries[i].name < entries[j].name
	})

	return entries
}

// writeSection writes a section to the string builder.
func (t *Transformer) writeSection(sb *strings.Builder, name string, value any, level int) {
	// Convert section name to title case
	title := t.toTitleCase(name)

	// Write heading
	for i := 0; i < level; i++ {
		sb.WriteString("#")
	}
	sb.WriteString(" ")
	sb.WriteString(title)
	sb.WriteString("\n\n")

	// Write content based on type
	switch v := value.(type) {
	case string:
		sb.WriteString(v)
		sb.WriteString("\n\n")

	case []any:
		for _, item := range v {
			switch itemVal := item.(type) {
			case string:
				sb.WriteString("- ")
				sb.WriteString(itemVal)
				sb.WriteString("\n")
			case map[string]any:
				t.writeMapAsList(sb, itemVal)
			default:
				sb.WriteString("- ")
				sb.WriteString(fmt.Sprintf("%v", item))
				sb.WriteString("\n")
			}
		}
		sb.WriteString("\n")

	case map[string]any:
		// Check if this is a nested section or a simple key-value map
		if t.hasNestedSections(v) {
			// Nested sections - recurse with increased heading level
			subSections := t.orderSections(v)
			for _, sub := range subSections {
				t.writeSection(sb, sub.name, sub.value, level+1)
			}
		} else {
			// Simple key-value pairs - write as list
			t.writeMapAsList(sb, v)
			sb.WriteString("\n")
		}

	default:
		sb.WriteString(fmt.Sprintf("%v", value))
		sb.WriteString("\n\n")
	}
}

// hasNestedSections checks if a map contains nested sections (maps or arrays).
func (t *Transformer) hasNestedSections(m map[string]any) bool {
	for _, v := range m {
		switch v.(type) {
		case map[string]any, []any:
			return true
		}
	}
	return false
}

// writeMapAsList writes a map as a markdown list.
func (t *Transformer) writeMapAsList(sb *strings.Builder, m map[string]any) {
	// Get sorted keys for consistent output
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := m[k]
		title := t.toTitleCase(k)

		switch val := v.(type) {
		case []any:
			sb.WriteString("**")
			sb.WriteString(title)
			sb.WriteString(":**\n")
			for _, item := range val {
				sb.WriteString("  - ")
				sb.WriteString(fmt.Sprintf("%v", item))
				sb.WriteString("\n")
			}
		case string:
			sb.WriteString("- **")
			sb.WriteString(title)
			sb.WriteString(":** ")
			sb.WriteString(val)
			sb.WriteString("\n")
		default:
			sb.WriteString("- **")
			sb.WriteString(title)
			sb.WriteString(":** ")
			sb.WriteString(fmt.Sprintf("%v", v))
			sb.WriteString("\n")
		}
	}
}

// toTitleCase converts snake_case to Title Case.
func (t *Transformer) toTitleCase(s string) string {
	// Replace underscores with spaces
	s = strings.ReplaceAll(s, "_", " ")

	// Title case each word
	words := strings.Fields(s)
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(string(word[0])) + strings.ToLower(word[1:])
		}
	}

	return strings.Join(words, " ")
}

// TransformPlan provides plan-specific transformation.
func (t *Transformer) TransformPlan(content DocumentContent) string {
	// Ensure plan has expected sections
	if content.Sections == nil {
		content.Sections = make(map[string]any)
	}
	return t.Transform(content)
}

// TransformSpec provides spec-specific transformation with GIVEN/WHEN/THEN formatting.
func (t *Transformer) TransformSpec(content DocumentContent) string {
	// Pre-process scenarios to format GIVEN/WHEN/THEN
	if scenarios, ok := content.Sections["scenarios"].([]any); ok {
		formattedScenarios := make([]any, 0, len(scenarios))
		for _, scenario := range scenarios {
			if s, ok := scenario.(map[string]any); ok {
				formattedScenarios = append(formattedScenarios, t.formatScenario(s))
			} else {
				formattedScenarios = append(formattedScenarios, scenario)
			}
		}
		content.Sections["scenarios"] = formattedScenarios
	}
	return t.Transform(content)
}

// formatScenario formats a scenario map with GIVEN/WHEN/THEN.
func (t *Transformer) formatScenario(s map[string]any) string {
	var sb strings.Builder

	if name, ok := s["name"].(string); ok {
		sb.WriteString("**")
		sb.WriteString(name)
		sb.WriteString("**\n\n")
	}

	if given, ok := s["given"].(string); ok {
		sb.WriteString("**GIVEN** ")
		sb.WriteString(given)
		sb.WriteString("\n")
	}

	if when, ok := s["when"].(string); ok {
		sb.WriteString("**WHEN** ")
		sb.WriteString(when)
		sb.WriteString("\n")
	}

	if then, ok := s["then"].(string); ok {
		sb.WriteString("**THEN** ")
		sb.WriteString(then)
		sb.WriteString("\n")
	}

	return sb.String()
}

// TransformTasks provides tasks-specific transformation.
func (t *Transformer) TransformTasks(content DocumentContent) string {
	return t.Transform(content)
}
