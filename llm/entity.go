package llm

import (
	"fmt"
	"time"

	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semstreams/message"
)

// responsePreviewMaxLen is the maximum length of the response preview stored in graph.
const responsePreviewMaxLen = 500

// LLMCallEntity converts a CallRecord to graph triples.
type LLMCallEntity struct {
	record  *CallRecord
	org     string
	project string
}

// NewLLMCallEntity creates an entity from a CallRecord.
func NewLLMCallEntity(record *CallRecord, org, project string) *LLMCallEntity {
	return &LLMCallEntity{record: record, org: org, project: project}
}

// EntityID returns the 6-part entity identifier.
// Format: {org}.semspec.llm.call.{project}.{request_id}
func (e *LLMCallEntity) EntityID() string {
	return fmt.Sprintf("%s.semspec.llm.call.%s.%s", e.org, e.project, e.record.RequestID)
}

// Triples converts the CallRecord to graph triples.
func (e *LLMCallEntity) Triples() []message.Triple {
	id := e.EntityID()
	triples := []message.Triple{
		// Activity predicates (from agent.activity.*)
		{Subject: id, Predicate: semspec.PredicateActivityType, Object: "model_call"},
		{Subject: id, Predicate: semspec.ActivityModel, Object: e.record.Model},
		{Subject: id, Predicate: semspec.ActivityTokensIn, Object: e.record.PromptTokens},
		{Subject: id, Predicate: semspec.ActivityTokensOut, Object: e.record.CompletionTokens},
		{Subject: id, Predicate: semspec.ActivityDuration, Object: e.record.DurationMs},
		{Subject: id, Predicate: semspec.ActivitySuccess, Object: e.record.Error == ""},
		{Subject: id, Predicate: semspec.ActivityStartedAt, Object: e.record.StartedAt.Format(time.RFC3339)},
		{Subject: id, Predicate: semspec.ActivityEndedAt, Object: e.record.CompletedAt.Format(time.RFC3339)},

		// LLM-specific predicates (new)
		{Subject: id, Predicate: semspec.LLMCapability, Object: e.record.Capability},
		{Subject: id, Predicate: semspec.LLMProvider, Object: e.record.Provider},
		{Subject: id, Predicate: semspec.LLMFinishReason, Object: e.record.FinishReason},
		{Subject: id, Predicate: semspec.LLMRequestID, Object: e.record.RequestID},
	}

	// Optional predicates - loop association
	if e.record.LoopID != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: semspec.ActivityLoop, Object: e.record.LoopID})
	}

	// Optional predicates - trace correlation
	if e.record.TraceID != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: semspec.DCIdentifier, Object: e.record.TraceID})
	}

	// Optional predicates - error information
	if e.record.Error != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: semspec.ActivityError, Object: e.record.Error})
	}

	// Optional predicates - context budget tracking
	if e.record.ContextBudget > 0 {
		triples = append(triples, message.Triple{Subject: id, Predicate: semspec.LLMContextBudget, Object: e.record.ContextBudget})
	}
	if e.record.ContextTruncated {
		triples = append(triples, message.Triple{Subject: id, Predicate: semspec.LLMContextTruncated, Object: true})
	}

	// Optional predicates - retry/fallback tracking
	if e.record.Retries > 0 {
		triples = append(triples, message.Triple{Subject: id, Predicate: semspec.LLMRetries, Object: e.record.Retries})
	}
	for _, fallback := range e.record.FallbacksUsed {
		triples = append(triples, message.Triple{Subject: id, Predicate: semspec.LLMFallback, Object: fallback})
	}

	// Message count (always include if messages exist)
	if len(e.record.Messages) > 0 {
		triples = append(triples, message.Triple{Subject: id, Predicate: semspec.LLMMessagesCount, Object: len(e.record.Messages)})
	} else if e.record.MessagesCount > 0 {
		triples = append(triples, message.Triple{Subject: id, Predicate: semspec.LLMMessagesCount, Object: e.record.MessagesCount})
	}

	// Response preview (truncated for lightweight queries)
	if e.record.Response != "" {
		preview := e.record.Response
		if len(preview) > responsePreviewMaxLen {
			preview = preview[:responsePreviewMaxLen] + "..."
		}
		triples = append(triples, message.Triple{Subject: id, Predicate: semspec.LLMResponsePreview, Object: preview})
	} else if e.record.ResponsePreview != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: semspec.LLMResponsePreview, Object: e.record.ResponsePreview})
	}

	return triples
}
