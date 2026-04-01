package executionmanager

import (
	"reflect"

	"github.com/c360studio/semstreams/service"

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
	return &service.OpenAPISpec{
		Tags: []service.TagSpec{
			{Name: "Lessons", Description: "Role-scoped lessons learned and error pattern tracking"},
		},
		Paths: map[string]service.PathSpec{
			"/execution-manager/lessons": {
				GET: &service.OperationSpec{
					Summary:     "List lessons",
					Description: "Returns recent lessons, optionally filtered by ?role=",
					Tags:        []string{"Lessons"},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Array of lessons",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/Lesson",
							IsArray:     true,
						},
						"503": {Description: "Lesson store not available"},
					},
				},
			},
			"/execution-manager/lessons/counts": {
				GET: &service.OperationSpec{
					Summary:     "Lesson counts",
					Description: "Returns per-category error counts for a role (?role=, defaults to developer)",
					Tags:        []string{"Lessons"},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Per-category error counts",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/RoleLessonCounts",
						},
					},
				},
			},
		},
		ResponseTypes: []reflect.Type{
			reflect.TypeOf(workflow.Lesson{}),
			reflect.TypeOf(workflow.RoleLessonCounts{}),
		},
	}
}
