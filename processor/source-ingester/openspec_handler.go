package sourceingester

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/c360studio/semspec/source/parser"
	sourceVocab "github.com/c360studio/semspec/vocabulary/source"
	specVocab "github.com/c360studio/semspec/vocabulary/spec"
	"github.com/c360studio/semstreams/message"
)

// OpenSpecHandler processes OpenSpec document ingestion.
type OpenSpecHandler struct {
	parser     *parser.OpenSpecParser
	sourcesDir string
}

// NewOpenSpecHandler creates a new OpenSpec handler.
func NewOpenSpecHandler(sourcesDir string) *OpenSpecHandler {
	return &OpenSpecHandler{
		parser:     parser.NewOpenSpecParser(),
		sourcesDir: sourcesDir,
	}
}

// IngestSpec processes an OpenSpec document and returns entities for graph ingestion.
func (h *OpenSpecHandler) IngestSpec(ctx context.Context, req IngestRequest) ([]*SourceEntityPayload, error) {
	// Resolve path
	path := req.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(h.sourcesDir, path)
	}

	// Read document content
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read document: %w", err)
	}

	// Parse OpenSpec document
	spec, err := h.parser.ParseSpec(path, content)
	if err != nil {
		return nil, fmt.Errorf("parse openspec: %w", err)
	}

	// Build entities
	var entities []*SourceEntityPayload

	// Generate spec entity
	specEntity := h.buildSpecEntity(spec, path, req)
	entities = append(entities, specEntity)

	// Generate entities based on spec type
	if spec.Type == parser.SpecTypeSourceOfTruth {
		// Generate requirement and scenario entities
		for _, req := range spec.Requirements {
			reqEntities := h.buildRequirementEntities(specEntity.EntityID_, req)
			entities = append(entities, reqEntities...)
		}
	} else {
		// Generate delta operation entities
		for i, op := range spec.DeltaOps {
			opEntities := h.buildDeltaOperationEntities(specEntity.EntityID_, op, i)
			entities = append(entities, opEntities...)
		}
	}

	return entities, nil
}

// buildSpecEntity creates the main spec document entity.
func (h *OpenSpecHandler) buildSpecEntity(spec *parser.ParsedSpec, path string, req IngestRequest) *SourceEntityPayload {
	// Generate spec ID
	specID := generateSpecID(path, spec.FileHash)

	triples := []message.Triple{
		{Subject: specID, Predicate: specVocab.SpecType, Object: "specification"},
		{Subject: specID, Predicate: specVocab.SpecSpecType, Object: spec.Type},
		{Subject: specID, Predicate: specVocab.SpecFilePath, Object: path},
		{Subject: specID, Predicate: specVocab.SpecFileHash, Object: spec.FileHash},
		{Subject: specID, Predicate: sourceVocab.SourceType, Object: "openspec"},
		{Subject: specID, Predicate: sourceVocab.SourceStatus, Object: "ready"},
		{Subject: specID, Predicate: sourceVocab.SourceAddedAt, Object: time.Now().Format(time.RFC3339)},
	}

	if spec.Title != "" {
		triples = append(triples, message.Triple{
			Subject: specID, Predicate: specVocab.SpecTitle, Object: spec.Title,
		})
	}

	if len(spec.AppliesTo) > 0 {
		triples = append(triples, message.Triple{
			Subject: specID, Predicate: specVocab.AppliesTo, Object: spec.AppliesTo,
		})
	}

	// Handle delta spec modifies relationship
	if spec.Type == parser.SpecTypeDelta {
		if modifies, ok := spec.Frontmatter["modifies"].(string); ok && modifies != "" {
			triples = append(triples, message.Triple{
				Subject: specID, Predicate: specVocab.Modifies, Object: modifies,
			})
		}
	}

	if req.ProjectID != "" {
		triples = append(triples, message.Triple{
			Subject: specID, Predicate: sourceVocab.SourceProject, Object: req.ProjectID,
		})
	}

	if req.AddedBy != "" {
		triples = append(triples, message.Triple{
			Subject: specID, Predicate: sourceVocab.SourceAddedBy, Object: req.AddedBy,
		})
	}

	return &SourceEntityPayload{
		EntityID_:  specID,
		TripleData: triples,
		UpdatedAt:  time.Now(),
	}
}

// buildRequirementEntities creates requirement and scenario entities.
func (h *OpenSpecHandler) buildRequirementEntities(specID string, req parser.Requirement) []*SourceEntityPayload {
	var entities []*SourceEntityPayload

	// Generate requirement ID
	reqID := generateRequirementID(specID, req.Name)

	triples := []message.Triple{
		{Subject: reqID, Predicate: specVocab.SpecType, Object: "requirement"},
		{Subject: reqID, Predicate: specVocab.RequirementName, Object: req.Name},
		{Subject: reqID, Predicate: specVocab.RequirementDescription, Object: req.Description},
		{Subject: reqID, Predicate: specVocab.RequirementStatus, Object: "active"},
		{Subject: reqID, Predicate: sourceVocab.CodeBelongs, Object: specID},
	}

	// Link spec to requirement
	entities = append(entities, &SourceEntityPayload{
		EntityID_: specID + ".link." + sanitizeID(req.Name),
		TripleData: []message.Triple{
			{Subject: specID, Predicate: specVocab.HasRequirement, Object: reqID},
		},
		UpdatedAt: time.Now(),
	})

	if len(req.Normatives) > 0 {
		triples = append(triples, message.Triple{
			Subject: reqID, Predicate: specVocab.RequirementNormative, Object: req.Normatives,
		})
	}

	if len(req.AppliesTo) > 0 {
		triples = append(triples, message.Triple{
			Subject: reqID, Predicate: specVocab.AppliesTo, Object: req.AppliesTo,
		})
	}

	reqEntity := &SourceEntityPayload{
		EntityID_:  reqID,
		TripleData: triples,
		UpdatedAt:  time.Now(),
	}
	entities = append(entities, reqEntity)

	// Create scenario entities
	for _, scenario := range req.Scenarios {
		scenarioEntity := h.buildScenarioEntity(reqID, scenario)
		entities = append(entities, scenarioEntity)

		// Add link from requirement to scenario
		entities = append(entities, &SourceEntityPayload{
			EntityID_: reqID + ".link." + sanitizeID(scenario.Name),
			TripleData: []message.Triple{
				{Subject: reqID, Predicate: specVocab.HasScenario, Object: scenarioEntity.EntityID_},
			},
			UpdatedAt: time.Now(),
		})
	}

	return entities
}

// buildScenarioEntity creates a scenario entity.
func (h *OpenSpecHandler) buildScenarioEntity(reqID string, scenario parser.Scenario) *SourceEntityPayload {
	scenarioID := generateScenarioID(reqID, scenario.Name)

	triples := []message.Triple{
		{Subject: scenarioID, Predicate: specVocab.SpecType, Object: "scenario"},
		{Subject: scenarioID, Predicate: specVocab.ScenarioName, Object: scenario.Name},
		{Subject: scenarioID, Predicate: sourceVocab.CodeBelongs, Object: reqID},
	}

	if scenario.Given != "" {
		triples = append(triples, message.Triple{
			Subject: scenarioID, Predicate: specVocab.ScenarioGiven, Object: scenario.Given,
		})
	}

	if scenario.When != "" {
		triples = append(triples, message.Triple{
			Subject: scenarioID, Predicate: specVocab.ScenarioWhen, Object: scenario.When,
		})
	}

	if scenario.Then != "" {
		triples = append(triples, message.Triple{
			Subject: scenarioID, Predicate: specVocab.ScenarioThen, Object: scenario.Then,
		})
	}

	return &SourceEntityPayload{
		EntityID_:  scenarioID,
		TripleData: triples,
		UpdatedAt:  time.Now(),
	}
}

// buildDeltaOperationEntities creates delta operation entities.
func (h *OpenSpecHandler) buildDeltaOperationEntities(specID string, op parser.DeltaOperation, index int) []*SourceEntityPayload {
	var entities []*SourceEntityPayload

	// Generate delta operation ID
	opID := fmt.Sprintf("%s.delta.%d.%s", specID, index, sanitizeID(op.Requirement.Name))

	triples := []message.Triple{
		{Subject: opID, Predicate: specVocab.SpecType, Object: "delta-operation"},
		{Subject: opID, Predicate: specVocab.DeltaOperation, Object: op.Operation},
		{Subject: opID, Predicate: specVocab.DeltaTarget, Object: op.Requirement.Name},
		{Subject: opID, Predicate: sourceVocab.CodeBelongs, Object: specID},
	}

	// Include requirement details in the operation
	if op.Requirement.Description != "" {
		triples = append(triples, message.Triple{
			Subject: opID, Predicate: specVocab.RequirementDescription, Object: op.Requirement.Description,
		})
	}

	if len(op.Requirement.Normatives) > 0 {
		triples = append(triples, message.Triple{
			Subject: opID, Predicate: specVocab.RequirementNormative, Object: op.Requirement.Normatives,
		})
	}

	opEntity := &SourceEntityPayload{
		EntityID_:  opID,
		TripleData: triples,
		UpdatedAt:  time.Now(),
	}
	entities = append(entities, opEntity)

	// Create scenario entities for the requirement in this delta operation
	for _, scenario := range op.Requirement.Scenarios {
		scenarioEntity := h.buildScenarioEntity(opID, scenario)
		entities = append(entities, scenarioEntity)
	}

	return entities
}

// generateSpecID creates a stable spec entity ID.
func generateSpecID(path, hash string) string {
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	name = strings.TrimSuffix(name, ".spec") // Handle .spec.md
	name = sanitizeID(name)

	shortHash := hash
	if len(shortHash) > 12 {
		shortHash = hash[:12]
	}

	return fmt.Sprintf("spec.%s.%s", name, shortHash)
}

// generateRequirementID creates a requirement entity ID.
func generateRequirementID(specID, reqName string) string {
	return fmt.Sprintf("%s.req.%s", specID, sanitizeID(reqName))
}

// generateScenarioID creates a scenario entity ID.
func generateScenarioID(reqID, scenarioName string) string {
	return fmt.Sprintf("%s.scenario.%s", reqID, sanitizeID(scenarioName))
}

// sanitizeID makes a string safe for use as an entity ID component.
func sanitizeID(s string) string {
	var result strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z':
			result.WriteRune(r)
		case r >= '0' && r <= '9':
			result.WriteRune(r)
		case r == '-' || r == '_' || r == ' ':
			result.WriteRune('-')
		}
	}
	return result.String()
}

// IsOpenSpecFile checks if a path is an OpenSpec file.
// Exported for use by the component.
func IsOpenSpecFile(path string) bool {
	return parser.IsOpenSpecFile(path)
}
