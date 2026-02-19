package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/agentic"
)

// ConstitutionExecutor implements constitution validation tools.
type ConstitutionExecutor struct {
	repoRoot string
}

// NewConstitutionExecutor creates a new constitution executor.
func NewConstitutionExecutor(repoRoot string) *ConstitutionExecutor {
	return &ConstitutionExecutor{repoRoot: repoRoot}
}

// Execute executes a constitution tool call.
func (e *ConstitutionExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	switch call.Name {
	case "workflow_check_constitution":
		return e.checkConstitution(ctx, call)
	case "workflow_get_principles":
		return e.getPrinciples(ctx, call)
	default:
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("unknown tool: %s", call.Name),
		}, fmt.Errorf("unknown tool: %s", call.Name)
	}
}

// ListTools returns the tool definitions for constitution operations.
func (e *ConstitutionExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        "workflow_check_constitution",
			Description: "Validate a document against the project constitution principles. Returns a list of potential violations or concerns. Use this to ensure generated documents comply with project standards.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"content": map[string]any{
						"type":        "string",
						"description": "The document content to check against constitution principles",
					},
					"document_type": map[string]any{
						"type":        "string",
						"enum":        []string{"plan", "tasks"},
						"description": "The type of document being checked",
					},
				},
				"required": []string{"content", "document_type"},
			},
		},
		{
			Name:        "workflow_get_principles",
			Description: "Get all constitution principles for the project. Use this to understand the project's coding standards, architectural decisions, and requirements before generating documents.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}
}

// checkConstitution checks content against constitution principles.
func (e *ConstitutionExecutor) checkConstitution(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: err.Error()}, nil
	}
	content, ok := call.Arguments["content"].(string)
	if !ok || content == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "content argument is required",
		}, nil
	}

	docType, ok := call.Arguments["document_type"].(string)
	if !ok || docType == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "document_type argument is required",
		}, nil
	}

	manager := workflow.NewManager(e.repoRoot)

	constitution, err := manager.LoadConstitution()
	if err != nil {
		// No constitution is not an error - just no checks to perform
		return agentic.ToolResult{
			CallID:  call.ID,
			Content: `{"has_constitution": false, "message": "No constitution.md found. Consider creating one to define project principles."}`,
		}, nil
	}

	// Perform basic checks based on content and principles
	concerns := []map[string]any{}
	contentLower := strings.ToLower(content)

	for _, principle := range constitution.Principles {
		// Check for potential violations based on principle keywords
		concern := e.checkPrinciple(principle, contentLower, docType)
		if concern != nil {
			concerns = append(concerns, concern)
		}
	}

	result := map[string]any{
		"has_constitution":   true,
		"principles_checked": len(constitution.Principles),
		"concerns":           concerns,
		"passed":             len(concerns) == 0,
	}

	if len(concerns) == 0 {
		result["message"] = "Document appears to comply with all constitution principles."
	} else {
		result["message"] = fmt.Sprintf("Found %d potential concern(s) to address.", len(concerns))
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: string(output),
	}, nil
}

// checkPrinciple checks if content might violate a principle.
func (e *ConstitutionExecutor) checkPrinciple(principle workflow.Principle, contentLower, docType string) map[string]any {
	titleLower := strings.ToLower(principle.Title)
	descLower := strings.ToLower(principle.Description)

	// Test-First Development principle
	if strings.Contains(titleLower, "test") && strings.Contains(descLower, "before") {
		// For specs and tasks, check if testing is mentioned
		if docType == "spec" || docType == "tasks" {
			if !strings.Contains(contentLower, "test") {
				return map[string]any{
					"principle_number": principle.Number,
					"principle_title":  principle.Title,
					"concern":          "Document doesn't mention testing. Ensure test requirements are included.",
					"severity":         "warning",
				}
			}
		}
	}

	// Documentation Required principle
	if strings.Contains(titleLower, "documentation") {
		if docType == "spec" || docType == "tasks" {
			if !strings.Contains(contentLower, "document") && !strings.Contains(contentLower, "readme") {
				return map[string]any{
					"principle_number": principle.Number,
					"principle_title":  principle.Title,
					"concern":          "Consider including documentation tasks.",
					"severity":         "info",
				}
			}
		}
	}

	// Error Handling principle
	if strings.Contains(titleLower, "error") && strings.Contains(descLower, "explicit") {
		if docType == "design" || docType == "spec" {
			if !strings.Contains(contentLower, "error") {
				return map[string]any{
					"principle_number": principle.Number,
					"principle_title":  principle.Title,
					"concern":          "Document doesn't address error handling. Consider how errors will be handled.",
					"severity":         "warning",
				}
			}
		}
	}

	// Security considerations
	if strings.Contains(titleLower, "security") {
		if docType == "design" {
			if !strings.Contains(contentLower, "security") {
				return map[string]any{
					"principle_number": principle.Number,
					"principle_title":  principle.Title,
					"concern":          "Design doesn't address security considerations.",
					"severity":         "warning",
				}
			}
		}
	}

	return nil
}

// getPrinciples returns all constitution principles.
func (e *ConstitutionExecutor) getPrinciples(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: err.Error()}, nil
	}
	manager := workflow.NewManager(e.repoRoot)

	constitution, err := manager.LoadConstitution()
	if err != nil {
		return agentic.ToolResult{
			CallID:  call.ID,
			Content: `{"has_constitution": false, "message": "No constitution.md found at .semspec/constitution.md"}`,
		}, nil
	}

	principles := make([]map[string]any, 0, len(constitution.Principles))
	for _, p := range constitution.Principles {
		principles = append(principles, map[string]any{
			"number":      p.Number,
			"title":       p.Title,
			"description": p.Description,
			"rationale":   p.Rationale,
		})
	}

	result := map[string]any{
		"has_constitution": true,
		"version":          constitution.Version,
		"ratified":         constitution.Ratified.Format("2006-01-02"),
		"principles":       principles,
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: string(output),
	}, nil
}
