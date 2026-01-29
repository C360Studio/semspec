package query

import (
	"testing"
)

func TestNewRequest(t *testing.T) {
	req := NewRequest(QueryEntity)

	if req.Type != QueryEntity {
		t.Errorf("Type = %q, want %q", req.Type, QueryEntity)
	}
	if req.RequestID == "" {
		t.Error("RequestID is empty")
	}
	if req.MaxResults != 100 {
		t.Errorf("MaxResults = %d, want 100", req.MaxResults)
	}
	if req.Depth != 1 {
		t.Errorf("Depth = %d, want 1", req.Depth)
	}
}

func TestNewResponse(t *testing.T) {
	resp := NewResponse("test-123")

	if resp.RequestID != "test-123" {
		t.Errorf("RequestID = %q, want %q", resp.RequestID, "test-123")
	}
	if !resp.Success {
		t.Error("Success should be true")
	}
	if resp.Entities == nil {
		t.Error("Entities is nil")
	}
}

func TestNewErrorResponse(t *testing.T) {
	resp := NewErrorResponse("test-456", "something went wrong")

	if resp.RequestID != "test-456" {
		t.Errorf("RequestID = %q, want %q", resp.RequestID, "test-456")
	}
	if resp.Success {
		t.Error("Success should be false")
	}
	if resp.Error != "something went wrong" {
		t.Errorf("Error = %q, want %q", resp.Error, "something went wrong")
	}
}

func TestQueryTypes(t *testing.T) {
	types := []QueryType{
		QueryEntity,
		QueryRelated,
		QueryDependsOn,
		QueryDependedBy,
		QueryImplements,
		QueryContains,
		QuerySearch,
	}

	for _, qt := range types {
		if qt == "" {
			t.Error("QueryType is empty")
		}
	}
}

func TestRelationTypes(t *testing.T) {
	types := []RelationType{
		RelContains,
		RelBelongsTo,
		RelImports,
		RelImplements,
		RelEmbeds,
		RelCalls,
		RelReferences,
	}

	for _, rt := range types {
		if rt == "" {
			t.Error("RelationType is empty")
		}
	}
}

func TestImpactAnalysis(t *testing.T) {
	analysis := ImpactAnalysis{
		Target:               "test.entity",
		DirectDependents:     []string{"dep1", "dep2"},
		TransitiveDependents: []string{"trans1"},
		AffectedFiles:        []string{"file1.go"},
		AffectedPackages:     []string{"pkg1"},
	}

	if analysis.Target != "test.entity" {
		t.Errorf("Target = %q, want %q", analysis.Target, "test.entity")
	}
	if len(analysis.DirectDependents) != 2 {
		t.Errorf("DirectDependents count = %d, want 2", len(analysis.DirectDependents))
	}
}
