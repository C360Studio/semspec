package workflowdocuments

import (
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/workflow"
)

// RenderQASummary produces a markdown view of the plan's QA phase
// outcome — the qa-reviewer's prose verdict (when persisted), the
// executor result (QARun), and any plan-decisions the qa-reviewer raised.
// Returns "" when the plan has no QA state at all (pre-verdict, no QARun,
// no PlanDecisions, no QAVerdictSummary).
func RenderQASummary(plan *workflow.Plan) string {
	if plan == nil {
		return ""
	}
	if plan.QARun == nil && plan.QAVerdictSummary == nil && len(plan.PlanDecisions) == 0 {
		return ""
	}
	var b strings.Builder

	title := plan.Title
	if title == "" {
		title = plan.Slug
	}
	b.WriteString(fmt.Sprintf("# QA Summary: %s\n\n", title))
	b.WriteString(fmt.Sprintf("*Generated from the qa-reviewer verdict, the QA phase's executor result, and any plan-decisions the qa-reviewer raised. QA level for this plan: `%s`.*\n\n",
		plan.EffectiveQALevel()))

	renderQAVerdictSummary(&b, plan.QAVerdictSummary)
	renderQARun(&b, plan.QARun)
	renderQAPlanDecisions(&b, plan.PlanDecisions)

	return b.String()
}

// renderQAVerdictSummary renders the qa-reviewer's prose verdict (overall
// summary + per-dimension paragraphs). Each dimension is rendered only when
// non-empty so we surface what the reviewer actually assessed at the plan's
// QA level rather than showing empty headings for dimensions that don't
// apply (e.g. flake_judgment is integration-tier and up).
func renderQAVerdictSummary(b *strings.Builder, v *workflow.QAVerdictSummary) {
	if v == nil {
		return
	}
	b.WriteString("## Reviewer verdict\n\n")
	b.WriteString(fmt.Sprintf("- **Verdict:** `%s`\n", v.Verdict))
	b.WriteString(fmt.Sprintf("- **Level assessed:** `%s`\n", v.Level))
	if !v.RecordedAt.IsZero() {
		b.WriteString(fmt.Sprintf("- **Recorded at:** %s\n", v.RecordedAt.UTC().Format(time.RFC3339)))
	}
	b.WriteString("\n")
	if v.Summary != "" {
		b.WriteString(fmt.Sprintf("%s\n\n", v.Summary))
	}

	dims := []struct {
		label, body string
	}{
		{"Requirement fulfillment", v.Dimensions.RequirementFulfillment},
		{"Coverage", v.Dimensions.Coverage},
		{"Assertion quality", v.Dimensions.AssertionQuality},
		{"Regression surface", v.Dimensions.RegressionSurface},
		{"Flake judgment", v.Dimensions.FlakeJudgment},
	}
	headerWritten := false
	for _, d := range dims {
		if strings.TrimSpace(d.body) == "" {
			continue
		}
		if !headerWritten {
			b.WriteString("### Dimensions\n\n")
			headerWritten = true
		}
		b.WriteString(fmt.Sprintf("**%s.** %s\n\n", d.label, d.body))
	}
}

func renderQARun(b *strings.Builder, run *workflow.QARun) {
	if run == nil {
		b.WriteString("## Executor result\n\n")
		b.WriteString("*No executor run on this plan (QA level `synthesis` or `none` — qa-reviewer renders verdict directly without running tests).*\n\n")
		return
	}
	b.WriteString("## Executor result\n\n")
	status := "FAILED"
	if run.Passed {
		status = "PASSED"
	}
	b.WriteString(fmt.Sprintf("- **Verdict:** %s\n", status))
	b.WriteString(fmt.Sprintf("- **Run ID:** `%s`\n", run.RunID))
	if !run.CompletedAt.IsZero() {
		b.WriteString(fmt.Sprintf("- **Completed at:** %s\n", run.CompletedAt.UTC().Format(time.RFC3339)))
	}
	if run.DurationMs > 0 {
		b.WriteString(fmt.Sprintf("- **Duration:** %.1fs\n", float64(run.DurationMs)/1000.0))
	}
	if run.TraceID != "" {
		b.WriteString(fmt.Sprintf("- **Trace ID:** `%s`\n", run.TraceID))
	}
	if run.RunnerError != "" {
		b.WriteString(fmt.Sprintf("- **Runner error:** %s\n", run.RunnerError))
	}
	b.WriteString("\n")

	if len(run.Failures) > 0 {
		b.WriteString(fmt.Sprintf("### Failures (%d)\n\n", len(run.Failures)))
		for _, f := range run.Failures {
			b.WriteString(fmt.Sprintf("- **%s**", f.JobName))
			if f.Message != "" {
				b.WriteString(fmt.Sprintf(" — %s", f.Message))
			}
			b.WriteString("\n")
			if f.LogExcerpt != "" {
				excerpt := f.LogExcerpt
				if len(excerpt) > 600 {
					excerpt = excerpt[:600] + "\n... [truncated, see plan.json for full log]"
				}
				b.WriteString("\n```\n")
				b.WriteString(excerpt)
				b.WriteString("\n```\n")
			}
		}
		b.WriteString("\n")
	}

	if len(run.Artifacts) > 0 {
		b.WriteString(fmt.Sprintf("### Artifacts (%d)\n\n", len(run.Artifacts)))
		for _, a := range run.Artifacts {
			label := a.Type
			if a.Purpose != "" {
				label = fmt.Sprintf("%s — %s", a.Type, a.Purpose)
			}
			b.WriteString(fmt.Sprintf("- `%s` (%s)\n", a.Path, label))
		}
		b.WriteString("\n")
	}
}

func renderQAPlanDecisions(b *strings.Builder, decisions []workflow.PlanDecision) {
	if len(decisions) == 0 {
		return
	}
	b.WriteString(fmt.Sprintf("## Plan decisions raised (%d)\n\n", len(decisions)))
	b.WriteString("*The qa-reviewer raised the following change proposals. Each is independently reviewable; the plan transitions to `awaiting_review` or `complete` based on the verdict.*\n\n")
	for _, d := range decisions {
		b.WriteString(fmt.Sprintf("### %s\n\n", d.Title))
		b.WriteString(fmt.Sprintf("- **ID:** `%s`\n", d.ID))
		if d.Kind != "" {
			b.WriteString(fmt.Sprintf("- **Kind:** `%s`\n", d.Kind))
		}
		b.WriteString(fmt.Sprintf("- **Status:** `%s`\n", d.Status))
		if d.ProposedBy != "" {
			b.WriteString(fmt.Sprintf("- **Proposed by:** %s\n", d.ProposedBy))
		}
		if len(d.AffectedReqIDs) > 0 {
			b.WriteString(fmt.Sprintf("- **Affects requirements:** %s\n",
				strings.Join(d.AffectedReqIDs, ", ")))
		}
		b.WriteString("\n")
		if d.Rationale != "" {
			b.WriteString(fmt.Sprintf("**Rationale:** %s\n\n", d.Rationale))
		}
		if len(d.ArtifactReferences) > 0 {
			b.WriteString(fmt.Sprintf("**Artifact references (%d):**\n\n", len(d.ArtifactReferences)))
			for _, ar := range d.ArtifactReferences {
				b.WriteString(fmt.Sprintf("- `%s`", ar.Path))
				if ar.Purpose != "" {
					b.WriteString(fmt.Sprintf(" — %s", ar.Purpose))
				}
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
	}
}
