package workflow

import (
	"github.com/c360studio/semspec/pkg/paths"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/c360studio/semspec/workflow/graphutil"
)

// ExportSpecFiles generates per-requirement spec Markdown files in .semspec/specs/.
// Each file contains the requirement description and its scenarios in Given/When/Then format.
// Returns the list of file paths written.
func ExportSpecFiles(ctx context.Context, tw *graphutil.TripleWriter, repoRoot, slug string) ([]string, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	plan, err := LoadPlan(ctx, tw, slug)
	if err != nil {
		return nil, fmt.Errorf("load plan: %w", err)
	}

	requirements, err := LoadRequirements(ctx, tw, slug)
	if err != nil {
		return nil, fmt.Errorf("load requirements: %w", err)
	}

	scenarios, err := LoadScenarios(ctx, tw, slug)
	if err != nil {
		return nil, fmt.Errorf("load scenarios: %w", err)
	}

	// Index scenarios by requirement ID.
	scenariosByReq := make(map[string][]Scenario, len(requirements))
	for _, s := range scenarios {
		scenariosByReq[s.RequirementID] = append(scenariosByReq[s.RequirementID], s)
	}

	specsDir := paths.SpecsPath(repoRoot)
	if err := os.MkdirAll(specsDir, 0755); err != nil {
		return nil, fmt.Errorf("create specs dir: %w", err)
	}

	var written []string
	for _, req := range requirements {
		content := renderSpecFile(plan, &req, scenariosByReq[req.ID])
		reqSlug := paths.Slugify(req.Title)
		if reqSlug == "" {
			reqSlug = req.ID
		}
		filePath := filepath.Join(specsDir, reqSlug+".md")
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			return written, fmt.Errorf("write spec %s: %w", reqSlug, err)
		}
		written = append(written, filePath)
	}

	return written, nil
}

// renderSpecFile renders a single requirement spec as Markdown.
func renderSpecFile(plan *Plan, req *Requirement, scenarios []Scenario) string {
	var b strings.Builder

	b.WriteString("# ")
	b.WriteString(req.Title)
	b.WriteString("\n\n")

	b.WriteString("**Plan:** ")
	b.WriteString(plan.Title)
	b.WriteString("  \n")
	b.WriteString("**Status:** ")
	b.WriteString(string(req.Status))
	b.WriteString("  \n")
	b.WriteString("**Created:** ")
	b.WriteString(req.CreatedAt.Format(time.RFC3339))
	b.WriteString("\n")

	if req.Description != "" {
		b.WriteString("\n## Description\n\n")
		b.WriteString(req.Description)
		b.WriteString("\n")
	}

	if len(req.DependsOn) > 0 {
		b.WriteString("\n## Dependencies\n\n")
		for _, dep := range req.DependsOn {
			b.WriteString("- ")
			b.WriteString(dep)
			b.WriteString("\n")
		}
	}

	if len(scenarios) > 0 {
		b.WriteString("\n## Scenarios\n")
		for _, s := range scenarios {
			b.WriteString("\n### ")
			b.WriteString(s.When)
			b.WriteString("\n\n")
			b.WriteString("**Given** ")
			b.WriteString(s.Given)
			b.WriteString("  \n")
			b.WriteString("**When** ")
			b.WriteString(s.When)
			b.WriteString("  \n")
			b.WriteString("**Then**\n")
			for _, then := range s.Then {
				b.WriteString("- ")
				b.WriteString(then)
				b.WriteString("\n")
			}
			b.WriteString("\n*Status:* ")
			b.WriteString(string(s.Status))
			b.WriteString("\n")
		}
	}

	return b.String()
}

// GenerateArchive generates an archive Markdown document summarising a completed plan.
// The document is written to .semspec/archive/{slug}.md.
// Returns the file path written.
func GenerateArchive(ctx context.Context, tw *graphutil.TripleWriter, repoRoot, slug string) (string, error) {
	if err := ValidateSlug(slug); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	plan, err := LoadPlan(ctx, tw, slug)
	if err != nil {
		return "", fmt.Errorf("load plan: %w", err)
	}

	requirements, err := LoadRequirements(ctx, tw, slug)
	if err != nil {
		return "", fmt.Errorf("load requirements: %w", err)
	}

	scenarios, err := LoadScenarios(ctx, tw, slug)
	if err != nil {
		return "", fmt.Errorf("load scenarios: %w", err)
	}

	changeProposals, err := LoadChangeProposals(ctx, tw, slug)
	if err != nil {
		return "", fmt.Errorf("load change proposals: %w", err)
	}

	content := renderArchive(plan, requirements, scenarios, changeProposals)

	archiveDir := paths.ArchivePath(repoRoot)
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		return "", fmt.Errorf("create archive dir: %w", err)
	}

	filePath := filepath.Join(archiveDir, slug+".md")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write archive: %w", err)
	}

	return filePath, nil
}

// renderArchive renders the archive Markdown for a plan.
func renderArchive(plan *Plan, requirements []Requirement, scenarios []Scenario, changeProposals []ChangeProposal) string {
	var b strings.Builder

	b.WriteString("# Archive: ")
	b.WriteString(plan.Title)
	b.WriteString("\n")

	if plan.Goal != "" {
		b.WriteString("\n## Summary\n\n")
		b.WriteString(plan.Goal)
		b.WriteString("\n")
	}

	renderArchiveTimeline(&b, plan)
	renderArchiveRequirements(&b, requirements, scenarios)
	renderArchiveScenarios(&b, scenarios)
	renderArchiveChangeProposals(&b, changeProposals)

	return b.String()
}

func renderArchiveTimeline(b *strings.Builder, plan *Plan) {
	b.WriteString("\n## Timeline\n\n")
	b.WriteString("- **Created:** ")
	b.WriteString(plan.CreatedAt.Format(time.RFC3339))
	b.WriteString("\n")
	if plan.ApprovedAt != nil {
		b.WriteString("- **Approved:** ")
		b.WriteString(plan.ApprovedAt.Format(time.RFC3339))
		b.WriteString("\n")
	}
	b.WriteString("- **Status:** ")
	b.WriteString(string(plan.EffectiveStatus()))
	b.WriteString("\n")
	if plan.EffectiveStatus() == StatusComplete && plan.ApprovedAt != nil {
		b.WriteString("- **Duration:** ")
		b.WriteString(formatDuration(time.Since(*plan.ApprovedAt)))
		b.WriteString("\n")
	}
}

func renderArchiveRequirements(b *strings.Builder, requirements []Requirement, scenarios []Scenario) {
	b.WriteString(fmt.Sprintf("\n## Requirements (%d)\n\n", len(requirements)))
	if len(requirements) == 0 {
		return
	}
	scenCountByReq := make(map[string]int, len(requirements))
	for _, s := range scenarios {
		scenCountByReq[s.RequirementID]++
	}
	for _, req := range requirements {
		b.WriteString(fmt.Sprintf("- **%s** — %s (%d scenarios)\n", req.Title, req.Status, scenCountByReq[req.ID]))
	}
}

func renderArchiveScenarios(b *strings.Builder, scenarios []Scenario) {
	passing, failing, pending, skipped := 0, 0, 0, 0
	for _, s := range scenarios {
		switch s.Status {
		case ScenarioStatusPassing:
			passing++
		case ScenarioStatusFailing:
			failing++
		case ScenarioStatusPending:
			pending++
		case ScenarioStatusSkipped:
			skipped++
		}
	}
	b.WriteString(fmt.Sprintf("\n## Scenarios (%d)\n\n", len(scenarios)))
	b.WriteString(fmt.Sprintf("- Passing: %d\n- Failing: %d\n- Pending: %d\n- Skipped: %d\n", passing, failing, pending, skipped))
}

func renderArchiveChangeProposals(b *strings.Builder, changeProposals []ChangeProposal) {
	if len(changeProposals) == 0 {
		return
	}
	b.WriteString(fmt.Sprintf("\n## Change Proposals (%d)\n\n", len(changeProposals)))
	for _, cp := range changeProposals {
		b.WriteString(fmt.Sprintf("### %s\n\n", cp.Title))
		b.WriteString(fmt.Sprintf("**Status:** %s  \n", cp.Status))
		b.WriteString(fmt.Sprintf("**Proposed by:** %s  \n", cp.ProposedBy))
		if cp.Rationale != "" {
			b.WriteString(fmt.Sprintf("**Rationale:** %s  \n", cp.Rationale))
		}
		if len(cp.AffectedReqIDs) > 0 {
			b.WriteString(fmt.Sprintf("**Affected requirements:** %s\n", strings.Join(cp.AffectedReqIDs, ", ")))
		}
		b.WriteString("\n")
	}
}

// formatDuration formats a duration in a human-readable way.
func formatDuration(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	}
	hours := int(d.Hours())
	if hours < 24 {
		return fmt.Sprintf("%d hours", hours)
	}
	days := hours / 24
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}
