// Package graph provides utilities for publishing entities to the knowledge graph.
package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
)

// Subject for graph ingestion.
const GraphIngestSubject = "graph.ingest.entity"

// EntityIngestMessage is the message format for graph ingestion.
// Matches the format used by other semspec/semstreams components.
type EntityIngestMessage struct {
	ID        string            `json:"id"`
	Triples   []message.Triple  `json:"triples"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// PublishProposal publishes a change/proposal entity to the knowledge graph.
func PublishProposal(ctx context.Context, nc *natsclient.Client, change *workflow.Change) error {
	if nc == nil {
		return nil // Skip publishing if no NATS client (graceful degradation)
	}

	entityID := ProposalEntityID(change.Slug)
	now := time.Now()

	triples := []message.Triple{
		{
			Subject:    entityID,
			Predicate:  semspec.ProposalTitle,
			Object:     change.Title,
			Source:     "semspec.propose",
			Timestamp:  now,
			Confidence: 1.0,
		},
		{
			Subject:    entityID,
			Predicate:  semspec.ProposalSlug,
			Object:     change.Slug,
			Source:     "semspec.propose",
			Timestamp:  now,
			Confidence: 1.0,
		},
		{
			Subject:    entityID,
			Predicate:  semspec.PredicateProposalStatus,
			Object:     string(change.Status),
			Source:     "semspec.propose",
			Timestamp:  now,
			Confidence: 1.0,
		},
		{
			Subject:    entityID,
			Predicate:  semspec.ProposalAuthor,
			Object:     change.Author,
			Source:     "semspec.propose",
			Timestamp:  now,
			Confidence: 1.0,
		},
		{
			Subject:    entityID,
			Predicate:  semspec.ProposalCreatedAt,
			Object:     change.CreatedAt.Format(time.RFC3339),
			Source:     "semspec.propose",
			Timestamp:  now,
			Confidence: 1.0,
		},
		{
			Subject:    entityID,
			Predicate:  semspec.ProposalUpdatedAt,
			Object:     change.UpdatedAt.Format(time.RFC3339),
			Source:     "semspec.propose",
			Timestamp:  now,
			Confidence: 1.0,
		},
		// File status predicates
		{
			Subject:    entityID,
			Predicate:  semspec.ProposalHasProposal,
			Object:     change.Files.HasProposal,
			Source:     "semspec.propose",
			Timestamp:  now,
			Confidence: 1.0,
		},
		{
			Subject:    entityID,
			Predicate:  semspec.ProposalHasDesign,
			Object:     change.Files.HasDesign,
			Source:     "semspec.propose",
			Timestamp:  now,
			Confidence: 1.0,
		},
		{
			Subject:    entityID,
			Predicate:  semspec.ProposalHasSpec,
			Object:     change.Files.HasSpec,
			Source:     "semspec.propose",
			Timestamp:  now,
			Confidence: 1.0,
		},
		{
			Subject:    entityID,
			Predicate:  semspec.ProposalHasTasks,
			Object:     change.Files.HasTasks,
			Source:     "semspec.propose",
			Timestamp:  now,
			Confidence: 1.0,
		},
	}

	// Add GitHub integration predicates if present
	if change.GitHub != nil && change.GitHub.EpicNumber > 0 {
		triples = append(triples, message.Triple{
			Subject:    entityID,
			Predicate:  semspec.ProposalGitHubEpic,
			Object:     change.GitHub.EpicNumber,
			Source:     "semspec.propose",
			Timestamp:  now,
			Confidence: 1.0,
		})
		triples = append(triples, message.Triple{
			Subject:    entityID,
			Predicate:  semspec.ProposalGitHubRepo,
			Object:     change.GitHub.Repository,
			Source:     "semspec.propose",
			Timestamp:  now,
			Confidence: 1.0,
		})
	}

	msg := EntityIngestMessage{
		ID:        entityID,
		Triples:   triples,
		UpdatedAt: now,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal proposal entity: %w", err)
	}

	if err := nc.PublishToStream(ctx, GraphIngestSubject, data); err != nil {
		return fmt.Errorf("publish proposal entity: %w", err)
	}

	return nil
}

// ProposalEntityID generates a consistent entity ID for a proposal.
// Format: semspec.local.workflow.proposal.<slug>
func ProposalEntityID(slug string) string {
	return fmt.Sprintf("semspec.local.workflow.proposal.proposal.%s", slug)
}

// SpecEntityID generates a consistent entity ID for a spec.
// Format: semspec.local.workflow.spec.<slug>
func SpecEntityID(slug string) string {
	return fmt.Sprintf("semspec.local.workflow.spec.spec.%s", slug)
}

// TaskEntityID generates a consistent entity ID for a task.
// Format: semspec.local.workflow.task.<slug>-<index>
func TaskEntityID(slug string, index int) string {
	return fmt.Sprintf("semspec.local.workflow.task.task.%s-%d", slug, index)
}
