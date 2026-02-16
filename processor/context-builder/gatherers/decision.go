// Package gatherers provides context gathering implementations.
// This file implements the decision gatherer for retrieving git-as-memory
// decision context from the knowledge graph.
package gatherers

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Decision represents a git decision entity retrieved from the graph.
type Decision struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`       // feat, fix, refactor, etc.
	File      string    `json:"file"`       // File path
	Commit    string    `json:"commit"`     // Commit hash
	Message   string    `json:"message"`    // Commit message
	Branch    string    `json:"branch"`     // Branch name
	Agent     string    `json:"agent"`      // Agent ID if semspec-driven
	Loop      string    `json:"loop"`       // Loop ID if semspec-driven
	Timestamp time.Time `json:"timestamp"`  // When the decision was made
	Operation string    `json:"operation"`  // add, modify, delete, rename
}

// DecisionGatherer gathers git decision context from the knowledge graph.
type DecisionGatherer struct {
	graph *GraphGatherer
}

// NewDecisionGatherer creates a new decision gatherer.
func NewDecisionGatherer(graph *GraphGatherer) *DecisionGatherer {
	return &DecisionGatherer{
		graph: graph,
	}
}

// GatherForFiles retrieves decision history for the given files.
// Returns decisions ordered by timestamp (most recent first).
func (g *DecisionGatherer) GatherForFiles(ctx context.Context, files []string) ([]Decision, error) {
	if len(files) == 0 {
		return nil, nil
	}

	var allDecisions []Decision

	// Query decisions for each file
	for _, file := range files {
		decisions, err := g.queryDecisionsForFile(ctx, file)
		if err != nil {
			// Log error but continue gathering for other files
			continue
		}
		allDecisions = append(allDecisions, decisions...)
	}

	// Sort by timestamp (most recent first) - done client-side since
	// graph queries may not support sorting across predicates
	sortDecisionsByTimestamp(allDecisions)

	return allDecisions, nil
}

// GatherRecentDecisions retrieves the most recent N decisions across all files.
func (g *DecisionGatherer) GatherRecentDecisions(ctx context.Context, limit int) ([]Decision, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	// Query all decision entities
	entities, err := g.graph.QueryEntitiesByPredicate(ctx, "source.git.decision")
	if err != nil {
		return nil, fmt.Errorf("query decisions: %w", err)
	}

	decisions := make([]Decision, 0, len(entities))
	for _, e := range entities {
		decision := entityToDecision(e)
		decisions = append(decisions, decision)
	}

	// Sort and limit
	sortDecisionsByTimestamp(decisions)
	if len(decisions) > limit {
		decisions = decisions[:limit]
	}

	return decisions, nil
}

// GatherDecisionsByCommit retrieves all decisions for a specific commit.
func (g *DecisionGatherer) GatherDecisionsByCommit(ctx context.Context, commitHash string) ([]Decision, error) {
	// Query all decision entities and filter by commit
	entities, err := g.graph.QueryEntitiesByPredicate(ctx, "source.git.decision")
	if err != nil {
		return nil, fmt.Errorf("query decisions: %w", err)
	}

	var decisions []Decision
	for _, e := range entities {
		commit := getPredicateString(e, "source.git.decision.commit")
		// Match full or short hash
		if strings.HasPrefix(commitHash, commit) || strings.HasPrefix(commit, commitHash) {
			decisions = append(decisions, entityToDecision(e))
		}
	}

	return decisions, nil
}

// GatherDecisionsByType retrieves decisions of a specific type (feat, fix, etc.).
func (g *DecisionGatherer) GatherDecisionsByType(ctx context.Context, decisionType string, limit int) ([]Decision, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	// Query all decision entities and filter by type
	entities, err := g.graph.QueryEntitiesByPredicate(ctx, "source.git.decision")
	if err != nil {
		return nil, fmt.Errorf("query decisions: %w", err)
	}

	var decisions []Decision
	for _, e := range entities {
		dt := getPredicateString(e, "source.git.decision.type")
		if dt == decisionType {
			decisions = append(decisions, entityToDecision(e))
		}
	}

	sortDecisionsByTimestamp(decisions)
	if len(decisions) > limit {
		decisions = decisions[:limit]
	}

	return decisions, nil
}

// FormatDecisionsAsContext formats decisions as markdown for LLM context.
func FormatDecisionsAsContext(decisions []Decision, maxDecisions int) string {
	if len(decisions) == 0 {
		return ""
	}

	if maxDecisions > 0 && len(decisions) > maxDecisions {
		decisions = decisions[:maxDecisions]
	}

	var sb strings.Builder
	sb.WriteString("## Previous Decisions for These Files\n\n")

	for _, d := range decisions {
		sb.WriteString(fmt.Sprintf("### %s (%s)\n", d.Message, d.Commit))
		sb.WriteString(fmt.Sprintf("- **File**: %s\n", d.File))
		sb.WriteString(fmt.Sprintf("- **Type**: %s\n", d.Type))
		if d.Operation != "" {
			sb.WriteString(fmt.Sprintf("- **Operation**: %s\n", d.Operation))
		}
		if !d.Timestamp.IsZero() {
			sb.WriteString(fmt.Sprintf("- **When**: %s\n", d.Timestamp.Format(time.RFC3339)))
		}
		if d.Agent != "" {
			sb.WriteString(fmt.Sprintf("- **Agent**: %s\n", d.Agent))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// queryDecisionsForFile queries decisions for a specific file path.
func (g *DecisionGatherer) queryDecisionsForFile(ctx context.Context, filePath string) ([]Decision, error) {
	// Query all decision entities and filter by file path
	entities, err := g.graph.QueryEntitiesByPredicate(ctx, "source.git.decision")
	if err != nil {
		return nil, err
	}

	var decisions []Decision
	for _, e := range entities {
		file := getPredicateString(e, "source.git.decision.file")
		if file == filePath {
			decisions = append(decisions, entityToDecision(e))
		}
	}

	return decisions, nil
}

// entityToDecision converts a graph entity to a Decision struct.
func entityToDecision(e Entity) Decision {
	d := Decision{
		ID:        e.ID,
		Type:      getPredicateString(e, "source.git.decision.type"),
		File:      getPredicateString(e, "source.git.decision.file"),
		Commit:    getPredicateString(e, "source.git.decision.commit"),
		Message:   getPredicateString(e, "source.git.decision.message"),
		Branch:    getPredicateString(e, "source.git.decision.branch"),
		Agent:     getPredicateString(e, "source.git.decision.agent"),
		Loop:      getPredicateString(e, "source.git.decision.loop"),
		Operation: getPredicateString(e, "source.git.decision.operation"),
	}

	// Parse timestamp
	if ts := getPredicateString(e, "source.git.decision.timestamp"); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			d.Timestamp = t
		}
	}

	return d
}

// getPredicateString extracts a string value from an entity's triples.
func getPredicateString(e Entity, predicate string) string {
	for _, t := range e.Triples {
		if t.Predicate == predicate {
			if s, ok := t.Object.(string); ok {
				return s
			}
		}
	}
	return ""
}

// sortDecisionsByTimestamp sorts decisions by timestamp (most recent first).
func sortDecisionsByTimestamp(decisions []Decision) {
	sort.Slice(decisions, func(i, j int) bool {
		return decisions[j].Timestamp.Before(decisions[i].Timestamp)
	})
}
