package workflowapi

import (
	"reflect"

	"github.com/c360studio/semstreams/service"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/aggregation"
	"github.com/c360studio/semspec/workflow/prompts"
)

func init() {
	service.RegisterOpenAPISpec("workflow-api", workflowAPIOpenAPISpec())
}

// OpenAPISpec implements the OpenAPIProvider interface.
func (c *Component) OpenAPISpec() *service.OpenAPISpec {
	return workflowAPIOpenAPISpec()
}

// workflowAPIOpenAPISpec returns the OpenAPI specification for workflow-api endpoints.
func workflowAPIOpenAPISpec() *service.OpenAPISpec {
	slugParam := service.ParameterSpec{
		Name:        "slug",
		In:          "path",
		Required:    true,
		Description: "URL-friendly plan identifier",
		Schema:      service.Schema{Type: "string"},
	}

	return &service.OpenAPISpec{
		Tags: []service.TagSpec{
			{Name: "Plans", Description: "Workflow plan management - create, retrieve, and advance development plans through their lifecycle"},
		},
		Paths: map[string]service.PathSpec{
			"/workflow-api/plans": {
				GET: &service.OperationSpec{
					Summary:     "List plans",
					Description: "Returns all development plans with their current workflow stage and active agent loops",
					Tags:        []string{"Plans"},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Array of plans with status",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/PlanWithStatus",
							IsArray:     true,
						},
					},
				},
				POST: &service.OperationSpec{
					Summary:     "Create plan",
					Description: "Creates a new development plan from a description and triggers the planner agent to generate Goal, Context, and Scope",
					Tags:        []string{"Plans"},
					Responses: map[string]service.ResponseSpec{
						"201": {
							Description: "Plan created and planning triggered",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/CreatePlanResponse",
						},
						"200": {
							Description: "Plan already exists, returns current state",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/PlanWithStatus",
						},
						"400": {Description: "Invalid request (missing description)"},
					},
				},
			},
			"/workflow-api/plans/{slug}": {
				GET: &service.OperationSpec{
					Summary:     "Get plan",
					Description: "Returns a single plan with its current workflow stage and active agent loops",
					Tags:        []string{"Plans"},
					Parameters:  []service.ParameterSpec{slugParam},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Plan with current status",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/PlanWithStatus",
						},
						"404": {Description: "Plan not found"},
					},
				},
			},
			"/workflow-api/plans/{slug}/promote": {
				POST: &service.OperationSpec{
					Summary:     "Promote plan",
					Description: "Approves a plan draft, marking it ready for task generation and execution",
					Tags:        []string{"Plans"},
					Parameters:  []service.ParameterSpec{slugParam},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Plan approved and returned with updated status",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/PlanWithStatus",
						},
						"404": {Description: "Plan not found"},
					},
				},
			},
			"/workflow-api/plans/{slug}/tasks": {
				GET: &service.OperationSpec{
					Summary:     "List plan tasks",
					Description: "Returns all tasks associated with the given plan",
					Tags:        []string{"Plans"},
					Parameters:  []service.ParameterSpec{slugParam},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Array of tasks for the plan",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/Task",
							IsArray:     true,
						},
					},
				},
			},
			"/workflow-api/plans/{slug}/tasks/generate": {
				POST: &service.OperationSpec{
					Summary:     "Generate tasks",
					Description: "Triggers the task generator agent to produce executable tasks from an approved plan's Goal, Context, and Scope",
					Tags:        []string{"Plans"},
					Parameters:  []service.ParameterSpec{slugParam},
					Responses: map[string]service.ResponseSpec{
						"202": {
							Description: "Task generation accepted and started asynchronously",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/AsyncOperationResponse",
						},
						"400": {Description: "Plan must be approved before generating tasks"},
						"404": {Description: "Plan not found"},
					},
				},
			},
			"/workflow-api/plans/{slug}/execute": {
				POST: &service.OperationSpec{
					Summary:     "Execute plan",
					Description: "Triggers the batch task dispatcher to execute all tasks for an approved plan",
					Tags:        []string{"Plans"},
					Parameters:  []service.ParameterSpec{slugParam},
					Responses: map[string]service.ResponseSpec{
						"202": {
							Description: "Plan execution accepted and started asynchronously",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/PlanWithStatus",
						},
						"400": {Description: "Plan must be approved before execution"},
						"404": {Description: "Plan not found"},
					},
				},
			},
			"/workflow-api/plans/{slug}/reviews": {
				GET: &service.OperationSpec{
					Summary:     "Get plan reviews",
					Description: "Returns the aggregated review synthesis result for a plan, combining findings from all reviewers",
					Tags:        []string{"Plans"},
					Parameters:  []service.ParameterSpec{slugParam},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Aggregated review synthesis result with verdict, findings, and per-reviewer summaries",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/SynthesisResult",
						},
						"404": {Description: "Plan not found or no completed review available"},
					},
				},
			},
		},
		ResponseTypes: []reflect.Type{
			reflect.TypeOf(PlanWithStatus{}),
			reflect.TypeOf(ActiveLoopStatus{}),
			reflect.TypeOf(CreatePlanRequest{}),
			reflect.TypeOf(CreatePlanResponse{}),
			reflect.TypeOf(AsyncOperationResponse{}),
			reflect.TypeOf(workflow.Plan{}),
			reflect.TypeOf(workflow.Scope{}),
			reflect.TypeOf(workflow.Task{}),
			reflect.TypeOf(workflow.AcceptanceCriterion{}),
			reflect.TypeOf(aggregation.SynthesisResult{}),
			reflect.TypeOf(aggregation.ReviewerSummary{}),
			reflect.TypeOf(aggregation.SynthesisStats{}),
			reflect.TypeOf(prompts.ReviewFinding{}),
		},
	}
}
