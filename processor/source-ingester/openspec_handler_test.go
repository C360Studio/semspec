package sourceingester

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sourceVocab "github.com/c360studio/semspec/vocabulary/source"
	specVocab "github.com/c360studio/semspec/vocabulary/spec"
)

func TestOpenSpecHandler_IngestSpec_SourceOfTruth(t *testing.T) {
	// Create temp directory with test spec
	tempDir := t.TempDir()

	specContent := `---
title: Authentication Spec
applies_to:
  - "auth/**/*.go"
---

# Authentication

### Requirement: Token-Refresh

The system MUST refresh tokens before expiration.
The system SHALL NOT allow expired tokens.

#### Scenario: Valid Token Refresh

**GIVEN** a valid refresh token
**WHEN** the token is about to expire
**THEN** a new access token is issued

### Requirement: Session-Timeout

Sessions SHALL timeout after 30 minutes.
`

	specPath := filepath.Join(tempDir, "auth.spec.md")
	if err := os.WriteFile(specPath, []byte(specContent), 0644); err != nil {
		t.Fatalf("Failed to write test spec: %v", err)
	}

	handler := NewOpenSpecHandler(tempDir)

	entities, err := handler.IngestSpec(context.Background(), IngestRequest{
		Path:      specPath,
		ProjectID: "test-project",
		AddedBy:   "test-user",
	})
	if err != nil {
		t.Fatalf("IngestSpec failed: %v", err)
	}

	// Should have: 1 spec + 2 requirements + 2 req links + 1 scenario + 1 scenario link = 7
	if len(entities) < 5 {
		t.Errorf("Expected at least 5 entities, got %d", len(entities))
	}

	// Find spec entity
	var specEntity *SourceEntityPayload
	for _, e := range entities {
		if hasTriple(e, specVocab.SpecType, "specification") {
			specEntity = e
			break
		}
	}

	if specEntity == nil {
		t.Fatal("No spec entity found")
	}

	// Verify spec predicates
	if !hasTriple(specEntity, specVocab.SpecSpecType, "source-of-truth") {
		t.Error("Expected spec to be source-of-truth")
	}

	if !hasTriple(specEntity, specVocab.SpecTitle, "Authentication Spec") {
		t.Error("Expected title to be 'Authentication Spec'")
	}

	if !hasTriple(specEntity, sourceVocab.SourceType, "openspec") {
		t.Error("Expected source type to be 'openspec'")
	}

	// Find requirement entities
	var reqCount int
	for _, e := range entities {
		if hasTriple(e, specVocab.SpecType, "requirement") {
			reqCount++
		}
	}

	if reqCount != 2 {
		t.Errorf("Expected 2 requirement entities, got %d", reqCount)
	}

	// Find scenario entity
	var scenarioEntity *SourceEntityPayload
	for _, e := range entities {
		if hasTriple(e, specVocab.SpecType, "scenario") {
			scenarioEntity = e
			break
		}
	}

	if scenarioEntity == nil {
		t.Fatal("No scenario entity found")
	}

	// Verify scenario has Given/When/Then
	if !hasPredicateWithValue(scenarioEntity, specVocab.ScenarioGiven) {
		t.Error("Expected scenario to have Given clause")
	}
	if !hasPredicateWithValue(scenarioEntity, specVocab.ScenarioWhen) {
		t.Error("Expected scenario to have When clause")
	}
	if !hasPredicateWithValue(scenarioEntity, specVocab.ScenarioThen) {
		t.Error("Expected scenario to have Then clause")
	}
}

func TestOpenSpecHandler_IngestSpec_Delta(t *testing.T) {
	tempDir := t.TempDir()

	deltaContent := `---
title: Auth Changes
modifies: auth.spec
---

# Authentication Changes

## ADDED Requirements

### Requirement: MFA-Support

The system MUST support multi-factor authentication.

## MODIFIED Requirements

### Requirement: Token-Refresh

Tokens MUST now refresh 5 minutes before expiration.

## REMOVED Requirements

### Requirement: Legacy-Auth

This requirement is deprecated.
`

	specPath := filepath.Join(tempDir, "changes.delta.md")
	if err := os.WriteFile(specPath, []byte(deltaContent), 0644); err != nil {
		t.Fatalf("Failed to write test spec: %v", err)
	}

	handler := NewOpenSpecHandler(tempDir)

	entities, err := handler.IngestSpec(context.Background(), IngestRequest{
		Path: specPath,
	})
	if err != nil {
		t.Fatalf("IngestSpec failed: %v", err)
	}

	// Should have: 1 spec + 3 delta operations = 4
	if len(entities) < 4 {
		t.Errorf("Expected at least 4 entities, got %d", len(entities))
	}

	// Find spec entity
	var specEntity *SourceEntityPayload
	for _, e := range entities {
		if hasTriple(e, specVocab.SpecType, "specification") {
			specEntity = e
			break
		}
	}

	if specEntity == nil {
		t.Fatal("No spec entity found")
	}

	// Verify delta spec type
	if !hasTriple(specEntity, specVocab.SpecSpecType, "delta") {
		t.Error("Expected spec to be delta")
	}

	// Verify modifies relationship
	if !hasTriple(specEntity, specVocab.Modifies, "auth.spec") {
		t.Error("Expected spec to modify auth.spec")
	}

	// Count delta operations
	var deltaOps = map[string]int{}
	for _, e := range entities {
		if hasTriple(e, specVocab.SpecType, "delta-operation") {
			// Find the operation type
			for _, triple := range e.TripleData {
				if triple.Predicate == specVocab.DeltaOperation {
					if op, ok := triple.Object.(string); ok {
						deltaOps[op]++
					}
				}
			}
		}
	}

	if deltaOps["added"] != 1 {
		t.Errorf("Expected 1 added operation, got %d", deltaOps["added"])
	}
	if deltaOps["modified"] != 1 {
		t.Errorf("Expected 1 modified operation, got %d", deltaOps["modified"])
	}
	if deltaOps["removed"] != 1 {
		t.Errorf("Expected 1 removed operation, got %d", deltaOps["removed"])
	}
}

func TestGenerateSpecID(t *testing.T) {
	tests := []struct {
		path string
		hash string
	}{
		{path: "/path/to/auth.spec.md", hash: "abc123def456"},
		{path: "simple.md", hash: "xyz789abc123"},
		{path: "/openspec/specs/user-auth.spec.md", hash: "hash12345678901234"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := generateSpecID(tt.path, tt.hash)
			// Verify 6-part format
			parts := strings.Split(result, ".")
			if len(parts) != 6 {
				t.Errorf("generateSpecID(%s, %s) = %s, expected 6 dot-separated parts, got %d", tt.path, tt.hash, result, len(parts))
			}
			if parts[0] != "c360" || parts[1] != "semspec" || parts[2] != "source" || parts[3] != "spec" || parts[4] != "openspec" {
				t.Errorf("generateSpecID(%s, %s) = %s, expected prefix c360.semspec.source.spec.openspec", tt.path, tt.hash, result)
			}
		})
	}
}

// hasTriple checks if an entity has a triple with the given predicate and object.
func hasTriple(entity *SourceEntityPayload, predicate string, object any) bool {
	for _, triple := range entity.TripleData {
		if triple.Predicate == predicate && triple.Object == object {
			return true
		}
	}
	return false
}

// hasPredicateWithValue checks if an entity has a non-empty value for the given predicate.
func hasPredicateWithValue(entity *SourceEntityPayload, predicate string) bool {
	for _, triple := range entity.TripleData {
		if triple.Predicate == predicate {
			if s, ok := triple.Object.(string); ok && s != "" {
				return true
			}
		}
	}
	return false
}
