package executionmanager

import (
	"reflect"

	"github.com/c360studio/semstreams/service"

	"github.com/c360studio/semspec/agentgraph"
	"github.com/c360studio/semspec/workflow"
)

func init() {
	service.RegisterOpenAPISpec("execution-manager", executionManagerOpenAPISpec())
}

// OpenAPISpec implements the OpenAPIProvider interface.
func (c *Component) OpenAPISpec() *service.OpenAPISpec {
	return executionManagerOpenAPISpec()
}

// executionManagerOpenAPISpec returns the OpenAPI specification for execution-manager endpoints.
func executionManagerOpenAPISpec() *service.OpenAPISpec {
	agentIDParam := service.ParameterSpec{
		Name:        "id",
		In:          "path",
		Required:    true,
		Description: "Agent identifier",
		Schema:      service.Schema{Type: "string"},
	}

	return &service.OpenAPISpec{
		Tags: []service.TagSpec{
			{Name: "Agent Roster", Description: "Agent team roster, review history, and knowledge infrastructure"},
		},
		Paths: map[string]service.PathSpec{
			"/execution-manager/agents/": {
				GET: &service.OperationSpec{
					Summary:     "List agents",
					Description: "Returns all agents in the roster with error counts, review stats, and persona display names",
					Tags:        []string{"Agent Roster"},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Array of agents",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/AgentResponse",
							IsArray:     true,
						},
						"503": {Description: "Agent roster not available"},
					},
				},
			},
			"/execution-manager/agents/{id}/reviews": {
				GET: &service.OperationSpec{
					Summary:     "List agent reviews",
					Description: "Returns all peer reviews for a specific agent",
					Tags:        []string{"Agent Roster"},
					Parameters:  []service.ParameterSpec{agentIDParam},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Array of reviews for the agent",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/Review",
							IsArray:     true,
						},
						"500": {Description: "Internal server error"},
						"503": {Description: "Agent roster not available"},
					},
				},
			},
			"/execution-manager/teams": {
				GET: &service.OperationSpec{
					Summary:     "List teams",
					Description: "Returns all teams with stats, member IDs, and insight counts",
					Tags:        []string{"Agent Roster"},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Array of teams",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/TeamResponse",
							IsArray:     true,
						},
						"503": {Description: "Agent roster not available"},
					},
				},
			},
		},
		ResponseTypes: []reflect.Type{
			reflect.TypeOf(AgentResponse{}),
			reflect.TypeOf(TeamResponse{}),
			reflect.TypeOf(agentgraph.Review{}),
			reflect.TypeOf(agentgraph.ReviewErrorRef{}),
			reflect.TypeOf(workflow.ReviewStats{}),
		},
	}
}
