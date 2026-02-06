package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/agentic"
	agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"
	"github.com/google/uuid"
)

// ContextCommand implements the /context command for querying the knowledge graph.
type ContextCommand struct{}

// Config returns the command configuration.
func (c *ContextCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/context(?:\s+(.*))?$`,
		Permission:  "view",
		RequireLoop: false,
		Help:        "/context [query|slug] - Query knowledge graph for context",
	}
}

// Execute runs the context command.
func (c *ContextCommand) Execute(
	ctx context.Context,
	cmdCtx *agenticdispatch.CommandContext,
	msg agentic.UserMessage,
	args []string,
	loopID string,
) (agentic.UserResponse, error) {
	query := ""
	if len(args) > 0 {
		query = strings.TrimSpace(args[0])
	}

	// If no query, show summary of all proposal entities
	if query == "" {
		return c.showProposalSummary(ctx, cmdCtx, msg)
	}

	// Check if query looks like a slug (for proposal lookup)
	if !strings.Contains(query, " ") && !strings.HasPrefix(query, "proposal:") {
		return c.showProposalContext(ctx, cmdCtx, msg, query)
	}

	// Generic query (future: implement graph search)
	return c.showGenericContext(ctx, cmdCtx, msg, query)
}

// showProposalSummary shows a summary of all proposal entities in the graph.
func (c *ContextCommand) showProposalSummary(
	ctx context.Context,
	cmdCtx *agenticdispatch.CommandContext,
	msg agentic.UserMessage,
) (agentic.UserResponse, error) {
	gatewayURL := getGatewayURL()

	// Query for all proposal entities
	graphqlQuery := `{
		entities(filter: { predicatePrefix: "semspec.proposal" }) {
			id
			triples {
				predicate
				object
			}
		}
	}`

	result, err := queryGraphQL(ctx, gatewayURL, graphqlQuery)
	if err != nil {
		// Graceful degradation: show helpful message if graph is unavailable
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeResult,
			Content: "## Knowledge Graph Context\n\n" +
				"The graph gateway is not available. Graph entities are created when you run commands like /propose.\n\n" +
				"**To view filesystem-based changes:**\n" +
				"- Run `/changes` to see active changes\n\n" +
				"**To enable graph context:**\n" +
				"1. Ensure graph-gateway is configured in your semspec.json\n" +
				"2. The graph accumulates context as you use semspec\n\n" +
				"Run `/help context` for more information.",
			Timestamp: time.Now(),
		}, nil
	}

	// Parse and format results
	var sb strings.Builder
	sb.WriteString("## Knowledge Graph Context\n\n")

	entities, ok := result["entities"].([]interface{})
	if !ok || len(entities) == 0 {
		sb.WriteString("No proposal entities found in the knowledge graph.\n\n")
		sb.WriteString("Create a proposal with `/propose <description>` to populate the graph.\n")
	} else {
		sb.WriteString("### Proposals\n\n")
		sb.WriteString("| Title | Status | Author |\n")
		sb.WriteString("|-------|--------|--------|\n")

		for _, e := range entities {
			entity, _ := e.(map[string]interface{})
			triples, _ := entity["triples"].([]interface{})

			title := ""
			status := ""
			author := ""

			for _, t := range triples {
				triple, _ := t.(map[string]interface{})
				pred, _ := triple["predicate"].(string)
				obj := triple["object"]

				switch pred {
				case "semspec.proposal.title":
					title, _ = obj.(string)
				case "semspec.proposal.status":
					status, _ = obj.(string)
				case "semspec.proposal.author":
					author, _ = obj.(string)
				}
			}

			if title != "" {
				sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", title, status, author))
			}
		}

		sb.WriteString(fmt.Sprintf("\n*%d proposal(s) in graph*\n", len(entities)))
	}

	sb.WriteString("\n---\n")
	sb.WriteString("Run `/context <slug>` to see context for a specific proposal.\n")

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

// showProposalContext shows context for a specific proposal.
func (c *ContextCommand) showProposalContext(
	ctx context.Context,
	cmdCtx *agenticdispatch.CommandContext,
	msg agentic.UserMessage,
	slug string,
) (agentic.UserResponse, error) {
	gatewayURL := getGatewayURL()
	entityID := workflow.ProposalEntityID(slug)

	// Query for the specific entity and its relationships
	graphqlQuery := fmt.Sprintf(`{
		entity(id: "%s") {
			id
			triples {
				predicate
				object
			}
		}
		traverse(start: "%s", depth: 1) {
			nodes {
				id
				triples {
					predicate
					object
				}
			}
			edges {
				source
				target
				predicate
			}
		}
	}`, entityID, entityID)

	result, err := queryGraphQL(ctx, gatewayURL, graphqlQuery)
	if err != nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to query graph: %v\n\nThe graph gateway may not be available.", err),
			Timestamp:   time.Now(),
		}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Context: %s\n\n", slug))

	// Format entity details
	entity, hasEntity := result["entity"].(map[string]interface{})
	if !hasEntity || entity == nil {
		sb.WriteString("Proposal not found in knowledge graph.\n\n")
		sb.WriteString("This proposal may exist in the filesystem but hasn't been published to the graph.\n")
		sb.WriteString("Run `/changes " + slug + "` to see filesystem status.\n")
	} else {
		sb.WriteString("### Entity Properties\n\n")
		triples, _ := entity["triples"].([]interface{})

		for _, t := range triples {
			triple, _ := t.(map[string]interface{})
			pred, _ := triple["predicate"].(string)
			obj := triple["object"]

			// Format predicate nicely
			shortPred := strings.TrimPrefix(pred, "semspec.proposal.")
			shortPred = strings.TrimPrefix(shortPred, "semspec.spec.")
			shortPred = strings.TrimPrefix(shortPred, "semspec.task.")

			sb.WriteString(fmt.Sprintf("- **%s**: %v\n", shortPred, obj))
		}

		// Format relationships if traversal returned results
		traverse, hasTraverse := result["traverse"].(map[string]interface{})
		if hasTraverse && traverse != nil {
			edges, _ := traverse["edges"].([]interface{})
			if len(edges) > 0 {
				sb.WriteString("\n### Relationships\n\n")
				for _, e := range edges {
					edge, _ := e.(map[string]interface{})
					target, _ := edge["target"].(string)
					pred, _ := edge["predicate"].(string)

					sb.WriteString(fmt.Sprintf("- %s → %s\n", pred, target))
				}
			}
		}
	}

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

// showGenericContext handles free-form queries.
func (c *ContextCommand) showGenericContext(
	ctx context.Context,
	cmdCtx *agenticdispatch.CommandContext,
	msg agentic.UserMessage,
	query string,
) (agentic.UserResponse, error) {
	// For now, explain that generic queries require graph search
	content := fmt.Sprintf("## Query: %s\n\n", query) +
		"Free-form graph search is not yet implemented.\n\n" +
		"**Available queries:**\n" +
		"- `/context` - Show all proposals in the graph\n" +
		"- `/context <slug>` - Show context for a specific proposal\n\n" +
		"**Related commands:**\n" +
		"- `/changes` - List filesystem-based changes\n" +
		"- `/changes <slug>` - Show detailed change status\n"

	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		Type:        agentic.ResponseTypeResult,
		Content:     content,
		Timestamp:   time.Now(),
	}, nil
}

// getGatewayURL returns the graph gateway URL from environment or default.
func getGatewayURL() string {
	if url := os.Getenv("SEMSPEC_GRAPH_GATEWAY_URL"); url != "" {
		return url
	}
	// Default to localhost service-manager port
	return "http://localhost:8080"
}

// queryGraphQL executes a GraphQL query against the graph gateway.
func queryGraphQL(ctx context.Context, gatewayURL, query string) (map[string]interface{}, error) {
	reqBody := map[string]string{"query": query}
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", gatewayURL+"/graphql", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("graph gateway returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data   map[string]interface{} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("graphql error: %s", result.Errors[0].Message)
	}

	return result.Data, nil
}
