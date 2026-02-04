package commands

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/agentic"
	agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"
	"github.com/google/uuid"
)

// CheckCommand implements the /check command for validating against constitution.
type CheckCommand struct{}

// Config returns the command configuration.
func (c *CheckCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/check\s+(.+)$`,
		Permission:  "view",
		RequireLoop: false,
		Help:        "/check <change> - Validate change against constitution",
	}
}

// Execute runs the check command.
func (c *CheckCommand) Execute(
	ctx context.Context,
	cmdCtx *agenticdispatch.CommandContext,
	msg agentic.UserMessage,
	args []string,
	loopID string,
) (agentic.UserResponse, error) {
	slug := ""
	if len(args) > 0 {
		slug = strings.TrimSpace(args[0])
	}

	if slug == "" {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     "Usage: /check <change>",
			Timestamp:   time.Now(),
		}, nil
	}

	// Get repo root from environment or current directory
	repoRoot := os.Getenv("SEMSPEC_REPO_PATH")
	if repoRoot == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			return agentic.UserResponse{
				ResponseID:  uuid.New().String(),
				ChannelType: msg.ChannelType,
				ChannelID:   msg.ChannelID,
				UserID:      msg.UserID,
				Type:        agentic.ResponseTypeError,
				Content:     fmt.Sprintf("Failed to get working directory: %v", err),
				Timestamp:   time.Now(),
			}, nil
		}
	}

	manager := workflow.NewManager(repoRoot)

	// Load the change
	change, err := manager.LoadChange(slug)
	if err != nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Change not found: %s", slug),
			Timestamp:   time.Now(),
		}, nil
	}

	// Load the constitution
	constitution, err := manager.LoadConstitution()
	if err != nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Constitution not found. Create `.semspec/constitution.md` first."),
			Timestamp:   time.Now(),
		}, nil
	}

	// Read all change files for checking
	var allContent strings.Builder
	if change.Files.HasProposal {
		content, _ := manager.ReadProposal(slug)
		allContent.WriteString(content)
		allContent.WriteString("\n\n")
	}
	if change.Files.HasDesign {
		content, _ := manager.ReadDesign(slug)
		allContent.WriteString(content)
		allContent.WriteString("\n\n")
	}
	if change.Files.HasSpec {
		content, _ := manager.ReadSpec(slug)
		allContent.WriteString(content)
		allContent.WriteString("\n\n")
	}
	if change.Files.HasTasks {
		content, _ := manager.ReadTasks(slug)
		allContent.WriteString(content)
	}

	// Perform constitution check
	result := checkAgainstConstitution(constitution, allContent.String(), change)

	cmdCtx.Logger.Info("Constitution check completed",
		"user_id", msg.UserID,
		"slug", change.Slug,
		"passed", result.Passed,
		"violations", len(result.Violations))

	// Build response
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Constitution Check: %s\n\n", change.Title))

	if result.Passed {
		sb.WriteString("✓ **All checks passed**\n\n")
		sb.WriteString(fmt.Sprintf("Validated against %d principles.\n\n", len(constitution.Principles)))
		sb.WriteString("Next steps:\n")
		sb.WriteString(fmt.Sprintf("- Run `/approve %s` to mark ready for implementation\n", change.Slug))
	} else {
		sb.WriteString("✗ **Check failed**\n\n")
		sb.WriteString("### Violations\n\n")
		for _, v := range result.Violations {
			sb.WriteString(fmt.Sprintf("- **Principle %d (%s)**: %s\n",
				v.Principle.Number, v.Principle.Title, v.Message))
		}
		sb.WriteString("\n")
		sb.WriteString("Please address these violations before approval.\n")
	}

	responseType := agentic.ResponseTypeResult
	if !result.Passed {
		responseType = agentic.ResponseTypeError
	}

	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		Type:        responseType,
		Content:     sb.String(),
		Timestamp:   time.Now(),
	}, nil
}

// hasTestingSectionHeader checks if content has a level-2 Testing section header.
// We check for "## testing" at the start of a line to avoid matching "### testing".
func hasTestingSectionHeader(contentLower string) bool {
	// Check for ## testing, ## test plan, or ## tests at start of line
	lines := strings.Split(contentLower, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## testing") ||
			strings.HasPrefix(trimmed, "## test plan") ||
			strings.HasPrefix(trimmed, "## tests") {
			return true
		}
	}
	return false
}

// checkAgainstConstitution validates content against constitution principles.
func checkAgainstConstitution(constitution *workflow.Constitution, content string, change *workflow.Change) *workflow.CheckResult {
	result := &workflow.CheckResult{
		Passed:    true,
		CheckedAt: time.Now(),
	}

	contentLower := strings.ToLower(content)

	for _, principle := range constitution.Principles {
		switch {
		case strings.Contains(strings.ToLower(principle.Title), "test"):
			// Check for actual testing section header at start of line
			if !hasTestingSectionHeader(contentLower) {
				result.Passed = false
				result.Violations = append(result.Violations, workflow.CheckViolation{
					Principle: principle,
					Message:   "Missing ## Testing section in specification",
				})
			}
		case strings.Contains(strings.ToLower(principle.Title), "database") ||
			strings.Contains(strings.ToLower(principle.Title), "repository"):
			// Check for direct database access patterns
			if strings.Contains(contentLower, "sql.db") || strings.Contains(contentLower, "direct database") {
				result.Passed = false
				result.Violations = append(result.Violations, workflow.CheckViolation{
					Principle: principle,
					Message:   "Proposal mentions direct database access instead of repository pattern",
				})
			}
		case strings.Contains(strings.ToLower(principle.Title), "error"):
			// Check for error handling mentions if implementation is involved
			if change.Files.HasSpec && !strings.Contains(contentLower, "error") {
				result.Passed = false
				result.Violations = append(result.Violations, workflow.CheckViolation{
					Principle: principle,
					Message:   "Specification does not mention error handling",
				})
			}
		case strings.Contains(strings.ToLower(principle.Title), "documentation"):
			// Check for documentation mentions
			if change.Files.HasSpec && !strings.Contains(contentLower, "document") {
				result.Passed = false
				result.Violations = append(result.Violations, workflow.CheckViolation{
					Principle: principle,
					Message:   "No mention of documentation in the change",
				})
			}
		}
	}

	return result
}
