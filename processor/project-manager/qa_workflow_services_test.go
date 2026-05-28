package projectmanager

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/harnesscatalog"
)

// makeServicesSelection returns a single resolved selection whose Profile is a
// minimal services-orchestrated entry. Used as the positive fixture for
// DecideQAWorkflowInjection / InjectServicesIntoIntegrationJob tests so they
// do not depend on the embedded catalog fixture set.
func makeServicesSelection() []harnesscatalog.ResolvedSelection {
	return []harnesscatalog.ResolvedSelection{
		{
			Selection: workflow.HarnessProfileSelection{ProfileID: "test.services-profile"},
			Profile: harnesscatalog.Profile{
				ID:            "test.services-profile",
				Orchestration: harnesscatalog.OrchestrationServices,
				Images: []harnesscatalog.ImageRef{
					{Name: "nginx:alpine"},
				},
				Ports: []harnesscatalog.PortRef{
					{Name: "http", ContainerPort: 80},
				},
			},
		},
	}
}

func TestDecideQAWorkflowInjection_OperatorOptOutWins(t *testing.T) {
	pc := &workflow.ProjectConfig{QASkipServiceInjection: true}
	got, err := DecideQAWorkflowInjection(makeServicesSelection(), pc)
	if err != nil {
		t.Fatalf("DecideQAWorkflowInjection error = %v", err)
	}
	if got.Inject {
		t.Errorf("Inject = true, want false when operator opts out")
	}
	if !strings.Contains(got.Reason, "operator opted out") {
		t.Errorf("Reason = %q, want operator-opt-out signal", got.Reason)
	}
}

func TestDecideQAWorkflowInjection_NoServicesSelectionsSkips(t *testing.T) {
	pc := &workflow.ProjectConfig{}
	selections := []harnesscatalog.ResolvedSelection{
		{
			Selection: workflow.HarnessProfileSelection{ProfileID: "test.pure"},
			Profile: harnesscatalog.Profile{
				ID:            "test.pure",
				Orchestration: harnesscatalog.OrchestrationPureFixture,
			},
		},
	}
	got, err := DecideQAWorkflowInjection(selections, pc)
	if err != nil {
		t.Fatalf("DecideQAWorkflowInjection error = %v", err)
	}
	if got.Inject {
		t.Errorf("Inject = true, want false when no services-orchestrated profiles selected")
	}
	if !strings.Contains(got.Reason, "no services-orchestrated") {
		t.Errorf("Reason = %q, want no-services-selected signal", got.Reason)
	}
}

func TestDecideQAWorkflowInjection_EmptyConfigNilSelectionsSkips(t *testing.T) {
	got, err := DecideQAWorkflowInjection(nil, nil)
	if err != nil {
		t.Fatalf("DecideQAWorkflowInjection error = %v", err)
	}
	if got.Inject {
		t.Errorf("Inject = true, want false on empty input")
	}
}

func TestDecideQAWorkflowInjection_ServicesProfileEmitsBlock(t *testing.T) {
	got, err := DecideQAWorkflowInjection(makeServicesSelection(), &workflow.ProjectConfig{})
	if err != nil {
		t.Fatalf("DecideQAWorkflowInjection error = %v", err)
	}
	if !got.Inject {
		t.Fatalf("Inject = false, want true; reason = %q", got.Reason)
	}
	if got.ContainerImage != defaultActJobImage {
		t.Errorf("ContainerImage = %q, want %q", got.ContainerImage, defaultActJobImage)
	}
	if !strings.Contains(got.ServicesYAML, "test-services-profile:") {
		t.Errorf("ServicesYAML missing service entry, got:\n%s", got.ServicesYAML)
	}
	if !strings.Contains(got.ServicesYAML, "image: nginx:alpine") {
		t.Errorf("ServicesYAML missing image, got:\n%s", got.ServicesYAML)
	}
}

func TestInjectServicesIntoIntegrationJob_EmptyServicesYAMLReturnsBaseUnchanged(t *testing.T) {
	base := BuildQAWorkflow(&workflow.ProjectConfig{
		Languages: []workflow.LanguageInfo{{Name: "Go", Primary: true}},
	})
	got, err := InjectServicesIntoIntegrationJob(base, "", "")
	if err != nil {
		t.Fatalf("InjectServicesIntoIntegrationJob error = %v", err)
	}
	if got != base {
		t.Errorf("empty servicesYAML should pass base through unchanged.\n--- got ---\n%s\n--- base ---\n%s", got, base)
	}
}

func TestInjectServicesIntoIntegrationJob_AddsContainerAndServices(t *testing.T) {
	base := BuildQAWorkflow(&workflow.ProjectConfig{
		Languages: []workflow.LanguageInfo{{Name: "Go", Primary: true}},
	})
	decision, err := DecideQAWorkflowInjection(makeServicesSelection(), &workflow.ProjectConfig{})
	if err != nil {
		t.Fatalf("DecideQAWorkflowInjection error = %v", err)
	}
	if !decision.Inject {
		t.Fatalf("expected Inject=true; reason=%q", decision.Reason)
	}

	got, err := InjectServicesIntoIntegrationJob(base, decision.ServicesYAML, decision.ContainerImage)
	if err != nil {
		t.Fatalf("InjectServicesIntoIntegrationJob error = %v", err)
	}

	var doc map[string]any
	if err := yaml.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatalf("re-parse injected qa.yml failed: %v\n%s", err, got)
	}
	jobs, ok := doc["jobs"].(map[string]any)
	if !ok {
		t.Fatalf("jobs key missing or wrong shape; doc=%v", doc)
	}
	integration, ok := jobs["integration"].(map[string]any)
	if !ok {
		t.Fatalf("jobs.integration missing or wrong shape; jobs=%v", jobs)
	}

	container, ok := integration["container"].(map[string]any)
	if !ok {
		t.Fatalf("integration.container missing or wrong shape; integration=%v", integration)
	}
	if container["image"] != defaultActJobImage {
		t.Errorf("container.image = %v, want %q", container["image"], defaultActJobImage)
	}

	services, ok := integration["services"].(map[string]any)
	if !ok {
		t.Fatalf("integration.services missing or wrong shape; integration=%v", integration)
	}
	svc, ok := services["test-services-profile"].(map[string]any)
	if !ok {
		t.Fatalf("services.test-services-profile missing or wrong shape; services=%v", services)
	}
	if svc["image"] != "nginx:alpine" {
		t.Errorf("service.image = %v, want %q", svc["image"], "nginx:alpine")
	}
}

func TestInjectServicesIntoIntegrationJob_RejectsPreexistingServices(t *testing.T) {
	base := `name: QA
on: [push]
jobs:
  integration:
    runs-on: ubuntu-latest
    services:
      preexisting:
        image: redis:alpine
    steps:
      - run: true
`
	_, err := InjectServicesIntoIntegrationJob(base, "anything: { image: x }\n", "")
	if err == nil {
		t.Fatal("expected error when base already declares services block, got nil")
	}
	if !strings.Contains(err.Error(), "qa_skip_service_injection") {
		t.Errorf("error should reference opt-out flag for operator UX; got %v", err)
	}
}

func TestInjectServicesIntoIntegrationJob_MissingIntegrationJobFails(t *testing.T) {
	base := `name: QA
on: [push]
jobs:
  e2e:
    runs-on: ubuntu-latest
    steps:
      - run: true
`
	_, err := InjectServicesIntoIntegrationJob(base, "anything: { image: x }\n", "")
	if err == nil {
		t.Fatal("expected error when base has no integration job, got nil")
	}
	if !strings.Contains(err.Error(), "integration") {
		t.Errorf("error should mention integration job; got %v", err)
	}
}

func TestInjectServicesIntoIntegrationJob_DefaultsContainerImage(t *testing.T) {
	base := BuildQAWorkflow(&workflow.ProjectConfig{
		Languages: []workflow.LanguageInfo{{Name: "Go", Primary: true}},
	})
	decision, _ := DecideQAWorkflowInjection(makeServicesSelection(), &workflow.ProjectConfig{})

	got, err := InjectServicesIntoIntegrationJob(base, decision.ServicesYAML, "")
	if err != nil {
		t.Fatalf("InjectServicesIntoIntegrationJob error = %v", err)
	}
	if !strings.Contains(got, "image: "+defaultActJobImage) {
		t.Errorf("expected default container image %q in output, got:\n%s", defaultActJobImage, got)
	}
}
