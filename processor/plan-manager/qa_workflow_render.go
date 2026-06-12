package planmanager

import (
	"os"
	"path/filepath"

	projectmanager "github.com/c360studio/semspec/processor/project-manager"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/harnesscatalog"
)

// maybeRenderQAWithServices is the ADR-039 Phase 1c entry point on QA
// dispatch. It returns true when it took ownership of writing qa.yml so the
// caller skips the legacy EnsureQAWorkflow fallback. Returns false when no
// catalog injection applies (no architecture, no services-orchestrated
// selections, operator opt-out, catalog load failure) so the caller writes
// the language-aware scaffold instead.
//
// Render failures log a warning and return false — the operator's CI will
// surface a clearer error against whichever qa.yml is on disk than a silent
// partial write would.
func (c *Component) maybeRenderQAWithServices(plan *workflow.Plan, pc *workflow.ProjectConfig) bool {
	if plan == nil || plan.Architecture == nil || len(plan.Architecture.HarnessProfiles) == 0 {
		return false
	}

	repoRoot := c.resolveRepoRoot()

	catalog, err := harnesscatalog.Load(repoRoot)
	if err != nil {
		c.logger.Warn("Failed to load harness catalog for qa.yml render — falling back to scaffold",
			"slug", plan.Slug, "error", err)
		return false
	}

	selections, err := catalog.ResolveSelections(plan.Architecture.HarnessProfiles)
	if err != nil {
		c.logger.Warn("Failed to resolve harness profiles for qa.yml render — falling back to scaffold",
			"slug", plan.Slug, "error", err)
		return false
	}

	decision, err := projectmanager.DecideQAWorkflowInjection(selections, pc)
	if err != nil {
		c.logger.Warn("Catalog services render failed — falling back to scaffold",
			"slug", plan.Slug, "error", err)
		return false
	}
	if !decision.Inject {
		c.logger.Info("Skipping qa.yml service injection",
			"slug", plan.Slug, "reason", decision.Reason)
		return false
	}

	base := projectmanager.BuildQAWorkflow(pc)
	injected, err := projectmanager.InjectServicesIntoIntegrationJob(base, decision.ServicesYAML, decision.ContainerImage)
	if err != nil {
		c.logger.Warn("Failed to inject services into qa.yml — falling back to scaffold",
			"slug", plan.Slug, "error", err)
		return false
	}

	workflowPath := filepath.Join(repoRoot, ".github", "workflows", "qa.yml")
	if err := os.MkdirAll(filepath.Dir(workflowPath), 0o755); err != nil {
		c.logger.Warn("Failed to mkdir .github/workflows for injected qa.yml",
			"slug", plan.Slug, "error", err)
		return false
	}
	if err := os.WriteFile(workflowPath, []byte(injected), 0o644); err != nil {
		c.logger.Warn("Failed to write injected qa.yml — falling back to scaffold",
			"slug", plan.Slug, "error", err)
		return false
	}

	c.logger.Info("Rendered qa.yml with catalog services + container block (ADR-039 Phase 1c)",
		"slug", plan.Slug,
		"path", workflowPath,
		"profiles", len(plan.Architecture.HarnessProfiles),
		"container_image", decision.ContainerImage)
	return true
}
