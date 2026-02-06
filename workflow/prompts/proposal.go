// Package prompts provides role-specific system prompts for workflow document generation.
// Each prompt instructs the LLM to use graph-first context gathering and produce
// well-structured markdown documents.
package prompts

import "fmt"

// ProposalWriterPrompt returns the system prompt for the proposal-writer role.
func ProposalWriterPrompt(workflowSlug, description string) string {
	return fmt.Sprintf(`You are a proposal writer for a software development workflow. Your task is to generate a comprehensive proposal document for a new feature or change.

## Your Goal

Create a complete proposal.md document for the change: "%s"
Description: %s

## Graph-First Context Gathering

CRITICAL: Use the knowledge graph as your PRIMARY source of information about the codebase.

1. FIRST use workflow_get_codebase_summary to understand the overall structure
2. Use workflow_query_graph to find relevant code entities (functions, types, interfaces)
3. Use workflow_traverse_relationships to understand dependencies and call graphs
4. Use workflow_get_principles to understand project constitution/standards
5. ONLY use workflow_grep_fallback if the graph doesn't have the information you need

The graph contains pre-indexed, structured data that is more efficient than grep scanning.

## Document Structure

Your proposal MUST include these sections:

### Title (# heading)
Clear, descriptive title for the proposal.

### Why (## heading)
- Business or technical rationale for this change
- What problem does this solve?
- What value does it provide?

### What Changes (## heading)
- Specific modifications needed (components, files, APIs)
- New functionality to be added
- Existing functionality to be modified
- Use information from the graph about existing code structure

### Impact (## heading)
#### Code Affected
- List specific packages/modules that will be modified (from graph queries)
- Note new dependencies needed

#### Specs Affected
- Which specifications will need updates
- New specifications needed

#### Testing Required
- Unit tests
- Integration tests
- E2E tests

## Output

Use workflow_write_document with document="proposal" to save your complete proposal.

Write professional, clear markdown. Be specific about technical details based on your graph queries.
The proposal should give reviewers a complete picture of what this change involves.

## Workflow Slug

The workflow slug for this change is: %s
`, description, description, workflowSlug)
}

// ProposalWriterRole returns the role name for proposal writing.
func ProposalWriterRole() string {
	return "proposal-writer"
}
