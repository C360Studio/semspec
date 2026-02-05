package scenarios

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// RDFExportScenario tests the /export command that exports proposals as RDF.
// This verifies the vocabulary/RDF export functionality with different formats and profiles.
type RDFExportScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	fs          *client.FilesystemClient
	createdSlug string
}

// NewRDFExportScenario creates a new RDF export scenario.
func NewRDFExportScenario(cfg *config.Config) *RDFExportScenario {
	return &RDFExportScenario{
		name:        "rdf-export",
		description: "Tests /export command with RDF formats (turtle, ntriples, jsonld) and profiles (minimal, bfo, cco)",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *RDFExportScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *RDFExportScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *RDFExportScenario) Setup(ctx context.Context) error {
	// Create filesystem client and setup workspace
	s.fs = client.NewFilesystemClient(s.config.WorkspacePath)
	if err := s.fs.SetupWorkspace(); err != nil {
		return fmt.Errorf("setup workspace: %w", err)
	}

	// Create HTTP client
	s.http = client.NewHTTPClient(s.config.HTTPBaseURL)

	// Wait for service to be healthy
	if err := s.http.WaitForHealthy(ctx); err != nil {
		return fmt.Errorf("service not healthy: %w", err)
	}

	return nil
}

// Execute runs the RDF export scenario.
func (s *RDFExportScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"create-proposal", s.stageCreateProposal},
		{"export-turtle-minimal", s.stageExportTurtleMinimal},
		{"export-turtle-bfo", s.stageExportTurtleBFO},
		{"export-turtle-cco", s.stageExportTurtleCCO},
		{"export-jsonld", s.stageExportJSONLD},
		{"export-ntriples", s.stageExportNTriples},
	}

	for _, stage := range stages {
		stageStart := time.Now()
		stageCtx, cancel := context.WithTimeout(ctx, s.config.StageTimeout)

		err := stage.fn(stageCtx, result)
		cancel()

		stageDuration := time.Since(stageStart)
		result.SetMetric(fmt.Sprintf("%s_duration_ms", stage.name), stageDuration.Milliseconds())

		if err != nil {
			result.AddStage(stage.name, false, stageDuration, err.Error())
			result.AddError(fmt.Sprintf("%s: %v", stage.name, err))
			result.Error = fmt.Sprintf("%s failed: %v", stage.name, err)
			return result, nil
		}

		result.AddStage(stage.name, true, stageDuration, "")
	}

	result.Success = true
	return result, nil
}

// Teardown cleans up after the scenario.
func (s *RDFExportScenario) Teardown(ctx context.Context) error {
	// HTTP client doesn't need cleanup
	return nil
}

// stageCreateProposal creates a test proposal to export.
func (s *RDFExportScenario) stageCreateProposal(ctx context.Context, result *Result) error {
	proposalText := "Test RDF Export Feature"
	result.SetDetail("proposal_text", proposalText)

	resp, err := s.http.SendMessage(ctx, "/propose "+proposalText)
	if err != nil {
		return fmt.Errorf("send /propose command: %w", err)
	}

	if resp.Type == "error" {
		return fmt.Errorf("propose returned error: %s", resp.Content)
	}

	// Try to extract actual slug from response first
	s.createdSlug = extractSlug(resp.Content)

	// If extraction failed, look for change in filesystem
	if s.createdSlug == "" {
		changes, err := s.fs.ListChanges()
		if err != nil {
			return fmt.Errorf("list changes: %w", err)
		}
		// Find a change matching our proposal text pattern
		for _, slug := range changes {
			if strings.Contains(slug, "rdf-export") || strings.Contains(slug, "test-rdf") {
				s.createdSlug = slug
				break
			}
		}
	}

	// Last resort: use expected slug format
	if s.createdSlug == "" {
		s.createdSlug = "test-rdf-export-feature"
	}

	result.SetDetail("created_slug", s.createdSlug)

	// Verify proposal was created
	if err := s.fs.WaitForChange(ctx, s.createdSlug); err != nil {
		return fmt.Errorf("proposal '%s' not created: %w", s.createdSlug, err)
	}

	return nil
}

// stageExportTurtleMinimal tests Turtle export with minimal profile.
func (s *RDFExportScenario) stageExportTurtleMinimal(ctx context.Context, result *Result) error {
	cmd := fmt.Sprintf("/export %s turtle minimal", s.createdSlug)
	resp, err := s.http.SendMessage(ctx, cmd)
	if err != nil {
		return fmt.Errorf("send /export command: %w", err)
	}

	if resp.Type == "error" {
		return fmt.Errorf("export returned error: %s", resp.Content)
	}

	result.SetDetail("turtle_minimal_response", resp.Content)

	// Verify Turtle format markers
	checks := []struct {
		pattern string
		desc    string
	}{
		{"@prefix", "Turtle prefix declaration"},
		{"prov#Entity", "PROV-O Entity type"},
		{"semspec.dev/ontology/Proposal", "Semspec Proposal type"},
		{"semspec.dev/entity", "Entity namespace"},
	}

	for _, check := range checks {
		if !strings.Contains(resp.Content, check.pattern) {
			return fmt.Errorf("missing %s: expected '%s' in output", check.desc, check.pattern)
		}
	}

	// Verify minimal profile does NOT include BFO or CCO type assertions
	// Note: Prefix declarations (@prefix cco:) are always included, but actual type usage should not be
	if strings.Contains(resp.Content, "obo/BFO_") {
		return fmt.Errorf("minimal profile should not include BFO type assertions")
	}
	// Check for CCO type assertions (actual usage, not just prefix declaration)
	// CCO types appear as: <http://www.ontologyrepository.com/CommonCoreOntologies/InformationContentEntity>
	if strings.Contains(resp.Content, "CommonCoreOntologies/Information") ||
		strings.Contains(resp.Content, "CommonCoreOntologies/Act") ||
		strings.Contains(resp.Content, "CommonCoreOntologies/Person") {
		return fmt.Errorf("minimal profile should not include CCO type assertions")
	}

	return nil
}

// stageExportTurtleBFO tests Turtle export with BFO profile.
func (s *RDFExportScenario) stageExportTurtleBFO(ctx context.Context, result *Result) error {
	cmd := fmt.Sprintf("/export %s turtle bfo", s.createdSlug)
	resp, err := s.http.SendMessage(ctx, cmd)
	if err != nil {
		return fmt.Errorf("send /export command: %w", err)
	}

	if resp.Type == "error" {
		return fmt.Errorf("export returned error: %s", resp.Content)
	}

	result.SetDetail("turtle_bfo_response", resp.Content)

	// Verify BFO profile includes BFO types
	checks := []struct {
		pattern string
		desc    string
	}{
		{"@prefix", "Turtle prefix declaration"},
		{"prov#Entity", "PROV-O Entity type"},
		{"obo/BFO_", "BFO type assertion"},
	}

	for _, check := range checks {
		if !strings.Contains(resp.Content, check.pattern) {
			return fmt.Errorf("missing %s: expected '%s' in output", check.desc, check.pattern)
		}
	}

	// Verify BFO profile does NOT include CCO type assertions (only BFO)
	// Note: Prefix declarations are always included, check for actual type usage
	if strings.Contains(resp.Content, "CommonCoreOntologies/Information") ||
		strings.Contains(resp.Content, "CommonCoreOntologies/Act") ||
		strings.Contains(resp.Content, "CommonCoreOntologies/Person") {
		return fmt.Errorf("bfo profile should not include CCO type assertions")
	}

	return nil
}

// stageExportTurtleCCO tests Turtle export with CCO profile.
func (s *RDFExportScenario) stageExportTurtleCCO(ctx context.Context, result *Result) error {
	cmd := fmt.Sprintf("/export %s turtle cco", s.createdSlug)
	resp, err := s.http.SendMessage(ctx, cmd)
	if err != nil {
		return fmt.Errorf("send /export command: %w", err)
	}

	if resp.Type == "error" {
		return fmt.Errorf("export returned error: %s", resp.Content)
	}

	result.SetDetail("turtle_cco_response", resp.Content)

	// Verify CCO profile includes all types
	checks := []struct {
		pattern string
		desc    string
	}{
		{"@prefix", "Turtle prefix declaration"},
		{"prov#Entity", "PROV-O Entity type"},
		{"obo/BFO_", "BFO type assertion"},
		{"CommonCoreOntologies/Information", "CCO InformationContentEntity type"},
	}

	for _, check := range checks {
		if !strings.Contains(resp.Content, check.pattern) {
			return fmt.Errorf("missing %s: expected '%s' in output", check.desc, check.pattern)
		}
	}

	return nil
}

// stageExportJSONLD tests JSON-LD export.
func (s *RDFExportScenario) stageExportJSONLD(ctx context.Context, result *Result) error {
	cmd := fmt.Sprintf("/export %s jsonld", s.createdSlug)
	resp, err := s.http.SendMessage(ctx, cmd)
	if err != nil {
		return fmt.Errorf("send /export command: %w", err)
	}

	if resp.Type == "error" {
		return fmt.Errorf("export returned error: %s", resp.Content)
	}

	result.SetDetail("jsonld_response", resp.Content)

	// Verify JSON-LD format markers
	checks := []struct {
		pattern string
		desc    string
	}{
		{"@context", "JSON-LD context"},
		{"@graph", "JSON-LD graph"},
		{"@id", "JSON-LD id"},
		{"@type", "JSON-LD type"},
	}

	for _, check := range checks {
		if !strings.Contains(resp.Content, check.pattern) {
			return fmt.Errorf("missing %s: expected '%s' in output", check.desc, check.pattern)
		}
	}

	return nil
}

// stageExportNTriples tests N-Triples export.
func (s *RDFExportScenario) stageExportNTriples(ctx context.Context, result *Result) error {
	cmd := fmt.Sprintf("/export %s ntriples", s.createdSlug)
	resp, err := s.http.SendMessage(ctx, cmd)
	if err != nil {
		return fmt.Errorf("send /export command: %w", err)
	}

	if resp.Type == "error" {
		return fmt.Errorf("export returned error: %s", resp.Content)
	}

	result.SetDetail("ntriples_response", resp.Content)

	// Verify N-Triples format markers
	// N-Triples lines end with " ." and use full IRIs
	checks := []struct {
		pattern string
		desc    string
	}{
		{"> .", "N-Triples line terminator"},
		{"<http", "Full IRI (not prefixed)"},
		{"rdf-syntax-ns#type", "RDF type predicate"},
	}

	for _, check := range checks {
		if !strings.Contains(resp.Content, check.pattern) {
			return fmt.Errorf("missing %s: expected '%s' in output", check.desc, check.pattern)
		}
	}

	// N-Triples should NOT have @prefix declarations
	if strings.Contains(resp.Content, "@prefix") {
		return fmt.Errorf("N-Triples should not have @prefix declarations")
	}

	return nil
}
