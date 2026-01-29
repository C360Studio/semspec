package query

import (
	"time"
)

// Request represents a graph query request
type Request struct {
	// RequestID is a unique identifier for this request
	RequestID string `json:"request_id"`

	// Type is the query operation type
	Type QueryType `json:"type"`

	// EntityID is the target entity for entity-based queries
	EntityID string `json:"entity_id,omitempty"`

	// Relation is the relationship type for related queries
	Relation RelationType `json:"relation,omitempty"`

	// SearchText is the text to search for in search queries
	SearchText string `json:"search_text,omitempty"`

	// Predicates are specific predicates to filter on
	Predicates map[string]interface{} `json:"predicates,omitempty"`

	// MaxResults limits the number of results
	MaxResults int `json:"max_results,omitempty"`

	// IncludeTriples includes full triple data in results
	IncludeTriples bool `json:"include_triples,omitempty"`

	// Depth is the traversal depth for relationship queries
	Depth int `json:"depth,omitempty"`
}

// Response represents a graph query response
type Response struct {
	// RequestID matches the original request
	RequestID string `json:"request_id"`

	// Success indicates if the query succeeded
	Success bool `json:"success"`

	// Error contains error details if success is false
	Error string `json:"error,omitempty"`

	// Entities are the matching entities
	Entities []EntityResult `json:"entities,omitempty"`

	// TotalCount is the total number of matches (may exceed returned count)
	TotalCount int `json:"total_count"`

	// QueryTime is how long the query took
	QueryTime time.Duration `json:"query_time"`
}

// EntityResult represents a single entity in query results
type EntityResult struct {
	// ID is the entity identifier
	ID string `json:"id"`

	// Type is the entity type (function, struct, file, etc.)
	Type string `json:"type,omitempty"`

	// Name is the entity name
	Name string `json:"name,omitempty"`

	// Path is the file path
	Path string `json:"path,omitempty"`

	// Package is the Go package
	Package string `json:"package,omitempty"`

	// Triples are the full triples if requested
	Triples []Triple `json:"triples,omitempty"`

	// Related contains related entity IDs by relationship type
	Related map[RelationType][]string `json:"related,omitempty"`
}

// Triple represents a subject-predicate-object triple
type Triple struct {
	Subject   string      `json:"subject"`
	Predicate string      `json:"predicate"`
	Object    interface{} `json:"object"`
}

// ImpactAnalysis represents the result of impact analysis
type ImpactAnalysis struct {
	// Target is the entity being analyzed
	Target string `json:"target"`

	// DirectDependents are entities that directly reference the target
	DirectDependents []string `json:"direct_dependents"`

	// TransitiveDependents are entities that indirectly depend on the target
	TransitiveDependents []string `json:"transitive_dependents"`

	// AffectedFiles are files that would be affected by changes
	AffectedFiles []string `json:"affected_files"`

	// AffectedPackages are packages that would be affected
	AffectedPackages []string `json:"affected_packages"`
}

// NewRequest creates a new query request with a generated ID
func NewRequest(queryType QueryType) *Request {
	return &Request{
		RequestID:  generateRequestID(),
		Type:       queryType,
		MaxResults: 100,
		Depth:      1,
	}
}

// NewResponse creates a successful empty response
func NewResponse(requestID string) *Response {
	return &Response{
		RequestID: requestID,
		Success:   true,
		Entities:  make([]EntityResult, 0),
	}
}

// NewErrorResponse creates an error response
func NewErrorResponse(requestID, errorMsg string) *Response {
	return &Response{
		RequestID: requestID,
		Success:   false,
		Error:     errorMsg,
	}
}

// generateRequestID generates a unique request ID
func generateRequestID() string {
	return time.Now().Format("20060102150405.000000")
}
