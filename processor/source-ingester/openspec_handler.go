package sourceingester

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

	reqID := generateRequirementID(req.Name)

	triples := []message.Triple{
		{Subject: reqID, Predicate: specVocab.SpecType, Object: "requirement"},
		{Subject: reqID, Predicate: specVocab.RequirementName, Object: req.Name},
		{Subject: reqID, Predicate: specVocab.RequirementDescription, Object: req.Description},
		{Subject: reqID, Predicate: specVocab.RequirementStatus, Object: "active"},
		{Subject: reqID, Predicate: sourceVocab.CodeBelongs, Object: specID},
	}

	// Link spec to requirement
	linkID := generateLinkID(specID, reqID)
	entities = append(entities, &SourceEntityPayload{
		EntityID_: linkID,
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
		scenLinkID := generateLinkID(reqID, scenarioEntity.EntityID_)
		entities = append(entities, &SourceEntityPayload{
			EntityID_: scenLinkID,
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
	scenarioID := generateScenarioID(scenario.Name)

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

	// 6-part delta operation ID: c360.semspec.source.spec.delta.{instance}
	instance := parser.SanitizeIDPart(op.Requirement.Name)
	hash := shortHash([]byte(fmt.Sprintf("%s-%d-%s", specID, index, op.Requirement.Name)))
	opID := fmt.Sprintf("c360.semspec.source.spec.delta.%s%s", instance, hash)

	triples := []message.Triple{
		{Subject: opID, Predicate: specVocab.SpecType, Object: "delta-operation"},
		{Subject: opID, Predicate: specVocab.DeltaOperation, Object: op.Operation},
		{Subject: opID, Predicate: specVocab.DeltaTarget, Object: op.Requirement.Name},
		{Subject: opID, Predicate: sourceVocab.CodeBelongs, Object: specID},
	}

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

// generateSpecID creates a 6-part spec entity ID.
// Format: c360.semspec.source.spec.openspec.{instance}
func generateSpecID(path, hash string) string {
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	name = strings.TrimSuffix(name, ".spec") // Handle .spec.md
	instance := parser.SanitizeIDPart(name)

	shortHash := hash
	if len(shortHash) > 12 {
		shortHash = hash[:12]
	}

	return fmt.Sprintf("c360.semspec.source.spec.openspec.%s%s", instance, shortHash)
}

// generateRequirementID creates a 6-part requirement entity ID.
// Format: c360.semspec.source.spec.requirement.{instance}
func generateRequirementID(reqName string) string {
	instance := parser.SanitizeIDPart(reqName)
	hash := shortHash([]byte(reqName))
	return fmt.Sprintf("c360.semspec.source.spec.requirement.%s%s", instance, hash)
}

// generateScenarioID creates a 6-part scenario entity ID.
// Format: c360.semspec.source.spec.scenario.{instance}
func generateScenarioID(scenarioName string) string {
	instance := parser.SanitizeIDPart(scenarioName)
	hash := shortHash([]byte(scenarioName))
	return fmt.Sprintf("c360.semspec.source.spec.scenario.%s%s", instance, hash)
}

// generateLinkID creates a 6-part link entity ID.
// Format: c360.semspec.source.spec.link.{instance}
func generateLinkID(fromID, toID string) string {
	hash := shortHash([]byte(fromID + ":" + toID))
	return fmt.Sprintf("c360.semspec.source.spec.link.%s", hash)
}

// shortHash returns a 12-char hex hash of the input.
func shortHash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])[:12]
}

// IsOpenSpecFile checks if a path is an OpenSpec file.
// Exported for use by the component.
func IsOpenSpecFile(path string) bool {
	return parser.IsOpenSpecFile(path)
}
