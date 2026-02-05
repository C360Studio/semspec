package commands

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/c360studio/semspec/export"
	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/agentic"
	agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"
	"github.com/google/uuid"
)

// ExportCommand implements the /export command for exporting proposals as RDF.
type ExportCommand struct{}

// Config returns the command configuration.
func (c *ExportCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/export\s+(\S+)(?:\s+(\S+))?(?:\s+(\S+))?$`,
		Permission:  "view",
		RequireLoop: false,
		Help:        "/export <slug> [format] [profile] - Export proposal as RDF (formats: turtle, ntriples, jsonld; profiles: minimal, bfo, cco)",
	}
}

// Execute runs the export command.
func (c *ExportCommand) Execute(
	ctx context.Context,
	cmdCtx *agenticdispatch.CommandContext,
	msg agentic.UserMessage,
	args []string,
	loopID string,
) (agentic.UserResponse, error) {
	// Check context cancellation early
	if err := ctx.Err(); err != nil {
		return c.errorResponse(msg, fmt.Sprintf("Request cancelled: %v", err))
	}

	// Parse arguments
	if len(args) == 0 || args[0] == "" {
		return c.errorResponse(msg, "Usage: /export <slug> [format] [profile]\n\nFormats: turtle (default), ntriples, jsonld\nProfiles: minimal (default), bfo, cco")
	}

	slug := strings.TrimSpace(args[0])

	// Parse format (default: turtle)
	format := export.FormatTurtle
	if len(args) > 1 && args[1] != "" {
		switch strings.ToLower(strings.TrimSpace(args[1])) {
		case "turtle", "ttl":
			format = export.FormatTurtle
		case "ntriples", "nt":
			format = export.FormatNTriples
		case "jsonld", "json-ld":
			format = export.FormatJSONLD
		default:
			return c.errorResponse(msg, fmt.Sprintf("Unknown format: %s\n\nSupported formats: turtle, ntriples, jsonld", args[1]))
		}
	}

	// Parse profile (default: minimal)
	profile := export.ProfileMinimal
	if len(args) > 2 && args[2] != "" {
		switch strings.ToLower(strings.TrimSpace(args[2])) {
		case "minimal":
			profile = export.ProfileMinimal
		case "bfo":
			profile = export.ProfileBFO
		case "cco":
			profile = export.ProfileCCO
		default:
			return c.errorResponse(msg, fmt.Sprintf("Unknown profile: %s\n\nSupported profiles: minimal, bfo, cco", args[2]))
		}
	}

	// Get repo root
	repoRoot := os.Getenv("SEMSPEC_REPO_PATH")
	if repoRoot == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			return c.errorResponse(msg, fmt.Sprintf("Failed to get working directory: %v", err))
		}
	}

	// Load the proposal
	manager := workflow.NewManager(repoRoot)
	change, err := manager.LoadChange(slug)
	if err != nil {
		cmdCtx.Logger.Error("Failed to load change for export", "slug", slug, "error", err)
		return c.errorResponse(msg, fmt.Sprintf("Failed to load change '%s': %v", slug, err))
	}

	// Create exporter with profile
	exporter := export.NewRDFExporter(profile)

	// Build entity ID following semspec naming convention
	entityID := fmt.Sprintf("semspec.local.workflow.proposal.%s", change.Slug)

	// Build triples from proposal data
	triples := c.buildProposalTriples(entityID, change)

	// Add entity to exporter
	exporter.AddEntityFromTriples(entityID, semspec.EntityTypeProposal, triples)

	cmdCtx.Logger.Info("Exporting proposal to RDF", "slug", change.Slug, "format", format, "profile", profile)

	// Export to specified format
	output, err := exporter.Export(format)
	if err != nil {
		return c.errorResponse(msg, fmt.Sprintf("Export failed: %v", err))
	}

	// Build response with format info
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## RDF Export: %s\n\n", change.Title))
	sb.WriteString(fmt.Sprintf("**Format**: %s | **Profile**: %s\n\n", format, profile))
	sb.WriteString("```")

	// Add file extension hint for syntax highlighting
	switch format {
	case export.FormatTurtle:
		sb.WriteString("turtle")
	case export.FormatJSONLD:
		sb.WriteString("json")
	case export.FormatNTriples:
		sb.WriteString("ntriples")
	}
	sb.WriteString("\n")
	sb.WriteString(output)
	sb.WriteString("```\n")

	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		Type:        agentic.ResponseTypeResult,
		Content:     sb.String(),
		Timestamp:   time.Now(),
	}, nil
}

// buildProposalTriples creates RDF triples from a Change.
func (c *ExportCommand) buildProposalTriples(entityID string, change *workflow.Change) []export.Triple {
	triples := []export.Triple{
		{
			Subject:   entityID,
			Predicate: semspec.DCTitle,
			Object:    change.Title,
		},
		{
			Subject:   entityID,
			Predicate: semspec.PropStatus,
			Object:    string(change.Status),
		},
		{
			Subject:   entityID,
			Predicate: semspec.PropSlug,
			Object:    change.Slug,
		},
		{
			Subject:   entityID,
			Predicate: semspec.ProvGeneratedAt,
			Object:    change.CreatedAt.Format(time.RFC3339),
		},
	}

	// Add author if present
	if change.Author != "" {
		triples = append(triples, export.Triple{
			Subject:   entityID,
			Predicate: semspec.ProposalAuthor,
			Object:    change.Author,
		})
	}

	return triples
}

// errorResponse creates an error response.
func (c *ExportCommand) errorResponse(msg agentic.UserMessage, content string) (agentic.UserResponse, error) {
	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		Type:        agentic.ResponseTypeError,
		Content:     content,
		Timestamp:   time.Now(),
	}, nil
}
