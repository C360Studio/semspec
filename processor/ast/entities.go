package ast

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/c360/semstreams/message"
)

// CodeEntity represents a code artifact extracted from AST parsing.
// It provides methods to convert to graph triples for storage.
type CodeEntity struct {
	// ID is the 6-part entity identifier
	// Format: {org}.semspec.code.{type}.{project}.{instance}
	ID string

	// Type classifies the code entity
	Type CodeEntityType

	// Name is the identifier (function name, type name, etc.)
	Name string

	// Path is the file path relative to repo root
	Path string

	// Package is the Go package name
	Package string

	// Visibility indicates if exported
	Visibility Visibility

	// Location in source
	StartLine int
	EndLine   int

	// Content hash for change detection
	Hash string

	// Documentation comment
	DocComment string

	// Relationships to other entities (entity IDs)
	ContainedBy string   // parent entity ID
	Contains    []string // child entity IDs
	Imports     []string // import paths
	Implements  []string // interface entity IDs
	Embeds      []string // embedded type entity IDs
	Calls       []string // called function entity IDs
	References  []string // type reference entity IDs
	Returns     []string // return type entity IDs
	Receiver    string   // receiver type entity ID (for methods)
	Parameters  []string // parameter type entity IDs

	// Timestamps
	IndexedAt time.Time
}

// NewCodeEntity creates a new code entity with the given parameters.
// The project parameter is used to construct the 6-part entity ID.
func NewCodeEntity(org, project string, entityType CodeEntityType, name, path string) *CodeEntity {
	// Build instance identifier from path and name
	instance := buildInstanceID(path, name, entityType)

	return &CodeEntity{
		ID:         fmt.Sprintf("%s.semspec.code.%s.%s.%s", org, entityType, project, instance),
		Type:       entityType,
		Name:       name,
		Path:       path,
		Visibility: determineVisibility(name),
		IndexedAt:  time.Now(),
	}
}

// buildInstanceID creates a unique instance identifier from path and name
func buildInstanceID(path, name string, entityType CodeEntityType) string {
	// Sanitize for use in entity ID (replace invalid characters)
	sanitized := strings.ReplaceAll(path, "/", "-")
	sanitized = strings.ReplaceAll(sanitized, ".", "-")
	sanitized = strings.TrimPrefix(sanitized, "-")

	if name != "" && entityType != TypeFile && entityType != TypePackage {
		// For functions, types, etc: include name
		return fmt.Sprintf("%s-%s", sanitized, name)
	}
	return sanitized
}

// determineVisibility checks if a Go identifier is exported
func determineVisibility(name string) Visibility {
	if name == "" {
		return VisibilityPrivate
	}
	r := []rune(name)
	if len(r) > 0 && unicode.IsUpper(r[0]) {
		return VisibilityPublic
	}
	return VisibilityPrivate
}

// ComputeHash computes a SHA256 hash of the given content
func ComputeHash(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:8]) // First 8 bytes for brevity
}

// Triples converts the CodeEntity to a slice of message.Triple for graph storage.
// All semantic properties are stored as triples using vocabulary predicates.
func (e *CodeEntity) Triples() []message.Triple {
	triples := make([]message.Triple, 0, 20)

	// Identity predicates
	triples = append(triples,
		message.Triple{Subject: e.ID, Predicate: CodeType, Object: string(e.Type)},
		message.Triple{Subject: e.ID, Predicate: DcTitle, Object: e.Name},
	)

	if e.Path != "" {
		triples = append(triples,
			message.Triple{Subject: e.ID, Predicate: CodePath, Object: e.Path})
	}

	if e.Package != "" {
		triples = append(triples,
			message.Triple{Subject: e.ID, Predicate: CodePackage, Object: e.Package})
	}

	if e.Hash != "" {
		triples = append(triples,
			message.Triple{Subject: e.ID, Predicate: CodeHash, Object: e.Hash})
	}

	// Always Go language for this parser
	triples = append(triples,
		message.Triple{Subject: e.ID, Predicate: CodeLanguage, Object: "go"})

	// Classification
	triples = append(triples,
		message.Triple{Subject: e.ID, Predicate: CodeVisibility, Object: string(e.Visibility)})

	// Location
	if e.StartLine > 0 {
		triples = append(triples,
			message.Triple{Subject: e.ID, Predicate: CodeStartLine, Object: e.StartLine})
	}
	if e.EndLine > 0 {
		triples = append(triples,
			message.Triple{Subject: e.ID, Predicate: CodeEndLine, Object: e.EndLine})
	}
	if e.StartLine > 0 && e.EndLine > 0 {
		triples = append(triples,
			message.Triple{Subject: e.ID, Predicate: CodeLines, Object: e.EndLine - e.StartLine + 1})
	}

	// Documentation
	if e.DocComment != "" {
		triples = append(triples,
			message.Triple{Subject: e.ID, Predicate: CodeDocComment, Object: e.DocComment})
	}

	// Structure relationships
	if e.ContainedBy != "" {
		triples = append(triples,
			message.Triple{Subject: e.ID, Predicate: CodeBelongsTo, Object: e.ContainedBy})
	}
	for _, childID := range e.Contains {
		triples = append(triples,
			message.Triple{Subject: e.ID, Predicate: CodeContains, Object: childID})
	}

	// Dependency relationships
	for _, importPath := range e.Imports {
		triples = append(triples,
			message.Triple{Subject: e.ID, Predicate: CodeImports, Object: importPath})
	}

	// Semantic relationships
	for _, implID := range e.Implements {
		triples = append(triples,
			message.Triple{Subject: e.ID, Predicate: CodeImplements, Object: implID})
	}
	for _, embedID := range e.Embeds {
		triples = append(triples,
			message.Triple{Subject: e.ID, Predicate: CodeEmbeds, Object: embedID})
	}
	for _, callID := range e.Calls {
		triples = append(triples,
			message.Triple{Subject: e.ID, Predicate: CodeCalls, Object: callID})
	}
	for _, refID := range e.References {
		triples = append(triples,
			message.Triple{Subject: e.ID, Predicate: CodeReferences, Object: refID})
	}
	for _, retID := range e.Returns {
		triples = append(triples,
			message.Triple{Subject: e.ID, Predicate: CodeReturns, Object: retID})
	}
	if e.Receiver != "" {
		triples = append(triples,
			message.Triple{Subject: e.ID, Predicate: CodeReceiver, Object: e.Receiver})
	}
	for _, paramID := range e.Parameters {
		triples = append(triples,
			message.Triple{Subject: e.ID, Predicate: CodeParameter, Object: paramID})
	}

	// Timestamps
	triples = append(triples,
		message.Triple{Subject: e.ID, Predicate: DcCreated, Object: e.IndexedAt.Format(time.RFC3339)})

	return triples
}

// EntityState converts the CodeEntity to a graph.EntityState for storage.
func (e *CodeEntity) EntityState() *EntityState {
	return &EntityState{
		ID:        e.ID,
		Triples:   e.Triples(),
		UpdatedAt: e.IndexedAt,
	}
}

// EntityState mirrors graph.EntityState for local use without importing the full graph package.
// This allows the AST package to prepare data for graph storage.
type EntityState struct {
	ID        string
	Triples   []message.Triple
	UpdatedAt time.Time
}

// ParseResult holds the results of parsing a Go file
type ParseResult struct {
	// FileEntity is the entity representing the file itself
	FileEntity *CodeEntity

	// Entities are all entities extracted from the file
	Entities []*CodeEntity

	// Imports are the import paths found in the file
	Imports []string

	// Package is the package name
	Package string

	// Path is the file path
	Path string

	// Hash is the content hash
	Hash string
}

// AllTriples returns all triples from all entities in the parse result
func (r *ParseResult) AllTriples() []message.Triple {
	var triples []message.Triple
	for _, entity := range r.Entities {
		triples = append(triples, entity.Triples()...)
	}
	return triples
}

// AllEntityStates returns all entity states from the parse result
func (r *ParseResult) AllEntityStates() []*EntityState {
	states := make([]*EntityState, 0, len(r.Entities))
	for _, entity := range r.Entities {
		states = append(states, entity.EntityState())
	}
	return states
}
