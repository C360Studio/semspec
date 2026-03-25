package executionmanager

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/google/uuid"
)

// buildTeamKnowledgeBlock returns a prompt section containing the team's shared
// knowledge filtered by skill and error categories. Returns "" when teams are
// disabled or when no relevant insights exist.
func (c *Component) buildTeamKnowledgeBlock(ctx context.Context, teamID, skill string, categories []string) string {
	if c.agentHelper == nil || teamID == "" {
		return ""
	}

	team, err := c.agentHelper.GetTeam(ctx, teamID)
	if err != nil || team == nil {
		return ""
	}

	var sb strings.Builder

	// Team motivation framing — always included for team-mode agents.
	sb.WriteString("\n\n---\n\n")
	sb.WriteString("TEAM CONTEXT\n\n")
	sb.WriteString(fmt.Sprintf("You are a member of Team %s. ", team.Name))
	sb.WriteString("All teams are working toward the shared goal of building an excellent project together. ")
	sb.WriteString("We learn from each other when we do our jobs well — ")
	sb.WriteString("blue team implementations help red teams understand quality, and red team challenges help blue teams grow. ")
	sb.WriteString("Constructive, high-quality work is valued over nitpicking or rubber-stamping. ")
	sb.WriteString("The team with the highest combined score (implementation quality + critique quality) earns the coveted Team Trophy.\n")

	// Filtered insights — appended only when relevant lessons exist.
	insights := team.FilterInsights(skill, categories, 10)
	if len(insights) > 0 {
		sb.WriteString("\nTEAM LESSONS LEARNED\n\n")
		for _, insight := range insights {
			sb.WriteString(fmt.Sprintf("- [%s] %s\n", insight.Source, insight.Summary))
		}
	}

	return sb.String()
}

// extractTeamInsights creates TeamInsight entries from reviewer feedback and
// stores them in the respective team's shared knowledge. Called after the
// reviewer completes, regardless of verdict.
func (c *Component) extractTeamInsights(ctx context.Context, exec *taskExecution, feedback string) {
	if c.agentHelper == nil {
		return
	}

	// Blue team insight from rejection feedback.
	if exec.BlueTeamID != "" && feedback != "" {
		// Classify feedback into error categories for filtering.
		var categoryIDs []string
		if c.errorCategories != nil {
			matches := c.errorCategories.MatchSignals(feedback)
			for _, m := range matches {
				categoryIDs = append(categoryIDs, m.Category.ID)
			}
		}

		// Determine skill from the rejection routing: test-related categories
		// indicate the tester should receive this lesson; all others go to builder.
		skill := "builder" // default: most rejections are implementation issues
		for _, cat := range categoryIDs {
			if cat == errorCategoryMissingTests || cat == errorCategoryEdgeCaseMissed {
				skill = "tester"
				break
			}
		}

		insight := workflow.TeamInsight{
			ID:          uuid.New().String(),
			Source:      "reviewer-feedback",
			ScenarioID:  exec.TaskID,
			Summary:     truncateInsight(feedback, 200),
			CategoryIDs: categoryIDs,
			Skill:       skill,
			CreatedAt:   time.Now(),
		}

		if err := c.agentHelper.AddTeamInsight(ctx, exec.BlueTeamID, insight); err != nil {
			c.logger.Warn("Failed to add blue team insight",
				"team_id", exec.BlueTeamID, "error", err)
		}
	}

	// Red team insight from reviewer assessment of critique quality.
	// Only store when a red team challenge was actually run and the critique scored poorly.
	if exec.RedTeamID != "" && exec.RedTeamChallenge != nil {
		if exec.RedTeamChallenge.OverallScore <= 2 {
			insight := workflow.TeamInsight{
				ID:         uuid.New().String(),
				Source:     "red-team-critique-feedback",
				ScenarioID: exec.TaskID,
				Summary:    fmt.Sprintf("Critique quality scored %d/5. Focus on accuracy and actionable feedback.", exec.RedTeamChallenge.OverallScore),
				Skill:      "red-team",
				CreatedAt:  time.Now(),
			}
			if err := c.agentHelper.AddTeamInsight(ctx, exec.RedTeamID, insight); err != nil {
				c.logger.Warn("Failed to add red team insight",
					"team_id", exec.RedTeamID, "error", err)
			}
		}
	}
}

// truncateInsight truncates s to maxLen runes, appending "..." if truncated.
// Operates on runes to avoid splitting multi-byte UTF-8 characters.
func truncateInsight(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}

// checkTeamBenching evaluates whether a team should be benched based on the
// number of individually benched members. A team is benched when a majority
// (>= len/2+1) of its members are benched.
func (c *Component) checkTeamBenching(ctx context.Context, teamID string) bool {
	if c.agentHelper == nil || teamID == "" {
		return false
	}

	team, err := c.agentHelper.GetTeam(ctx, teamID)
	if err != nil || team == nil {
		return false
	}

	benchedCount := 0
	for _, memberID := range team.MemberIDs {
		agent, err := c.agentHelper.GetAgent(ctx, memberID)
		if err != nil {
			continue
		}
		if agent.IsBenched() {
			benchedCount++
		}
	}

	threshold := len(team.MemberIDs)/2 + 1
	if benchedCount >= threshold {
		if err := c.agentHelper.SetTeamStatus(ctx, teamID, workflow.TeamBenched); err != nil {
			c.logger.Warn("Failed to bench team", "team_id", teamID, "error", err)
			return false
		}
		c.logger.Info("Team benched — majority of members are benched",
			"team_id", teamID,
			"team_name", team.Name,
			"benched_members", benchedCount,
			"total_members", len(team.MemberIDs),
		)
		return true
	}
	return false
}
