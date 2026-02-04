package export_test

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/export"
	"github.com/c360studio/semspec/vocabulary/semspec"
)

func TestNewRDFExporter(t *testing.T) {
	profiles := []export.Profile{
		export.ProfileMinimal,
		export.ProfileBFO,
		export.ProfileCCO,
	}

	for _, profile := range profiles {
		t.Run(string(profile), func(t *testing.T) {
			exporter := export.NewRDFExporter(profile)
			if exporter == nil {
				t.Fatal("NewRDFExporter returned nil")
			}
		})
	}
}

func TestExportTurtle(t *testing.T) {
	exporter := export.NewRDFExporter(export.ProfileMinimal)

	exporter.AddEntity(export.Entity{
		ID:         "acme.semspec.project.proposal.api.auth-refresh",
		EntityType: semspec.EntityTypeProposal,
		Triples: []export.Triple{
			{Subject: "acme.semspec.project.proposal.api.auth-refresh", Predicate: semspec.ProposalTitle, Object: "Auth Token Refresh"},
			{Subject: "acme.semspec.project.proposal.api.auth-refresh", Predicate: semspec.PredicateProposalStatus, Object: "approved"},
		},
	})

	output, err := exporter.Export(export.FormatTurtle)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// Check for expected content
	if !strings.Contains(output, "@prefix") {
		t.Error("Turtle output should contain prefix declarations")
	}
	if !strings.Contains(output, "semspec.dev/entity") {
		t.Error("Turtle output should contain entity IRIs")
	}
	if !strings.Contains(output, "Auth Token Refresh") {
		t.Error("Turtle output should contain the title")
	}
	if !strings.Contains(output, "approved") {
		t.Error("Turtle output should contain the status")
	}
}

func TestExportNTriples(t *testing.T) {
	exporter := export.NewRDFExporter(export.ProfileMinimal)

	exporter.AddEntity(export.Entity{
		ID:         "acme.semspec.project.proposal.api.auth-refresh",
		EntityType: semspec.EntityTypeProposal,
		Triples: []export.Triple{
			{Subject: "acme.semspec.project.proposal.api.auth-refresh", Predicate: semspec.ProposalTitle, Object: "Auth Token Refresh"},
		},
	})

	output, err := exporter.Export(export.FormatNTriples)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// N-Triples format should have one triple per line
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 {
		t.Error("N-Triples output should have at least one line")
	}

	// Each line should end with " ."
	for _, line := range lines {
		if !strings.HasSuffix(line, " .") {
			t.Errorf("N-Triple line should end with ' .': %s", line)
		}
	}
}

func TestExportJSONLD(t *testing.T) {
	exporter := export.NewRDFExporter(export.ProfileMinimal)

	exporter.AddEntity(export.Entity{
		ID:         "acme.semspec.project.proposal.api.auth-refresh",
		EntityType: semspec.EntityTypeProposal,
		Triples: []export.Triple{
			{Subject: "acme.semspec.project.proposal.api.auth-refresh", Predicate: semspec.ProposalTitle, Object: "Auth Token Refresh"},
		},
	})

	output, err := exporter.Export(export.FormatJSONLD)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// Check for expected JSON-LD structure
	if !strings.Contains(output, "@context") {
		t.Error("JSON-LD output should contain @context")
	}
	if !strings.Contains(output, "@graph") {
		t.Error("JSON-LD output should contain @graph")
	}
	if !strings.Contains(output, "@id") {
		t.Error("JSON-LD output should contain @id")
	}
	if !strings.Contains(output, "@type") {
		t.Error("JSON-LD output should contain @type")
	}
}

func TestExportProfileMinimal(t *testing.T) {
	exporter := export.NewRDFExporter(export.ProfileMinimal)

	exporter.AddEntity(export.Entity{
		ID:         "acme.semspec.project.proposal.api.auth-refresh",
		EntityType: semspec.EntityTypeProposal,
		Triples:    []export.Triple{},
	})

	output, err := exporter.Export(export.FormatTurtle)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// Minimal profile should include PROV-O type
	if !strings.Contains(output, "prov#Entity") {
		t.Error("Minimal profile should include prov:Entity type")
	}

	// Minimal profile should NOT include BFO type
	if strings.Contains(output, "BFO_0000031") {
		t.Error("Minimal profile should not include BFO types")
	}
}

func TestExportProfileBFO(t *testing.T) {
	exporter := export.NewRDFExporter(export.ProfileBFO)

	exporter.AddEntity(export.Entity{
		ID:         "acme.semspec.project.proposal.api.auth-refresh",
		EntityType: semspec.EntityTypeProposal,
		Triples:    []export.Triple{},
	})

	output, err := exporter.Export(export.FormatTurtle)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// BFO profile should include BFO type
	if !strings.Contains(output, "BFO_0000031") {
		t.Error("BFO profile should include BFO:GenericallyDependentContinuant")
	}
}

func TestExportProfileCCO(t *testing.T) {
	exporter := export.NewRDFExporter(export.ProfileCCO)

	exporter.AddEntity(export.Entity{
		ID:         "acme.semspec.project.proposal.api.auth-refresh",
		EntityType: semspec.EntityTypeProposal,
		Triples:    []export.Triple{},
	})

	output, err := exporter.Export(export.FormatTurtle)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// CCO profile should include CCO type
	if !strings.Contains(output, "InformationContentEntity") {
		t.Error("CCO profile should include CCO:InformationContentEntity")
	}

	// CCO profile should also include BFO type
	if !strings.Contains(output, "BFO_0000031") {
		t.Error("CCO profile should also include BFO types")
	}
}

func TestExportMultipleEntities(t *testing.T) {
	exporter := export.NewRDFExporter(export.ProfileMinimal)

	exporter.AddEntity(export.Entity{
		ID:         "acme.semspec.project.proposal.api.auth-refresh",
		EntityType: semspec.EntityTypeProposal,
		Triples: []export.Triple{
			{Subject: "acme.semspec.project.proposal.api.auth-refresh", Predicate: semspec.ProposalTitle, Object: "Auth Refresh"},
		},
	})

	exporter.AddEntity(export.Entity{
		ID:         "acme.semspec.project.spec.api.auth-refresh-v1",
		EntityType: semspec.EntityTypeSpec,
		Triples: []export.Triple{
			{Subject: "acme.semspec.project.spec.api.auth-refresh-v1", Predicate: semspec.SpecTitle, Object: "Auth Spec"},
		},
	})

	output, err := exporter.Export(export.FormatTurtle)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// Should contain both entities
	if !strings.Contains(output, "auth-refresh") {
		t.Error("Output should contain first entity")
	}
	if !strings.Contains(output, "auth-refresh-v1") {
		t.Error("Output should contain second entity")
	}
}

func TestExportObjectTypes(t *testing.T) {
	exporter := export.NewRDFExporter(export.ProfileMinimal)

	exporter.AddEntity(export.Entity{
		ID:         "acme.semspec.agent.loop.api.loop-123",
		EntityType: semspec.EntityTypeLoop,
		Triples: []export.Triple{
			// String
			{Subject: "test", Predicate: semspec.PredicateLoopRole, Object: "implementer"},
			// Integer
			{Subject: "test", Predicate: semspec.LoopIterations, Object: 5},
			// Boolean
			{Subject: "test", Predicate: semspec.ActivitySuccess, Object: true},
			// Datetime
			{Subject: "test", Predicate: semspec.LoopStartedAt, Object: "2025-01-28T10:30:00Z"},
			// Entity reference
			{Subject: "test", Predicate: semspec.LoopTask, Object: "acme.semspec.project.task.api.task-1"},
		},
	})

	output, err := exporter.Export(export.FormatTurtle)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// Check string literal
	if !strings.Contains(output, `"implementer"`) {
		t.Error("Output should contain string literal")
	}

	// Check integer with datatype
	if !strings.Contains(output, "xsd:integer") {
		t.Error("Output should contain integer datatype")
	}

	// Check boolean with datatype
	if !strings.Contains(output, "xsd:boolean") {
		t.Error("Output should contain boolean datatype")
	}

	// Check datetime with datatype
	if !strings.Contains(output, "xsd:dateTime") {
		t.Error("Output should contain dateTime datatype")
	}

	// Check entity reference as IRI
	if !strings.Contains(output, "semspec.dev/entity") {
		t.Error("Output should contain entity reference as IRI")
	}
}

func TestUnsupportedFormat(t *testing.T) {
	exporter := export.NewRDFExporter(export.ProfileMinimal)

	_, err := exporter.Export("unknown")
	if err == nil {
		t.Error("Expected error for unsupported format")
	}
}

func TestAddEntityFromTriples(t *testing.T) {
	exporter := export.NewRDFExporter(export.ProfileMinimal)

	triples := []export.Triple{
		{Subject: "acme.semspec.project.proposal.api.test", Predicate: semspec.ProposalTitle, Object: "Test Proposal"},
	}

	exporter.AddEntityFromTriples(
		"acme.semspec.project.proposal.api.test",
		semspec.EntityTypeProposal,
		triples,
	)

	output, err := exporter.Export(export.FormatTurtle)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	if !strings.Contains(output, "Test Proposal") {
		t.Error("Output should contain the added entity")
	}
}
