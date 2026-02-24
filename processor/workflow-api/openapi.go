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
			{Name: "Phases", Description: "Phase management - logical groupings of tasks within a plan with dependencies and approval gates"},
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
			// Phase endpoints
			"/workflow-api/plans/{slug}/phases": {
				GET: &service.OperationSpec{
					Summary:     "List phases",
					Description: "Returns all phases for a plan, ordered by sequence",
					Tags:        []string{"Phases"},
					Parameters:  []service.ParameterSpec{slugParam},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Array of phases for the plan",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/Phase",
							IsArray:     true,
						},
						"404": {Description: "Plan not found"},
					},
				},
				POST: &service.OperationSpec{
					Summary:     "Create phase",
					Description: "Creates a new phase within the plan",
					Tags:        []string{"Phases"},
					Parameters:  []service.ParameterSpec{slugParam},
					Responses: map[string]service.ResponseSpec{
						"201": {
							Description: "Phase created successfully",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/Phase",
						},
						"400": {Description: "Invalid request body"},
						"404": {Description: "Plan not found"},
					},
				},
			},
			"/workflow-api/plans/{slug}/phases/generate": {
				POST: &service.OperationSpec{
					Summary:     "Generate phases",
					Description: "Triggers the LLM to generate phases from an approved plan's Goal, Context, and Scope",
					Tags:        []string{"Phases"},
					Parameters:  []service.ParameterSpec{slugParam},
					Responses: map[string]service.ResponseSpec{
						"202": {
							Description: "Phase generation accepted and started asynchronously",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/AsyncOperationResponse",
						},
						"400": {Description: "Plan must be approved before generating phases"},
						"404": {Description: "Plan not found"},
					},
				},
			},
			"/workflow-api/plans/{slug}/phases/approve": {
				POST: &service.OperationSpec{
					Summary:     "Approve all phases",
					Description: "Bulk-approves all pending phases for a plan",
					Tags:        []string{"Phases"},
					Parameters:  []service.ParameterSpec{slugParam},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "All phases approved, returns updated phases",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/Phase",
							IsArray:     true,
						},
						"404": {Description: "Plan not found"},
					},
				},
			},
			"/workflow-api/plans/{slug}/phases/reorder": {
				PUT: &service.OperationSpec{
					Summary:     "Reorder phases",
					Description: "Reorders phases within the plan by specifying new sequence order",
					Tags:        []string{"Phases"},
					Parameters:  []service.ParameterSpec{slugParam},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Phases reordered, returns updated phases",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/Phase",
							IsArray:     true,
						},
						"400": {Description: "Invalid phase IDs"},
						"404": {Description: "Plan not found"},
					},
				},
			},
			"/workflow-api/plans/{slug}/phases/{phaseId}": {
				GET: &service.OperationSpec{
					Summary:     "Get phase",
					Description: "Returns a single phase by ID",
					Tags:        []string{"Phases"},
					Parameters: []service.ParameterSpec{
						slugParam,
						{Name: "phaseId", In: "path", Required: true, Description: "Phase identifier", Schema: service.Schema{Type: "string"}},
					},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Phase details",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/Phase",
						},
						"404": {Description: "Phase not found"},
					},
				},
				PUT: &service.OperationSpec{
					Summary:     "Update phase",
					Description: "Updates a phase's name, description, dependencies, or agent config",
					Tags:        []string{"Phases"},
					Parameters: []service.ParameterSpec{
						slugParam,
						{Name: "phaseId", In: "path", Required: true, Description: "Phase identifier", Schema: service.Schema{Type: "string"}},
					},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Phase updated",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/Phase",
						},
						"400": {Description: "Invalid request body"},
						"404": {Description: "Phase not found"},
					},
				},
				DELETE: &service.OperationSpec{
					Summary:     "Delete phase",
					Description: "Deletes a phase and reassigns its tasks to the default phase",
					Tags:        []string{"Phases"},
					Parameters: []service.ParameterSpec{
						slugParam,
						{Name: "phaseId", In: "path", Required: true, Description: "Phase identifier", Schema: service.Schema{Type: "string"}},
					},
					Responses: map[string]service.ResponseSpec{
						"204": {Description: "Phase deleted"},
						"404": {Description: "Phase not found"},
					},
				},
			},
			"/workflow-api/plans/{slug}/phases/{phaseId}/approve": {
				POST: &service.OperationSpec{
					Summary:     "Approve phase",
					Description: "Approves a single phase for execution",
					Tags:        []string{"Phases"},
					Parameters: []service.ParameterSpec{
						slugParam,
						{Name: "phaseId", In: "path", Required: true, Description: "Phase identifier", Schema: service.Schema{Type: "string"}},
					},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Phase approved",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/Phase",
						},
						"404": {Description: "Phase not found"},
					},
				},
			},
			"/workflow-api/plans/{slug}/phases/{phaseId}/reject": {
				POST: &service.OperationSpec{
					Summary:     "Reject phase",
					Description: "Rejects a phase with a reason",
					Tags:        []string{"Phases"},
					Parameters: []service.ParameterSpec{
						slugParam,
						{Name: "phaseId", In: "path", Required: true, Description: "Phase identifier", Schema: service.Schema{Type: "string"}},
					},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Phase rejected",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/Phase",
						},
						"400": {Description: "Rejection reason required"},
						"404": {Description: "Phase not found"},
					},
				},
			},
			"/workflow-api/plans/{slug}/phases/{phaseId}/tasks": {
				GET: &service.OperationSpec{
					Summary:     "List phase tasks",
					Description: "Returns all tasks belonging to a specific phase",
					Tags:        []string{"Phases"},
					Parameters: []service.ParameterSpec{
						slugParam,
						{Name: "phaseId", In: "path", Required: true, Description: "Phase identifier", Schema: service.Schema{Type: "string"}},
					},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Array of tasks for the phase",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/Task",
							IsArray:     true,
						},
						"404": {Description: "Phase not found"},
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
			// Phase types
			reflect.TypeOf(workflow.Phase{}),
			reflect.TypeOf(workflow.PhaseStatus("")),
			reflect.TypeOf(workflow.PhaseAgentConfig{}),
			reflect.TypeOf(CreatePhaseHTTPRequest{}),
			reflect.TypeOf(UpdatePhaseHTTPRequest{}),
			reflect.TypeOf(ReorderPhasesHTTPRequest{}),
			reflect.TypeOf(RejectPhaseHTTPRequest{}),
			reflect.TypeOf(ApprovePhaseHTTPRequest{}),
			reflect.TypeOf(PhaseStats{}),
		},
	}
}
