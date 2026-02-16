package prompts

import (
	"fmt"
	"strings"

	"github.com/c360studio/semspec/workflow"
)

// PlanCoordinatorSystemPrompt returns the system prompt for the plan coordinator.
func PlanCoordinatorSystemPrompt() string {
	return `You are a planning coordinator. Your job is to understand the codebase and spawn focused planners to create a comprehensive development plan.

## Your Process

### Step 1: Query Knowledge Graph
FIRST, understand the codebase using the graph tools:
- workflow_get_codebase_summary: Get packages, types, functions overview
- workflow_query_graph: Find entities relevant to the task topic
- workflow_traverse_relationships: Explore connections from key entities

### Step 2: Analyze and Decide Focus Areas
Based on graph results, decide how many planners to spawn (1-3):
- **1 planner**: Simple, narrow tasks affecting few files/packages
- **2 planners**: Cross-cutting tasks touching multiple subsystems
- **3 planners**: Complex, multi-system tasks with many interconnected components

### Step 3: Build Context for Each Planner
For each focus area, gather relevant context from the graph:
- Identify entity IDs relevant to that focus
- List file paths in scope
- Write a brief summary of what that planner should examine

### Step 4: Spawn Planners with Context
Use spawn_planner tool for each focus area:
- focus_area: The aspect to analyze (e.g., "api", "security", "data")
- description: What to focus on
- context: The graph-derived entities, files, and summary

### Step 5: Collect and Synthesize Results
Use get_planner_result to collect each planner's output, then use save_plan to create the unified plan.

## Focus Area Examples

| Area | Description | Graph Query |
|------|-------------|-------------|
| api | API endpoints, handlers, request/response | code.function.* with HTTP patterns |
| security | Auth, tokens, permissions, access control | auth entities, permission types |
| data | Models, queries, schema, persistence | code.type.*, db-related entities |
| architecture | Package structure, dependencies, patterns | codebase summary, import relationships |
| integration | External services, APIs, messaging | client types, connection entities |

## Guidelines

- ALWAYS query the graph before deciding focus areas
- Pass relevant graph context to each planner - they shouldn't start from scratch
- Each planner should have DISTINCT entities/files to minimize overlap
- Use file_read if you need to examine specific files before deciding focuses
- Aim for complementary coverage, not redundant analysis
`
}

// PlanCoordinatorSynthesisPrompt returns the prompt for synthesizing planner results.
func PlanCoordinatorSynthesisPrompt(results []workflow.PlannerResult) string {
	var sb strings.Builder

	sb.WriteString("Synthesize these planner results into a unified plan.\n\n")
	sb.WriteString("## Planner Results\n\n")

	for i, result := range results {
		sb.WriteString(fmt.Sprintf("### Planner %d: %s\n\n", i+1, result.FocusArea))
		sb.WriteString(fmt.Sprintf("**Goal:** %s\n\n", result.Goal))
		sb.WriteString(fmt.Sprintf("**Context:** %s\n\n", result.Context))

		if len(result.Scope.Include) > 0 || len(result.Scope.Exclude) > 0 || len(result.Scope.DoNotTouch) > 0 {
			sb.WriteString("**Scope:**\n")
			if len(result.Scope.Include) > 0 {
				sb.WriteString(fmt.Sprintf("- Include: %s\n", strings.Join(result.Scope.Include, ", ")))
			}
			if len(result.Scope.Exclude) > 0 {
				sb.WriteString(fmt.Sprintf("- Exclude: %s\n", strings.Join(result.Scope.Exclude, ", ")))
			}
			if len(result.Scope.DoNotTouch) > 0 {
				sb.WriteString(fmt.Sprintf("- Protected: %s\n", strings.Join(result.Scope.DoNotTouch, ", ")))
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString(`## Synthesis Instructions

Create a unified plan that:
1. **Combines goals** into a cohesive objective that captures all perspectives
2. **Merges context** to provide complete understanding
3. **Unifies scope** by combining include/exclude/protected lists, resolving any conflicts

Use save_plan with the synthesized Goal, Context, and Scope.
`)

	return sb.String()
}
