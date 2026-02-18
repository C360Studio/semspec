package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
	semspecVocab "github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/message"
)

// RDFExportScenario tests the rdf-export output component.
// It publishes an entity message to graph.ingest.entity and verifies
// that the component produces serialized RDF on graph.export.rdf.
type RDFExportScenario struct {
	name        string
	description string
	config      *config.Config
	nats        *client.NATSClient
	http        *client.HTTPClient
	capture     *client.MessageCapture
}

// NewRDFExportScenario creates a new RDF export scenario.
func NewRDFExportScenario(cfg *config.Config) *RDFExportScenario {
	return &RDFExportScenario{
		name:        "rdf-export",
		description: "Tests rdf-export component: publishes entity to graph.ingest.entity, verifies RDF output on graph.export.rdf",
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
	s.http = client.NewHTTPClient(s.config.HTTPBaseURL)
	if err := s.http.WaitForHealthy(ctx); err != nil {
		return fmt.Errorf("service not healthy: %w", err)
	}

	natsClient, err := client.NewNATSClient(ctx, s.config.NATSURL)
	if err != nil {
		return fmt.Errorf("create NATS client: %w", err)
	}
	s.nats = natsClient

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
		{"setup-capture", s.stageSetupCapture},
		{"publish-entity", s.stagePublishEntity},
		{"verify-rdf-output", s.stageVerifyRDFOutput},
	}

	for _, stage := range stages {
		stageStart := time.Now()
		stageCtx, cancel := context.WithTimeout(ctx, s.config.StageTimeout)

		err := stage.fn(stageCtx, result)
		cancel()

		stageDuration := time.Since(stageStart)
		result.SetMetric(fmt.Sprintf("%s_duration_us", stage.name), stageDuration.Microseconds())

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
	var errs []error
	if s.capture != nil {
		if err := s.capture.Stop(); err != nil {
			errs = append(errs, fmt.Errorf("stop capture: %w", err))
		}
	}
	if s.nats != nil {
		if err := s.nats.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("close NATS: %w", err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("teardown errors: %v", errs)
	}
	return nil
}

func (s *RDFExportScenario) stageSetupCapture(_ context.Context, result *Result) error {
	capture, err := s.nats.CaptureMessages("graph.export.rdf")
	if err != nil {
		return fmt.Errorf("start message capture: %w", err)
	}
	s.capture = capture
	result.SetDetail("capture_started", true)
	return nil
}

func (s *RDFExportScenario) stagePublishEntity(ctx context.Context, result *Result) error {
	entityID := "semspec.local.workflow.plan.plan.rdf-export-test"
	now := time.Now()

	payload := &workflow.PlanEntityPayload{
		EntityID_: entityID,
		TripleData: []message.Triple{
			{
				Subject:    entityID,
				Predicate:  semspecVocab.PlanTitle,
				Object:     "RDF Export Test Plan",
				Source:     "e2e-test",
				Timestamp:  now,
				Confidence: 1.0,
			},
			{
				Subject:    entityID,
				Predicate:  semspecVocab.PredicatePlanStatus,
				Object:     "exploring",
				Source:     "e2e-test",
				Timestamp:  now,
				Confidence: 1.0,
			},
			{
				Subject:    entityID,
				Predicate:  semspecVocab.PlanSlug,
				Object:     "rdf-export-test",
				Source:     "e2e-test",
				Timestamp:  now,
				Confidence: 1.0,
			},
		},
		UpdatedAt: now,
	}

	baseMsg := message.NewBaseMessage(workflow.EntityType, payload, "e2e-test")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal entity message: %w", err)
	}

	if err := s.nats.PublishToStream(ctx, "graph.ingest.entity", data); err != nil {
		return fmt.Errorf("publish entity: %w", err)
	}

	result.SetDetail("entity_id", entityID)
	result.SetDetail("entity_published", true)
	return nil
}

func (s *RDFExportScenario) stageVerifyRDFOutput(ctx context.Context, result *Result) error {
	waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	if err := s.capture.WaitForCount(waitCtx, 1); err != nil {
		return fmt.Errorf("no RDF output received on graph.export.rdf: %w", err)
	}

	msgs := s.capture.Messages()
	if len(msgs) == 0 {
		return fmt.Errorf("no messages in capture")
	}

	output := string(msgs[0].Data)
	result.SetDetail("rdf_output", output)
	result.SetMetric("rdf_output_bytes", len(output))

	// Verify Turtle format: either prefixed (@prefix) or compact with full IRIs (<...>)
	// The semstreams serializer produces compact Turtle with full IRIs
	hasPrefixes := strings.Contains(output, "@prefix")
	hasFullIRIs := strings.Contains(output, "<https://semspec.dev")

	if !hasPrefixes && !hasFullIRIs {
		return fmt.Errorf("invalid Turtle format: expected @prefix declarations or full IRIs (got: %s)",
			rdfTruncate(output, 500))
	}

	// Verify base IRI is present
	if !strings.Contains(output, "semspec.dev") {
		return fmt.Errorf("missing base IRI: expected 'semspec.dev' in output (got: %s)",
			rdfTruncate(output, 500))
	}

	// Verify entity data is present (at least one of the test values)
	hasTitle := strings.Contains(output, "RDF Export Test Plan")
	hasSlug := strings.Contains(output, "rdf-export-test")
	hasStatus := strings.Contains(output, "exploring")

	if !hasTitle && !hasSlug && !hasStatus {
		return fmt.Errorf("RDF output does not contain entity data (got: %s)", rdfTruncate(output, 500))
	}

	result.SetDetail("rdf_verified", true)
	return nil
}

func rdfTruncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
