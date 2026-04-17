package prompt

import (
	"fmt"
	"sort"
	"strings"
)

// AssembledPrompt is the output of prompt assembly.
type AssembledPrompt struct {
	// SystemMessage is the composed system prompt.
	SystemMessage string

	// SystemMessageChars is the character count of SystemMessage (for context budget monitoring).
	SystemMessageChars int

	// UserMessage is the user message (task description, plan input, etc).
	UserMessage string

	// FragmentsUsed lists fragment IDs included in assembly (for observability).
	FragmentsUsed []string
}

// Assembler composes prompt fragments into system and user messages.
type Assembler struct {
	registry *Registry
}

// NewAssembler creates a new assembler backed by the given registry.
func NewAssembler(registry *Registry) *Assembler {
	return &Assembler{registry: registry}
}

// Assemble composes fragments into an AssembledPrompt.
// Fragments are filtered by context, sorted by category/priority,
// grouped by category, and formatted per provider conventions.
func (a *Assembler) Assemble(ctx *AssemblyContext) AssembledPrompt {
	fragments := a.registry.GetFragmentsForContext(ctx)

	// Inject persona fragment when configured.
	if ctx.Persona != nil && ctx.Persona.SystemPrompt != "" {
		fragments = append(fragments, &Fragment{
			ID:       "dynamic.persona",
			Category: CategoryPersona,
			Priority: 0,
			Content:  ctx.Persona.SystemPrompt,
		})
	}

	style := a.registry.GetStyle(ctx.Provider)

	var sections []string
	var usedIDs []string

	groups := groupByCategory(fragments)

	for _, cat := range sortedCategories(groups) {
		frags := groups[cat]
		label := categoryLabel(cat)

		var content strings.Builder
		for _, f := range frags {
			if content.Len() > 0 {
				content.WriteByte('\n')
			}
			var text string
			if f.ContentFunc != nil {
				text = f.ContentFunc(ctx)
			} else {
				text = f.Content
			}
			if text == "" {
				continue
			}
			content.WriteString(text)
			usedIDs = append(usedIDs, f.ID)
		}

		if content.Len() > 0 {
			sections = append(sections, FormatSection(label, content.String(), style))
		}
	}

	sysMsg := strings.Join(sections, "\n\n")
	return AssembledPrompt{
		SystemMessage:      sysMsg,
		SystemMessageChars: len(sysMsg),
		FragmentsUsed:      usedIDs,
	}
}

// FormatSection wraps content with provider-appropriate delimiters.
func FormatSection(label, content string, style ProviderStyle) string {
	if style.PreferXML {
		tag := strings.ReplaceAll(strings.ToLower(label), " ", "_")
		return fmt.Sprintf("<%s>\n%s\n</%s>", tag, content, tag)
	}
	if style.PreferMarkdown {
		return fmt.Sprintf("## %s\n%s", label, content)
	}
	return fmt.Sprintf("%s:\n%s", label, content)
}

// groupByCategory groups fragments into a map keyed by category.
func groupByCategory(fragments []*Fragment) map[Category][]*Fragment {
	groups := make(map[Category][]*Fragment)
	for _, f := range fragments {
		groups[f.Category] = append(groups[f.Category], f)
	}
	return groups
}

// sortedCategories returns category keys in ascending order.
func sortedCategories(groups map[Category][]*Fragment) []Category {
	cats := make([]Category, 0, len(groups))
	for cat := range groups {
		cats = append(cats, cat)
	}
	sort.Slice(cats, func(i, j int) bool { return cats[i] < cats[j] })
	return cats
}

// categoryLabel returns a human-readable label for a fragment category.
func categoryLabel(cat Category) string {
	switch cat {
	case CategorySystemBase:
		return "Identity"
	case CategoryToolDirective:
		return "Tool Directives"
	case CategoryProviderHints:
		return "Provider"
	case CategoryBehavioralGate:
		return "Behavioral Gates"
	case CategoryRoleContext:
		return "Role"
	case CategoryKnowledgeManifest:
		return "Knowledge"
	case CategoryPeerFeedback:
		return "Peer Feedback"
	case CategoryDomainContext:
		return "Domain"
	case CategoryPersona:
		return "Persona"
	case CategoryToolGuidance:
		return "Tool Guidance"
	case CategoryOutputFormat:
		return "Output Format"
	case CategoryGapDetection:
		return "Gap Detection"
	default:
		return "Context"
	}
}
