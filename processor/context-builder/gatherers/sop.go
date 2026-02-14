package gatherers

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// SOPGatherer gathers Standard Operating Procedures from the knowledge graph.
type SOPGatherer struct {
	graph           *GraphGatherer
	sopEntityPrefix string
}

// NewSOPGatherer creates a new SOP gatherer.
func NewSOPGatherer(graph *GraphGatherer, sopEntityPrefix string) *SOPGatherer {
	if sopEntityPrefix == "" {
		sopEntityPrefix = "source.doc"
	}
	return &SOPGatherer{
		graph:           graph,
		sopEntityPrefix: sopEntityPrefix,
	}
}

// SOPDocument represents a Standard Operating Procedure document.
type SOPDocument struct {
	ID        string
	Title     string
	Content   string
	AppliesTo string // Path pattern this SOP applies to (e.g., "api/**/*.go")
	Type      string // Document type (e.g., "sop", "guide", "convention")
	Tokens    int
}

// GetAllSOPs retrieves all SOP documents from the graph.
func (g *SOPGatherer) GetAllSOPs(ctx context.Context) ([]*SOPDocument, error) {
	entities, err := g.graph.QueryEntitiesByPredicate(ctx, g.sopEntityPrefix)
	if err != nil {
		return nil, fmt.Errorf("query SOPs: %w", err)
	}

	sops := make([]*SOPDocument, 0, len(entities))
	for _, e := range entities {
		sop := g.entityToSOP(e)
		if sop != nil {
			sops = append(sops, sop)
		}
	}

	return sops, nil
}

// GetSOPsForFiles finds SOPs that apply to the given file paths.
// Uses the "applies_to" pattern matching.
func (g *SOPGatherer) GetSOPsForFiles(ctx context.Context, files []string) ([]*SOPDocument, error) {
	allSOPs, err := g.GetAllSOPs(ctx)
	if err != nil {
		return nil, err
	}

	// Filter SOPs that apply to at least one of the files
	matching := make([]*SOPDocument, 0)
	seen := make(map[string]bool)

	for _, sop := range allSOPs {
		if sop.AppliesTo == "" {
			// SOP applies to all files if no pattern specified
			if !seen[sop.ID] {
				seen[sop.ID] = true
				matching = append(matching, sop)
			}
			continue
		}

		// Check if any file matches the pattern
		for _, file := range files {
			if g.matchesPattern(file, sop.AppliesTo) {
				if !seen[sop.ID] {
					seen[sop.ID] = true
					matching = append(matching, sop)
				}
				break
			}
		}
	}

	return matching, nil
}

// GetSOPContent returns formatted content for the given SOPs.
func (g *SOPGatherer) GetSOPContent(sops []*SOPDocument) (string, int, []string) {
	if len(sops) == 0 {
		return "", 0, nil
	}

	var sb strings.Builder
	totalTokens := 0
	ids := make([]string, 0, len(sops))

	sb.WriteString("# Standard Operating Procedures\n\n")
	sb.WriteString("The following SOPs apply to the files being reviewed:\n\n")

	for _, sop := range sops {
		sb.WriteString(fmt.Sprintf("## %s\n\n", sop.Title))
		if sop.AppliesTo != "" {
			sb.WriteString(fmt.Sprintf("**Applies to:** `%s`\n\n", sop.AppliesTo))
		}
		sb.WriteString(sop.Content)
		sb.WriteString("\n\n---\n\n")

		totalTokens += sop.Tokens
		ids = append(ids, sop.ID)
	}

	return sb.String(), totalTokens, ids
}

// TotalTokens returns the total token count for a set of SOPs.
func (g *SOPGatherer) TotalTokens(sops []*SOPDocument) int {
	total := 0
	for _, sop := range sops {
		total += sop.Tokens
	}
	return total
}

// entityToSOP converts a graph entity to an SOP document.
func (g *SOPGatherer) entityToSOP(e Entity) *SOPDocument {
	sop := &SOPDocument{
		ID: e.ID,
	}

	for _, t := range e.Triples {
		switch t.Predicate {
		case "source.doc.title", "dc.terms.title":
			if s, ok := t.Object.(string); ok {
				sop.Title = s
			}
		case "source.doc.content", "dc.terms.description":
			if s, ok := t.Object.(string); ok {
				sop.Content = s
			}
		case "source.doc.applies_to":
			if s, ok := t.Object.(string); ok {
				sop.AppliesTo = s
			}
		case "source.doc.type", "dc.terms.type":
			if s, ok := t.Object.(string); ok {
				sop.Type = s
			}
		}
	}

	// Estimate tokens (roughly 4 chars per token)
	sop.Tokens = (len(sop.Title) + len(sop.Content)) / 4

	// Skip if no content
	if sop.Content == "" && sop.Title == "" {
		return nil
	}

	return sop
}

// matchesPattern checks if a file path matches a glob-style pattern.
func (g *SOPGatherer) matchesPattern(file, pattern string) bool {
	// Handle common glob patterns

	// Exact match
	if file == pattern {
		return true
	}

	// ** matches any path
	if pattern == "**" || pattern == "**/*" {
		return true
	}

	// Handle patterns like "api/**/*.go"
	if strings.Contains(pattern, "**") {
		parts := strings.Split(pattern, "**")
		if len(parts) == 2 {
			prefix := strings.TrimSuffix(parts[0], "/")
			suffix := strings.TrimPrefix(parts[1], "/")

			// Check prefix
			if prefix != "" && !strings.HasPrefix(file, prefix) {
				return false
			}

			// Check suffix (file extension or pattern)
			if suffix != "" {
				if strings.HasPrefix(suffix, "*.") {
					ext := strings.TrimPrefix(suffix, "*")
					return strings.HasSuffix(file, ext)
				}
				return strings.HasSuffix(file, suffix)
			}

			return true
		}
	}

	// Use filepath.Match for simpler patterns
	matched, _ := filepath.Match(pattern, file)
	if matched {
		return true
	}

	// Also try matching just the filename
	matched, _ = filepath.Match(pattern, filepath.Base(file))
	return matched
}
