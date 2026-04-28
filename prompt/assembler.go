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

	// UserMessage is the agent's user message, rendered from the role's
	// CategoryUserPrompt fragment (when one is registered). Empty when no
	// user-prompt fragment matched the role; existing callers that build their
	// own user prompt outside the registry can ignore this field.
	UserMessage string

	// UserPromptID identifies the fragment that produced UserMessage, for
	// telemetry and tests asserting the right template fired.
	UserPromptID string

	// FragmentsUsed lists fragment IDs included in assembly (for observability).
	FragmentsUsed []string

	// RenderError is the non-nil error returned by the user-prompt fragment's
	// render function, if any. Surfacing here (rather than panicking or being
	// returned alongside) lets callers fail loudly when they opt into the
	// user-prompt path while keeping the system-message-only path untouched.
	RenderError error
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
//
// CategoryUserPrompt fragments are handled separately: they do NOT contribute
// to the system message, and the matching role's fragment (if any) is rendered
// via Fragment.UserPrompt to produce AssembledPrompt.UserMessage.
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
	var userPromptFrag *Fragment

	groups := groupByCategory(fragments)

	for _, cat := range sortedCategories(groups) {
		// User-prompt fragments are NOT part of the system message — they
		// render the agent's user message instead. Pick out the role's
		// fragment here so the assembler still records it in FragmentsUsed
		// and renders it after the system-message loop.
		if cat == CategoryUserPrompt {
			for _, f := range groups[cat] {
				if userPromptFrag == nil {
					userPromptFrag = f
				}
				// Multiple matches mean a role/condition gating bug got past
				// the registry's uniqueness check (e.g., overlapping
				// Conditions on different fragments). Loud panic > silent
				// "first wins" because dial #1 already taught us how
				// expensive silent prompt mis-routing is.
				if userPromptFrag.ID != f.ID {
					panic(fmt.Sprintf(
						"prompt.Assembler: multiple CategoryUserPrompt fragments matched role %q — %q and %q. Tighten role/condition gating so exactly one matches per assembly.",
						ctx.Role, userPromptFrag.ID, f.ID,
					))
				}
			}
			continue
		}

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
	out := AssembledPrompt{
		SystemMessage:      sysMsg,
		SystemMessageChars: len(sysMsg),
		FragmentsUsed:      usedIDs,
	}

	// Render the user prompt last so it can read the same AssemblyContext the
	// system fragments composed against. Missing UserPrompt is a registration
	// bug — fragments in CategoryUserPrompt without a render function produce
	// silently-empty user messages, which is exactly the failure mode we're
	// trying to make impossible.
	if userPromptFrag != nil {
		if userPromptFrag.UserPrompt == nil {
			out.RenderError = fmt.Errorf(
				"prompt.Assembler: user-prompt fragment %q has nil UserPrompt — CategoryUserPrompt fragments must implement render",
				userPromptFrag.ID,
			)
			return out
		}
		userMsg, err := userPromptFrag.UserPrompt(ctx)
		out.UserPromptID = userPromptFrag.ID
		out.FragmentsUsed = append(out.FragmentsUsed, userPromptFrag.ID)
		if err != nil {
			out.RenderError = fmt.Errorf("user-prompt %q render: %w", userPromptFrag.ID, err)
			return out
		}
		out.UserMessage = userMsg
	}
	return out
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
