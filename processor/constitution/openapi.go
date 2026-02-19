package constitution

import (
	"reflect"

	"github.com/c360studio/semstreams/service"
)

func init() {
	service.RegisterOpenAPISpec("constitution", constitutionOpenAPISpec())
}

// OpenAPISpec implements the OpenAPIProvider interface.
func (c *Component) OpenAPISpec() *service.OpenAPISpec {
	return constitutionOpenAPISpec()
}

// constitutionOpenAPISpec returns the OpenAPI specification for constitution endpoints.
func constitutionOpenAPISpec() *service.OpenAPISpec {
	return &service.OpenAPISpec{
		Tags: []service.TagSpec{
			{Name: "Constitution", Description: "Project constitution management - rules, constraints, and compliance checking"},
		},
		Paths: map[string]service.PathSpec{
			"/api/constitution/": {
				GET: &service.OperationSpec{
					Summary:     "Get constitution",
					Description: "Returns the current project constitution with all sections and rules",
					Tags:        []string{"Constitution"},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Constitution with all sections and rules",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/Response",
						},
					},
				},
			},
			"/api/constitution/rules": {
				GET: &service.OperationSpec{
					Summary:     "Get all rules",
					Description: "Returns all rules across all sections with their section information",
					Tags:        []string{"Constitution"},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "List of all rules with section metadata",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/RulesResponse",
						},
					},
				},
			},
			"/api/constitution/rules/{section}": {
				GET: &service.OperationSpec{
					Summary:     "Get rules by section",
					Description: "Returns rules for a specific section (code_quality, testing, security, architecture)",
					Tags:        []string{"Constitution"},
					Parameters: []service.ParameterSpec{
						{
							Name:        "section",
							In:          "path",
							Required:    true,
							Description: "Section name: code_quality, testing, security, or architecture",
							Schema:      service.Schema{Type: "string"},
						},
					},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Rules for the specified section",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/SectionRulesResponse",
						},
						"400": {Description: "Invalid section name"},
					},
				},
			},
			"/api/constitution/check": {
				POST: &service.OperationSpec{
					Summary:     "Check content",
					Description: "Check content against all constitution rules and return violations and warnings",
					Tags:        []string{"Constitution"},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Check result with pass/fail status, violations, and warnings",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/HTTPCheckResponse",
						},
						"400": {Description: "Invalid request (missing content field)"},
					},
				},
			},
			"/api/constitution/reload": {
				POST: &service.OperationSpec{
					Summary:     "Reload constitution",
					Description: "Reload the constitution from the configured file path",
					Tags:        []string{"Constitution"},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Reload result with success status and rule count",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/ReloadResponse",
						},
						"500": {Description: "Failed to reload constitution"},
					},
				},
			},
		},
		ResponseTypes: []reflect.Type{
			reflect.TypeOf(Response{}),
			reflect.TypeOf(RulesResponse{}),
			reflect.TypeOf(SectionRulesResponse{}),
			reflect.TypeOf(HTTPCheckRequest{}),
			reflect.TypeOf(HTTPCheckResponse{}),
			reflect.TypeOf(ReloadResponse{}),
			reflect.TypeOf(Rule{}),
			reflect.TypeOf(Violation{}),
			reflect.TypeOf(RuleWithSection{}),
		},
	}
}
