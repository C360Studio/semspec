package gatherers

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	semspecVocab "github.com/c360studio/semspec/vocabulary/semspec"
	vocab "github.com/c360studio/semspec/vocabulary/source"
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
	ID             string
	Title          string
	Content        string
	AppliesTo      string   // Path pattern this SOP applies to (e.g., "api/**/*.go")
	Type           string   // Document type (e.g., "sop", "guide", "convention")
	Scope          string   // When this SOP applies: plan, code, or all
	Severity       string   // Violation severity: error, warning, or info
	Domains        []string // Semantic domains (e.g., ["auth", "security"])
	RelatedDomains []string // Related domains for cross-domain matching
	Keywords       []string // Extracted keywords for fuzzy matching
	Authority      bool     // Whether this is an authoritative source
	Tokens         int
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

// GetSOPsByScope retrieves SOPs that match the specified scope and patterns.
// scope should be "plan", "code", or "all". SOPs with scope="all" are always included.
// patterns are file patterns to match against applies_to (optional - if empty, no pattern filtering).
func (g *SOPGatherer) GetSOPsByScope(ctx context.Context, scope string, patterns []string) ([]*SOPDocument, error) {
	allSOPs, err := g.GetAllSOPs(ctx)
	if err != nil {
		return nil, err
	}

	matching := make([]*SOPDocument, 0)
	seen := make(map[string]bool)

	for _, sop := range allSOPs {
		// Check scope match: requested scope or "all"
		if !g.scopeMatches(sop.Scope, scope) {
			continue
		}

		// If no patterns provided, include all scope-matched SOPs
		if len(patterns) == 0 {
			if !seen[sop.ID] {
				seen[sop.ID] = true
				matching = append(matching, sop)
			}
			continue
		}

		// If SOP has no applies_to pattern, it applies universally
		if sop.AppliesTo == "" {
			if !seen[sop.ID] {
				seen[sop.ID] = true
				matching = append(matching, sop)
			}
			continue
		}

		// Check if any requested pattern matches the SOP's applies_to
		for _, pattern := range patterns {
			if g.patternsOverlap(pattern, sop.AppliesTo) {
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

// scopeMatches checks if an SOP scope matches the requested scope.
// "all" scope SOPs match any requested scope.
func (g *SOPGatherer) scopeMatches(sopScope, requestedScope string) bool {
	if sopScope == "all" {
		return true
	}
	return sopScope == requestedScope
}

// GetSOPsByDomain retrieves SOPs matching the given semantic domains.
// Also includes SOPs with related_domains that overlap with the requested domains.
// This enables domain-aware SOP matching: when touching auth code, find all
// auth-domain SOPs regardless of file path patterns.
func (g *SOPGatherer) GetSOPsByDomain(ctx context.Context, domains []string) ([]*SOPDocument, error) {
	if len(domains) == 0 {
		return nil, nil
	}

	allSOPs, err := g.GetAllSOPs(ctx)
	if err != nil {
		return nil, err
	}

	// Build a set of requested domains for fast lookup
	domainSet := make(map[string]bool)
	for _, d := range domains {
		domainSet[d] = true
	}

	matching := make([]*SOPDocument, 0)
	seen := make(map[string]bool)

	for _, sop := range allSOPs {
		if seen[sop.ID] {
			continue
		}

		// Check if any of the SOP's domains match
		for _, d := range sop.Domains {
			if domainSet[d] {
				seen[sop.ID] = true
				matching = append(matching, sop)
				break
			}
		}

		// Check if any of the SOP's related domains match
		if !seen[sop.ID] {
			for _, d := range sop.RelatedDomains {
				if domainSet[d] {
					seen[sop.ID] = true
					matching = append(matching, sop)
					break
				}
			}
		}
	}

	return matching, nil
}

// GetSOPsByKeywords retrieves SOPs with matching keywords.
// Uses case-insensitive matching with length-aware rules to reduce false positives.
func (g *SOPGatherer) GetSOPsByKeywords(ctx context.Context, keywords []string) ([]*SOPDocument, error) {
	if len(keywords) == 0 {
		return nil, nil
	}

	allSOPs, err := g.GetAllSOPs(ctx)
	if err != nil {
		return nil, err
	}

	// Normalize keywords to lowercase for matching
	normalizedKeywords := make([]string, len(keywords))
	for i, kw := range keywords {
		normalizedKeywords[i] = strings.ToLower(kw)
	}

	matching := make([]*SOPDocument, 0)
	seen := make(map[string]bool)

	for _, sop := range allSOPs {
		if seen[sop.ID] {
			continue
		}

		// Check if any SOP keyword matches any requested keyword
		for _, sopKw := range sop.Keywords {
			sopKwLower := strings.ToLower(sopKw)
			for _, reqKw := range normalizedKeywords {
				if keywordsMatch(sopKwLower, reqKw) {
					seen[sop.ID] = true
					matching = append(matching, sop)
					break
				}
			}
			if seen[sop.ID] {
				break
			}
		}
	}

	return matching, nil
}

// minKeywordLengthForSubstring is the minimum keyword length for substring matching.
// Keywords shorter than this require exact match to avoid false positives
// (e.g., "go" matching "mongo", "algorithm").
const minKeywordLengthForSubstring = 4

// keywordsMatch checks if two keywords match using length-aware rules.
// Short keywords require exact match; longer keywords allow substring matching.
func keywordsMatch(kw1, kw2 string) bool {
	// If either keyword is short, require exact match
	if len(kw1) < minKeywordLengthForSubstring || len(kw2) < minKeywordLengthForSubstring {
		return kw1 == kw2
	}
	// For longer keywords, allow substring match in either direction
	return strings.Contains(kw1, kw2) || strings.Contains(kw2, kw1)
}

// patternsOverlap checks if two glob patterns could match overlapping files.
// This is a conservative approximation - patterns that could potentially overlap return true.
func (g *SOPGatherer) patternsOverlap(pattern1, pattern2 string) bool {
	// If either pattern is universal, they overlap
	if pattern1 == "**" || pattern1 == "**/*" || pattern2 == "**" || pattern2 == "**/*" {
		return true
	}

	// Extract base directories from patterns
	dir1 := extractPatternDir(pattern1)
	dir2 := extractPatternDir(pattern2)

	// If one is a prefix of the other, they overlap
	if strings.HasPrefix(dir1, dir2) || strings.HasPrefix(dir2, dir1) {
		return true
	}

	// Check if patterns could match same extensions
	ext1 := extractPatternExtension(pattern1)
	ext2 := extractPatternExtension(pattern2)

	// If extensions differ (and both are specified), they don't overlap
	if ext1 != "" && ext2 != "" && ext1 != ext2 {
		return false
	}

	// Conservative: assume they could overlap
	return true
}

// extractPatternDir extracts the directory portion of a glob pattern.
func extractPatternDir(pattern string) string {
	// Remove ** and everything after
	if idx := strings.Index(pattern, "**"); idx > 0 {
		return strings.TrimSuffix(pattern[:idx], "/")
	}
	// Return directory portion
	dir := filepath.Dir(pattern)
	if dir == "." {
		return ""
	}
	return dir
}

// extractPatternExtension extracts the file extension from a glob pattern.
func extractPatternExtension(pattern string) string {
	// Look for *.ext pattern
	if idx := strings.LastIndex(pattern, "*."); idx >= 0 {
		return pattern[idx+1:]
	}
	return ""
}

// Default scope value for SOPs without explicit scope (backward compatible).
const defaultSOPScope = "code"

// entityToSOP converts a graph entity to an SOP document.
func (g *SOPGatherer) entityToSOP(e Entity) *SOPDocument {
	sop := &SOPDocument{
		ID:    e.ID,
		Scope: defaultSOPScope, // Default scope for backward compatibility
	}

	for _, t := range e.Triples {
		switch t.Predicate {
		case vocab.SourceName, semspecVocab.DCTitle, vocab.WebTitle:
			if s, ok := t.Object.(string); ok {
				sop.Title = s
			}
		case vocab.DocContent, semspecVocab.DCDescription, vocab.WebContent:
			if s, ok := t.Object.(string); ok {
				sop.Content = s
			}
		case vocab.DocAppliesTo, vocab.WebAppliesTo:
			if s, ok := t.Object.(string); ok {
				sop.AppliesTo = s
			}
		case vocab.DocType, semspecVocab.DCType, vocab.WebType:
			if s, ok := t.Object.(string); ok {
				sop.Type = s
			}
		case vocab.DocCategory, vocab.WebCategory:
			if s, ok := t.Object.(string); ok {
				sop.Type = s
			}
		case vocab.DocScope, vocab.WebScope:
			if s, ok := t.Object.(string); ok {
				sop.Scope = s
			}
		case vocab.DocSeverity, vocab.WebSeverity:
			if s, ok := t.Object.(string); ok {
				sop.Severity = s
			}
		case vocab.DocDomain, vocab.WebSemanticDomain:
			sop.Domains = extractStringArray(t.Object)
		case vocab.DocRelatedDomains, vocab.WebRelatedDomains:
			sop.RelatedDomains = extractStringArray(t.Object)
		case vocab.DocKeywords, vocab.WebKeywords:
			sop.Keywords = extractStringArray(t.Object)
		case vocab.SourceAuthority:
			if b, ok := t.Object.(bool); ok {
				sop.Authority = b
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

// extractStringArray extracts a string array from various possible object types.
func extractStringArray(obj any) []string {
	switch v := obj.(type) {
	case []string:
		return v
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case string:
		// Single string value
		if v != "" {
			return []string{v}
		}
	}
	return nil
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
