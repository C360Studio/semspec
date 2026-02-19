// Package git provides git operation tools for the Semspec agent.
// This file defines decision entity types for the "git-as-memory" pattern.
package git

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
)

func init() {
	err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "git",
		Category:    "decision",
		Version:     "v1",
		Description: "Git decision entity payload for tracking agent decisions via git commits",
		Factory:     func() any { return &DecisionEntityPayload{} },
	})
	if err != nil {
		panic("failed to register DecisionEntityPayload: " + err.Error())
	}
}

// DecisionEntityType is the message type for git decision entity payloads.
var DecisionEntityType = message.Type{Domain: "git", Category: "decision", Version: "v1"}

// DecisionEntityPayload implements message.Payload and graph.Graphable for decision entity ingestion.
// Each instance represents a single file changed in a git commit.
type DecisionEntityPayload struct {
	ID         string           `json:"id"`
	TripleData []message.Triple `json:"triples"`
	UpdatedAt  time.Time        `json:"updated_at"`

	// Fields for structured access (not stored in triples)
	FilePath   string `json:"file_path,omitempty"`
	CommitHash string `json:"commit_hash,omitempty"`
}

// EntityID returns the entity identifier for Graphable interface.
func (p *DecisionEntityPayload) EntityID() string { return p.ID }

// Triples returns the entity triples for Graphable interface.
func (p *DecisionEntityPayload) Triples() []message.Triple { return p.TripleData }

// Schema returns the message type for Payload interface.
func (p *DecisionEntityPayload) Schema() message.Type { return DecisionEntityType }

// Validate validates the payload for Payload interface.
func (p *DecisionEntityPayload) Validate() error {
	if p.ID == "" {
		return errors.New("entity ID is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *DecisionEntityPayload) MarshalJSON() ([]byte, error) {
	type Alias DecisionEntityPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *DecisionEntityPayload) UnmarshalJSON(data []byte) error {
	type Alias DecisionEntityPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// GenerateDecisionEntityID creates a unique entity ID for a file decision.
// Format: git.decision.{short_commit}.{path_hash}
// The path hash ensures uniqueness per file within a commit.
func GenerateDecisionEntityID(commitHash, filePath string) string {
	// Use short commit hash (7 chars)
	shortCommit := commitHash
	if len(shortCommit) > 7 {
		shortCommit = shortCommit[:7]
	}

	// Hash the file path to create a stable, URL-safe identifier
	pathHash := md5.Sum([]byte(filePath))
	pathHashStr := hex.EncodeToString(pathHash[:])[:8] // First 8 chars

	return "git.decision." + shortCommit + "." + pathHashStr
}

// NewDecisionEntityPayload creates a new decision entity payload.
func NewDecisionEntityPayload(commitHash, filePath string, triples []message.Triple) *DecisionEntityPayload {
	return &DecisionEntityPayload{
		ID:         GenerateDecisionEntityID(commitHash, filePath),
		TripleData: triples,
		UpdatedAt:  time.Now(),
		FilePath:   filePath,
		CommitHash: commitHash,
	}
}

// FileChangeInfo describes a file changed in a commit.
type FileChangeInfo struct {
	Path      string // File path relative to repo root
	Operation string // add, modify, delete, rename
}

// ParseFileOperation parses the git status code to determine operation type.
// Status codes from git diff-tree: A=added, M=modified, D=deleted, R=renamed
// Returns string values matching vocabulary/source.FileOperationType constants.
func ParseFileOperation(statusCode string) string {
	if len(statusCode) == 0 {
		return "modify"
	}
	switch statusCode[0] {
	case 'A':
		return "add"
	case 'D':
		return "delete"
	case 'R':
		return "rename"
	default:
		return "modify"
	}
}
