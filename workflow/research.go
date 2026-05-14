package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

// registerResearchPayloads registers the research request/answer payloads.
// Called by workflow.RegisterPayloads (entity.go).
//
// The research tool delegates upstream-API-surface discovery to a sub-agent
// (the "researcher"). The dev loop blocks on a research call; the researcher
// reads source/docs/specs in its OWN context window, distills the answer, and
// returns a compact summary + citations. The primary value is CONTEXT
// COMPACTION: a research summary replaces what would otherwise be many
// raw-source reads accumulating in the dev's context.
//
// See project_research_tool_plan_2026_05_14 for the full design and the
// take-23 trajectory data that motivated this addition.
func registerResearchPayloads(reg *payloadregistry.Registry) error {
	return errors.Join(
		reg.Register(&payloadregistry.Registration{
			Domain:      "research",
			Category:    "request",
			Version:     "v1",
			Description: "Research request payload — dev asks for upstream-API-surface investigation",
			Factory:     func() any { return &ResearchRequestPayload{} },
		}),
		reg.Register(&payloadregistry.Registration{
			Domain:      "research",
			Category:    "answer",
			Version:     "v1",
			Description: "Research answer payload — researcher returns distilled summary + citations",
			Factory:     func() any { return &ResearchAnswerPayload{} },
		}),
	)
}

// ResearchBucket is the KV bucket name for storing research records.
const ResearchBucket = "RESEARCH"

// MaxResearchAnswerBytes is the executor-layer cap on the size of a
// researcher's answer payload. Enforced at answer_research submission;
// answers larger than this are rejected so the researcher must distill
// further. Kept structural (not in the persona prompt) to avoid goodhart
// optimization on the metric itself — see [[verify-gate-before-blaming-model]]
// for prior session lessons on hardcoded numbers in prompts.
//
// 4 KiB ≈ 1000 tokens at typical English byte rates. Tuned for "fits in
// dev context comfortably even after several research calls."
const MaxResearchAnswerBytes = 4 * 1024

// ResearchStatus represents the lifecycle state of a research record.
type ResearchStatus string

const (
	// ResearchStatusPending — request written, researcher not yet dispatched.
	ResearchStatusPending ResearchStatus = "pending"
	// ResearchStatusInProgress — researcher dispatched, awaiting answer.
	ResearchStatusInProgress ResearchStatus = "in_progress"
	// ResearchStatusAnswered — researcher submitted; answer + citations populated.
	ResearchStatusAnswered ResearchStatus = "answered"
	// ResearchStatusTimeout — researcher exceeded its SLA before answering.
	ResearchStatusTimeout ResearchStatus = "timeout"
	// ResearchStatusError — researcher failed (loop error, validation rejected).
	ResearchStatusError ResearchStatus = "error"
)

// Citation is a pointer to upstream source material that backs an answer.
// Pointers, not pasted content — the dev re-fetches if they want raw bytes.
type Citation struct {
	// URL or File — exactly one must be set. URL for web sources (canonical
	// upstream GitHub raw URLs, docs sites). File for local paths (the
	// worktree itself, /tmp extractions). The two are mutually exclusive so
	// callers don't conflate provenance.
	URL  string `json:"url,omitempty"`
	File string `json:"file,omitempty"`

	// Lines is an optional line range hint (e.g. "45-52" or "120"). Lets
	// the dev jump to the exact span the researcher cited without
	// re-reading the entire file.
	Lines string `json:"lines,omitempty"`
}

// Validate checks that a citation has exactly one of URL or File set.
func (c *Citation) Validate() error {
	hasURL := c.URL != ""
	hasFile := c.File != ""
	switch {
	case hasURL && hasFile:
		return fmt.Errorf("citation: only one of url or file may be set")
	case !hasURL && !hasFile:
		return fmt.Errorf("citation: one of url or file is required")
	}
	return nil
}

// Research represents a research request and its eventual answer. Stored
// in the RESEARCH KV bucket keyed by ID. The state machine flows:
//
//	pending → in_progress → answered (happy path)
//	pending → in_progress → timeout  (researcher SLA exceeded)
//	pending → in_progress → error    (researcher loop failed or answer rejected)
type Research struct {
	// ID uniquely identifies this research request (format: research-{uuid}).
	ID string `json:"id"`

	// AskingLoopID is the dev loop that issued the `research` tool call and
	// is now blocked waiting for the answer.
	AskingLoopID string `json:"asking_loop_id"`

	// AskingCallID is the tool call_id from the dev's `research` invocation.
	// Used to synthesize the eventual ToolResult that unblocks the dev.
	AskingCallID string `json:"asking_call_id"`

	// Question is the specific question the dev wants answered.
	Question string `json:"question"`

	// Sources are hints from the dev about where to look (URLs, maven
	// coordinates, file path prefixes). The researcher MAY use other sources
	// if needed, but these are the dev's starting point.
	Sources []string `json:"sources,omitempty"`

	// Status is the current lifecycle state.
	Status ResearchStatus `json:"status"`

	// ResearcherLoopID is the sub-loop dispatched to do the investigation.
	// Set when status transitions from pending → in_progress.
	ResearcherLoopID string `json:"researcher_loop_id,omitempty"`

	// Answer is the researcher's distilled summary, populated on
	// status=answered. Size capped at MaxResearchAnswerBytes by the
	// answer_research executor.
	Answer string `json:"answer,omitempty"`

	// Citations are the source pointers backing the answer. Required on
	// status=answered.
	Citations []Citation `json:"citations,omitempty"`

	// Error describes the failure reason on status=error or timeout.
	Error string `json:"error,omitempty"`

	// TraceID correlates this research request with the dev's trace span.
	TraceID string `json:"trace_id,omitempty"`

	// PlanSlug is the plan the asking dev is working on, for routing /
	// display in any future researcher HTTP API.
	PlanSlug string `json:"plan_slug,omitempty"`

	// TaskID is the dev's task (node) the research is in service of.
	TaskID string `json:"task_id,omitempty"`

	// CreatedAt is when the dev issued the research call.
	CreatedAt time.Time `json:"created_at"`

	// DispatchedAt is when the researcher loop was dispatched.
	DispatchedAt *time.Time `json:"dispatched_at,omitempty"`

	// AnsweredAt is when the answer was recorded.
	AnsweredAt *time.Time `json:"answered_at,omitempty"`
}

// NewResearch creates a pending research record with a generated ID.
func NewResearch(askingLoopID, askingCallID, question string, sources []string) *Research {
	return &Research{
		ID:           fmt.Sprintf("research-%s", uuid.New().String()[:8]),
		AskingLoopID: askingLoopID,
		AskingCallID: askingCallID,
		Question:     question,
		Sources:      sources,
		Status:       ResearchStatusPending,
		CreatedAt:    time.Now().UTC(),
	}
}

// Validate checks structural invariants on the Research record. Called at
// every state transition site so an invalid record never reaches KV.
func (r *Research) Validate() error {
	if r.ID == "" {
		return fmt.Errorf("research.id is required")
	}
	if r.AskingLoopID == "" {
		return fmt.Errorf("research.asking_loop_id is required")
	}
	if r.AskingCallID == "" {
		return fmt.Errorf("research.asking_call_id is required")
	}
	if r.Question == "" {
		return fmt.Errorf("research.question is required")
	}
	switch r.Status {
	case ResearchStatusPending, ResearchStatusInProgress,
		ResearchStatusAnswered, ResearchStatusTimeout, ResearchStatusError:
		// known status
	default:
		return fmt.Errorf("research.status %q is not a known status", r.Status)
	}
	if r.Status == ResearchStatusAnswered {
		if r.Answer == "" {
			return fmt.Errorf("research.answer is required when status=answered")
		}
		if len(r.Answer) > MaxResearchAnswerBytes {
			return fmt.Errorf("research.answer is %d bytes (cap %d) — researcher must distill further",
				len(r.Answer), MaxResearchAnswerBytes)
		}
		if len(r.Citations) == 0 {
			return fmt.Errorf("research.citations is required when status=answered (no hallucination without sources)")
		}
		for i, c := range r.Citations {
			if err := c.Validate(); err != nil {
				return fmt.Errorf("research.citations[%d]: %w", i, err)
			}
		}
	}
	if (r.Status == ResearchStatusTimeout || r.Status == ResearchStatusError) && r.Error == "" {
		return fmt.Errorf("research.error is required when status=%s", r.Status)
	}
	return nil
}

// ResearchRequestPayload is published by the `research` tool executor to
// signal the researcher-manager that a new request awaits dispatch. The
// authoritative state lives in the RESEARCH KV bucket; this payload is
// just the kickoff event.
type ResearchRequestPayload struct {
	ResearchID string `json:"research_id"`
}

// ResearchAnswerPayload is published by the answer_research terminal tool
// when the researcher submits. Triggers researcher-manager to write the
// answer to RESEARCH KV and unblock the asking dev loop.
type ResearchAnswerPayload struct {
	ResearchID string     `json:"research_id"`
	Answer     string     `json:"answer"`
	Citations  []Citation `json:"citations,omitempty"`
}

// ResearchStore wraps the RESEARCH KV bucket with typed Get/Put helpers.
// Mirrors QuestionStore (workflow/question.go) so callers familiar with the
// question pattern can reach for the same shape.
type ResearchStore struct {
	nc     *natsclient.Client
	bucket jetstream.KeyValue
}

// NewResearchStore creates the RESEARCH KV bucket if needed and returns a
// store handle. Idempotent — safe to call from every component bootstrap that
// needs research access.
func NewResearchStore(nc *natsclient.Client) (*ResearchStore, error) {
	js, err := nc.JetStream()
	if err != nil {
		return nil, fmt.Errorf("get jetstream: %w", err)
	}

	bucket, err := js.CreateOrUpdateKeyValue(context.Background(), jetstream.KeyValueConfig{
		Bucket:      ResearchBucket,
		Description: "Research requests + answers from the sub-agent researcher delegation flow",
		TTL:         30 * 24 * time.Hour, // 30 days, matches QUESTIONS — research-by-plan retention
	})
	if err != nil {
		return nil, fmt.Errorf("create/update kv bucket %q: %w", ResearchBucket, err)
	}

	return &ResearchStore{
		nc:     nc,
		bucket: bucket,
	}, nil
}

// Bucket exposes the underlying KV handle for components that need to watch
// or scan the bucket directly (researcher-manager, the research tool
// executor). Read-only callers should use Get instead.
func (s *ResearchStore) Bucket() jetstream.KeyValue { return s.bucket }

// Get retrieves a Research record by ID. Returns an error wrapping
// jetstream.ErrKeyNotFound if the record doesn't exist.
func (s *ResearchStore) Get(ctx context.Context, id string) (*Research, error) {
	if id == "" {
		return nil, fmt.Errorf("research id is required")
	}
	entry, err := s.bucket.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("kv get %q: %w", id, err)
	}
	var r Research
	if err := json.Unmarshal(entry.Value(), &r); err != nil {
		return nil, fmt.Errorf("unmarshal research %q: %w", id, err)
	}
	return &r, nil
}

// Put writes a Research record to KV. Validates the record before write so
// no invalid state ever reaches the bucket. Returns the new revision.
func (s *ResearchStore) Put(ctx context.Context, r *Research) (uint64, error) {
	if r == nil {
		return 0, fmt.Errorf("research record is required")
	}
	if err := r.Validate(); err != nil {
		return 0, fmt.Errorf("validate research before put: %w", err)
	}
	data, err := json.Marshal(r)
	if err != nil {
		return 0, fmt.Errorf("marshal research: %w", err)
	}
	rev, err := s.bucket.Put(ctx, r.ID, data)
	if err != nil {
		return 0, fmt.Errorf("kv put %q: %w", r.ID, err)
	}
	return rev, nil
}
