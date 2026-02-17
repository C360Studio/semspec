package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/agentic"
)

// DocumentExecutor implements document read/write tools for workflow.
type DocumentExecutor struct {
	repoRoot string
}

// NewDocumentExecutor creates a new document executor.
func NewDocumentExecutor(repoRoot string) *DocumentExecutor {
	return &DocumentExecutor{repoRoot: repoRoot}
}

// Execute executes a document tool call.
func (e *DocumentExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	switch call.Name {
	case "workflow_read_document":
		return e.readDocument(ctx, call)
	case "workflow_write_document":
		return e.writeDocument(ctx, call)
	case "workflow_list_documents":
		return e.listDocuments(ctx, call)
	case "workflow_get_plan_status":
		return e.getPlanStatus(ctx, call)
	default:
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("unknown tool: %s", call.Name),
		}, fmt.Errorf("unknown tool: %s", call.Name)
	}
}

// ListTools returns the tool definitions for document operations.
func (e *DocumentExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        "workflow_read_document",
			Description: "Read a workflow document (plan.md, tasks.md) for a plan. Use this to read previously generated documents as context for generating subsequent documents.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"slug": map[string]any{
						"type":        "string",
						"description": "The plan slug (e.g., 'add-user-authentication')",
					},
					"document": map[string]any{
						"type":        "string",
						"enum":        []string{"plan", "tasks", "constitution"},
						"description": "The document type to read",
					},
				},
				"required": []string{"slug", "document"},
			},
		},
		{
			Name:        "workflow_write_document",
			Description: "Write content to a workflow document. Use this to save generated document content. The document will be created in .semspec/projects/default/plans/{slug}/. IMPORTANT: Write complete, well-formatted markdown content.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"slug": map[string]any{
						"type":        "string",
						"description": "The plan slug (e.g., 'add-user-authentication')",
					},
					"document": map[string]any{
						"type":        "string",
						"enum":        []string{"plan", "tasks"},
						"description": "The document type to write",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "The complete markdown content for the document",
					},
				},
				"required": []string{"slug", "document", "content"},
			},
		},
		{
			Name:        "workflow_list_documents",
			Description: "List all documents that exist for a plan. Returns which workflow documents (plan, tasks) have been created.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"slug": map[string]any{
						"type":        "string",
						"description": "The plan slug to check",
					},
				},
				"required": []string{"slug"},
			},
		},
		{
			Name:        "workflow_get_plan_status",
			Description: "Get the current status of a plan, including metadata and which documents exist. Use this to understand the current state of a workflow.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"slug": map[string]any{
						"type":        "string",
						"description": "The plan slug",
					},
				},
				"required": []string{"slug"},
			},
		},
	}
}

// readDocument reads a workflow document.
func (e *DocumentExecutor) readDocument(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	slug, ok := call.Arguments["slug"].(string)
	if !ok || slug == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "slug argument is required",
		}, nil
	}

	docType, ok := call.Arguments["document"].(string)
	if !ok || docType == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "document argument is required",
		}, nil
	}

	manager := workflow.NewManager(e.repoRoot)

	var content string
	var err error

	switch docType {
	case "plan":
		docPath := filepath.Join(manager.ProjectPlanPath(workflow.DefaultProjectSlug, slug), "plan.md")
		data, readErr := os.ReadFile(docPath)
		content, err = string(data), readErr
	case "tasks":
		docPath := filepath.Join(manager.ProjectPlanPath(workflow.DefaultProjectSlug, slug), "tasks.md")
		data, readErr := os.ReadFile(docPath)
		content, err = string(data), readErr
	case "constitution":
		// Constitution is at .semspec/constitution.md, not per-plan
		constitution, loadErr := manager.LoadConstitution()
		if loadErr != nil {
			return agentic.ToolResult{
				CallID: call.ID,
				Error:  fmt.Sprintf("constitution not found or invalid: %v", loadErr),
			}, nil
		}
		// Format constitution as markdown
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("# Project Constitution\n\nVersion: %s\n\n## Principles\n\n", constitution.Version))
		for _, p := range constitution.Principles {
			sb.WriteString(fmt.Sprintf("### %d. %s\n\n%s\n\n", p.Number, p.Title, p.Description))
			if p.Rationale != "" {
				sb.WriteString(fmt.Sprintf("Rationale: %s\n\n", p.Rationale))
			}
		}
		content = sb.String()
	default:
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("unknown document type: %s", docType),
		}, nil
	}

	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("document not found: %v", err),
		}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: content,
	}, nil
}

// writeDocument writes content to a workflow document.
func (e *DocumentExecutor) writeDocument(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	slug, ok := call.Arguments["slug"].(string)
	if !ok || slug == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "slug argument is required",
		}, nil
	}

	docType, ok := call.Arguments["document"].(string)
	if !ok || docType == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "document argument is required",
		}, nil
	}

	content, ok := call.Arguments["content"].(string)
	if !ok || content == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "content argument is required",
		}, nil
	}

	manager := workflow.NewManager(e.repoRoot)

	// Use project-based path for plan documents
	planPath := manager.ProjectPlanPath(workflow.DefaultProjectSlug, slug)
	if _, err := os.Stat(planPath); os.IsNotExist(err) {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("plan '%s' not found. Run /plan first to create it.", slug),
		}, nil
	}

	var filename string

	switch docType {
	case "plan":
		filename = "plan.md"
	case "tasks":
		filename = "tasks.md"
	default:
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("cannot write document type: %s", docType),
		}, nil
	}

	// Write directly to project plan path
	docPath := filepath.Join(planPath, filename)
	if err := os.WriteFile(docPath, []byte(content), 0644); err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("failed to write document: %v", err),
		}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: fmt.Sprintf("Successfully wrote %s to .semspec/projects/default/plans/%s/%s (%d bytes)", docType, slug, filename, len(content)),
	}, nil
}

// listDocuments lists which documents exist for a plan.
func (e *DocumentExecutor) listDocuments(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	slug, ok := call.Arguments["slug"].(string)
	if !ok || slug == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "slug argument is required",
		}, nil
	}

	manager := workflow.NewManager(e.repoRoot)
	planPath := manager.ProjectPlanPath(workflow.DefaultProjectSlug, slug)

	docs := map[string]bool{
		"plan":  fileExists(filepath.Join(planPath, "plan.md")),
		"tasks": fileExists(filepath.Join(planPath, "tasks.md")),
	}

	// Check for constitution
	constitutionPath := filepath.Join(e.repoRoot, ".semspec", "constitution.md")
	docs["constitution"] = fileExists(constitutionPath)

	output, _ := json.MarshalIndent(docs, "", "  ")
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: string(output),
	}, nil
}

// getPlanStatus returns the full status of a plan.
func (e *DocumentExecutor) getPlanStatus(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	slug, ok := call.Arguments["slug"].(string)
	if !ok || slug == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "slug argument is required",
		}, nil
	}

	manager := workflow.NewManager(e.repoRoot)

	plan, err := manager.LoadPlan(ctx, slug)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("plan not found: %v", err),
		}, nil
	}

	planPath := manager.ProjectPlanPath(workflow.DefaultProjectSlug, slug)

	status := map[string]any{
		"slug":       plan.Slug,
		"title":      plan.Title,
		"project_id": plan.ProjectID,
		"approved":   plan.Approved,
		"created_at": plan.CreatedAt.Format("2006-01-02T15:04:05Z"),
		"documents": map[string]bool{
			"plan":  fileExists(filepath.Join(planPath, "plan.md")),
			"tasks": fileExists(filepath.Join(planPath, "tasks.md")),
		},
	}

	if plan.ApprovedAt != nil {
		status["approved_at"] = plan.ApprovedAt.Format("2006-01-02T15:04:05Z")
	}

	output, _ := json.MarshalIndent(status, "", "  ")
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: string(output),
	}, nil
}

// fileExists checks if a file exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
