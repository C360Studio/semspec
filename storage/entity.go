// Package storage provides entity storage for semspec using NATS KV.
package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

// EntityType represents the type of entity stored in KV.
type EntityType string

const (
	EntityTypeProposal EntityType = "proposal"
	EntityTypeTask     EntityType = "task"
	EntityTypeResult   EntityType = "result"
)

// Bucket names for each entity type.
const (
	BucketProposals = "SEMSPEC_PROPOSALS"
	BucketTasks     = "SEMSPEC_TASKS"
	BucketResults   = "SEMSPEC_RESULTS"
)

// EntityID represents a typed entity identifier.
type EntityID struct {
	Type EntityType
	ID   string
}

// String returns the string representation of the entity ID.
func (e EntityID) String() string {
	return fmt.Sprintf("%s:%s", e.Type, e.ID)
}

// ParseEntityID parses an entity ID string into its components.
func ParseEntityID(s string) (EntityID, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return EntityID{}, fmt.Errorf("invalid entity ID format: %s", s)
	}
	entityType := EntityType(parts[0])
	switch entityType {
	case EntityTypeProposal, EntityTypeTask, EntityTypeResult:
		return EntityID{Type: entityType, ID: parts[1]}, nil
	default:
		return EntityID{}, fmt.Errorf("unknown entity type: %s", parts[0])
	}
}

// NewEntityID generates a new unique entity ID for the given type.
func NewEntityID(t EntityType) EntityID {
	return EntityID{
		Type: t,
		ID:   uuid.New().String(),
	}
}

// TaskStatus represents the status of a task.
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusComplete   TaskStatus = "complete"
	TaskStatusFailed     TaskStatus = "failed"
)

// Proposal represents a proposal entity.
type Proposal struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Task represents a task entity.
type Task struct {
	ID           string         `json:"id"`
	ProposalID   string         `json:"proposal_id"`
	Title        string         `json:"title"`
	Description  string         `json:"description"`
	Status       TaskStatus     `json:"status"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	StartedAt    *time.Time     `json:"started_at,omitempty"`
	CompletedAt  *time.Time     `json:"completed_at,omitempty"`
	StatusChange []StatusChange `json:"status_changes,omitempty"`
}

// StatusChange records a status transition.
type StatusChange struct {
	From      TaskStatus `json:"from"`
	To        TaskStatus `json:"to"`
	Timestamp time.Time  `json:"timestamp"`
}

// Artifact represents a file artifact in a result.
type Artifact struct {
	Path   string `json:"path"`
	Action string `json:"action"` // created, modified, deleted
	Hash   string `json:"hash,omitempty"`
}

// Result represents a task execution result.
type Result struct {
	ID        string     `json:"id"`
	TaskID    string     `json:"task_id"`
	Success   bool       `json:"success"`
	Output    string     `json:"output,omitempty"`
	Error     string     `json:"error,omitempty"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// Store provides entity storage operations backed by NATS KV.
type Store struct {
	proposals jetstream.KeyValue
	tasks     jetstream.KeyValue
	results   jetstream.KeyValue
}

// NewStore creates a new Store with the given JetStream context.
// It creates the necessary KV buckets if they don't exist.
func NewStore(ctx context.Context, js jetstream.JetStream) (*Store, error) {
	proposals, err := getOrCreateBucket(ctx, js, BucketProposals)
	if err != nil {
		return nil, fmt.Errorf("create proposals bucket: %w", err)
	}

	tasks, err := getOrCreateBucket(ctx, js, BucketTasks)
	if err != nil {
		return nil, fmt.Errorf("create tasks bucket: %w", err)
	}

	results, err := getOrCreateBucket(ctx, js, BucketResults)
	if err != nil {
		return nil, fmt.Errorf("create results bucket: %w", err)
	}

	return &Store{
		proposals: proposals,
		tasks:     tasks,
		results:   results,
	}, nil
}

func getOrCreateBucket(ctx context.Context, js jetstream.JetStream, name string) (jetstream.KeyValue, error) {
	kv, err := js.KeyValue(ctx, name)
	if err == nil {
		return kv, nil
	}
	// Bucket doesn't exist, create it
	return js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      name,
		Description: fmt.Sprintf("Semspec %s storage", strings.ToLower(name)),
		History:     5, // Keep last 5 revisions
	})
}

// CreateProposal creates a new proposal and returns its ID.
func (s *Store) CreateProposal(ctx context.Context, p *Proposal) (EntityID, error) {
	id := NewEntityID(EntityTypeProposal)
	p.ID = id.String()
	p.CreatedAt = time.Now()
	p.UpdatedAt = p.CreatedAt

	data, err := json.Marshal(p)
	if err != nil {
		return EntityID{}, fmt.Errorf("marshal proposal: %w", err)
	}

	if _, err := s.proposals.Create(ctx, id.ID, data); err != nil {
		return EntityID{}, fmt.Errorf("store proposal: %w", err)
	}

	return id, nil
}

// GetProposal retrieves a proposal by ID.
func (s *Store) GetProposal(ctx context.Context, id EntityID) (*Proposal, error) {
	if id.Type != EntityTypeProposal {
		return nil, fmt.Errorf("invalid entity type: expected proposal, got %s", id.Type)
	}

	entry, err := s.proposals.Get(ctx, id.ID)
	if err != nil {
		if isNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get proposal: %w", err)
	}

	var p Proposal
	if err := json.Unmarshal(entry.Value(), &p); err != nil {
		return nil, fmt.Errorf("unmarshal proposal: %w", err)
	}

	return &p, nil
}

// UpdateProposal updates an existing proposal.
func (s *Store) UpdateProposal(ctx context.Context, p *Proposal) error {
	id, err := ParseEntityID(p.ID)
	if err != nil {
		return fmt.Errorf("parse proposal ID: %w", err)
	}

	p.UpdatedAt = time.Now()

	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal proposal: %w", err)
	}

	if _, err := s.proposals.Put(ctx, id.ID, data); err != nil {
		return fmt.Errorf("update proposal: %w", err)
	}

	return nil
}

// ListProposals returns all proposals.
func (s *Store) ListProposals(ctx context.Context) ([]*Proposal, error) {
	keys, err := s.proposals.Keys(ctx)
	if err != nil {
		if err == jetstream.ErrNoKeysFound {
			return nil, nil
		}
		return nil, fmt.Errorf("list proposal keys: %w", err)
	}

	proposals := make([]*Proposal, 0, len(keys))
	for _, key := range keys {
		entry, err := s.proposals.Get(ctx, key)
		if err != nil {
			continue // Skip entries that fail to load
		}
		var p Proposal
		if err := json.Unmarshal(entry.Value(), &p); err != nil {
			continue
		}
		proposals = append(proposals, &p)
	}

	return proposals, nil
}

// CreateTask creates a new task and returns its ID.
func (s *Store) CreateTask(ctx context.Context, t *Task) (EntityID, error) {
	id := NewEntityID(EntityTypeTask)
	t.ID = id.String()
	t.Status = TaskStatusPending
	t.CreatedAt = time.Now()
	t.UpdatedAt = t.CreatedAt

	data, err := json.Marshal(t)
	if err != nil {
		return EntityID{}, fmt.Errorf("marshal task: %w", err)
	}

	if _, err := s.tasks.Create(ctx, id.ID, data); err != nil {
		return EntityID{}, fmt.Errorf("store task: %w", err)
	}

	return id, nil
}

// GetTask retrieves a task by ID.
func (s *Store) GetTask(ctx context.Context, id EntityID) (*Task, error) {
	if id.Type != EntityTypeTask {
		return nil, fmt.Errorf("invalid entity type: expected task, got %s", id.Type)
	}

	entry, err := s.tasks.Get(ctx, id.ID)
	if err != nil {
		if isNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get task: %w", err)
	}

	var t Task
	if err := json.Unmarshal(entry.Value(), &t); err != nil {
		return nil, fmt.Errorf("unmarshal task: %w", err)
	}

	return &t, nil
}

// UpdateTaskStatus updates a task's status and records the change.
func (s *Store) UpdateTaskStatus(ctx context.Context, id EntityID, newStatus TaskStatus) error {
	task, err := s.GetTask(ctx, id)
	if err != nil {
		return err
	}

	oldStatus := task.Status
	now := time.Now()

	task.Status = newStatus
	task.UpdatedAt = now
	task.StatusChange = append(task.StatusChange, StatusChange{
		From:      oldStatus,
		To:        newStatus,
		Timestamp: now,
	})

	// Track start/completion times
	if newStatus == TaskStatusInProgress && task.StartedAt == nil {
		task.StartedAt = &now
	}
	if newStatus == TaskStatusComplete || newStatus == TaskStatusFailed {
		task.CompletedAt = &now
	}

	parsedID, _ := ParseEntityID(task.ID)
	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("marshal task: %w", err)
	}

	if _, err := s.tasks.Put(ctx, parsedID.ID, data); err != nil {
		return fmt.Errorf("update task: %w", err)
	}

	return nil
}

// ListTasksByProposal returns all tasks for a given proposal.
func (s *Store) ListTasksByProposal(ctx context.Context, proposalID EntityID) ([]*Task, error) {
	keys, err := s.tasks.Keys(ctx)
	if err != nil {
		if err == jetstream.ErrNoKeysFound {
			return nil, nil
		}
		return nil, fmt.Errorf("list task keys: %w", err)
	}

	tasks := make([]*Task, 0)
	for _, key := range keys {
		entry, err := s.tasks.Get(ctx, key)
		if err != nil {
			continue
		}
		var t Task
		if err := json.Unmarshal(entry.Value(), &t); err != nil {
			continue
		}
		if t.ProposalID == proposalID.String() {
			tasks = append(tasks, &t)
		}
	}

	return tasks, nil
}

// CreateResult creates a new result and returns its ID.
func (s *Store) CreateResult(ctx context.Context, r *Result) (EntityID, error) {
	id := NewEntityID(EntityTypeResult)
	r.ID = id.String()
	r.CreatedAt = time.Now()

	data, err := json.Marshal(r)
	if err != nil {
		return EntityID{}, fmt.Errorf("marshal result: %w", err)
	}

	if _, err := s.results.Create(ctx, id.ID, data); err != nil {
		return EntityID{}, fmt.Errorf("store result: %w", err)
	}

	return id, nil
}

// GetResult retrieves a result by ID.
func (s *Store) GetResult(ctx context.Context, id EntityID) (*Result, error) {
	if id.Type != EntityTypeResult {
		return nil, fmt.Errorf("invalid entity type: expected result, got %s", id.Type)
	}

	entry, err := s.results.Get(ctx, id.ID)
	if err != nil {
		if isNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get result: %w", err)
	}

	var r Result
	if err := json.Unmarshal(entry.Value(), &r); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}

	return &r, nil
}

// GetResultByTask retrieves the result for a given task.
func (s *Store) GetResultByTask(ctx context.Context, taskID EntityID) (*Result, error) {
	keys, err := s.results.Keys(ctx)
	if err != nil {
		if err == jetstream.ErrNoKeysFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("list result keys: %w", err)
	}

	for _, key := range keys {
		entry, err := s.results.Get(ctx, key)
		if err != nil {
			continue
		}
		var r Result
		if err := json.Unmarshal(entry.Value(), &r); err != nil {
			continue
		}
		if r.TaskID == taskID.String() {
			return &r, nil
		}
	}

	return nil, ErrNotFound
}

// isNotFound checks if an error indicates a key was not found.
func isNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "key not found")
}
