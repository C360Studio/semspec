package projectapi

import (
	"reflect"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/service"
)

func init() {
	service.RegisterOpenAPISpec("project-api", projectAPIOpenAPISpec())
}

// OpenAPISpec implements the OpenAPIProvider interface.
func (c *Component) OpenAPISpec() *service.OpenAPISpec {
	return projectAPIOpenAPISpec()
}

// projectAPIOpenAPISpec returns the OpenAPI specification for project-api endpoints.
func projectAPIOpenAPISpec() *service.OpenAPISpec {
	return &service.OpenAPISpec{
		Tags: []service.TagSpec{
			{Name: "Project", Description: "Project initialization and setup wizard endpoints"},
		},
		Paths: map[string]service.PathSpec{
			"/api/project/status": {
				GET: &service.OperationSpec{
					Summary:     "Get project status",
					Description: "Returns the current project initialization status, including which config files exist and their approval state",
					Tags:        []string{"Project"},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Current project initialization status",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/InitStatus",
						},
					},
				},
			},
			"/api/project/wizard": {
				GET: &service.OperationSpec{
					Summary:     "Get wizard options",
					Description: "Returns the supported languages and frameworks available in the setup wizard",
					Tags:        []string{"Project"},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Supported languages and frameworks for the wizard",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/WizardResponse",
						},
					},
				},
			},
			"/api/project/detect": {
				POST: &service.OperationSpec{
					Summary:     "Detect project stack",
					Description: "Runs filesystem-based stack detection and returns the detected languages, frameworks, tools, and documentation files",
					Tags:        []string{"Project"},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Detected project stack",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/DetectionResult",
						},
						"500": {Description: "Detection failed"},
					},
				},
			},
			"/api/project/scaffold": {
				POST: &service.OperationSpec{
					Summary:     "Scaffold project files",
					Description: "Creates marker files for the selected languages and frameworks",
					Tags:        []string{"Project"},
					RequestBody: &service.RequestBodySpec{
						Description: "Selected languages and frameworks to scaffold",
						Required:    true,
						ContentType: "application/json",
						SchemaRef:   "#/components/schemas/ScaffoldRequest",
					},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "List of files created during scaffolding",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/ScaffoldResponse",
						},
						"400": {Description: "Missing languages in request"},
					},
				},
			},
			"/api/project/generate-standards": {
				POST: &service.OperationSpec{
					Summary:     "Generate project standards",
					Description: "Generates a set of project standards rules based on the detected stack and existing documentation",
					Tags:        []string{"Project"},
					RequestBody: &service.RequestBodySpec{
						Description: "Detection result and existing documentation content",
						Required:    false,
						ContentType: "application/json",
						SchemaRef:   "#/components/schemas/GenerateStandardsRequest",
					},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Generated standards rules with token estimate",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/GenerateStandardsResponse",
						},
					},
				},
			},
			"/api/project/init": {
				POST: &service.OperationSpec{
					Summary:     "Initialize project",
					Description: "Writes confirmed project metadata, checklist, and standards to disk under .semspec/",
					Tags:        []string{"Project"},
					RequestBody: &service.RequestBodySpec{
						Description: "Confirmed project metadata, checklist, and standards",
						Required:    true,
						ContentType: "application/json",
						SchemaRef:   "#/components/schemas/InitRequest",
					},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "List of files written during initialization",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/InitResponse",
						},
						"400": {Description: "Invalid request (missing required fields)"},
						"500": {Description: "Failed to write configuration files"},
					},
				},
			},
			"/api/project/approve": {
				POST: &service.OperationSpec{
					Summary:     "Approve configuration file",
					Description: "Sets the approved_at timestamp on the specified configuration file",
					Tags:        []string{"Project"},
					RequestBody: &service.RequestBodySpec{
						Description: "Name of the configuration file to approve",
						Required:    true,
						ContentType: "application/json",
						SchemaRef:   "#/components/schemas/ApproveRequest",
					},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Approval result with timestamp and overall approval state",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/ApproveResponse",
						},
						"400": {Description: "Invalid file name"},
						"404": {Description: "Configuration file not found"},
					},
				},
			},
		},
		ResponseTypes: []reflect.Type{
			reflect.TypeOf(workflow.InitStatus{}),
			reflect.TypeOf(WizardResponse{}),
			reflect.TypeOf(WizardLanguage{}),
			reflect.TypeOf(WizardFramework{}),
			reflect.TypeOf(workflow.DetectionResult{}),
			reflect.TypeOf(workflow.DetectedLanguage{}),
			reflect.TypeOf(workflow.DetectedFramework{}),
			reflect.TypeOf(workflow.DetectedTool{}),
			reflect.TypeOf(workflow.DetectedDoc{}),
			reflect.TypeOf(workflow.Check{}),
			reflect.TypeOf(ScaffoldResponse{}),
			reflect.TypeOf(GenerateStandardsResponse{}),
			reflect.TypeOf(workflow.Rule{}),
			reflect.TypeOf(InitResponse{}),
			reflect.TypeOf(ApproveResponse{}),
		},
		RequestBodyTypes: []reflect.Type{
			reflect.TypeOf(ScaffoldRequest{}),
			reflect.TypeOf(GenerateStandardsRequest{}),
			reflect.TypeOf(InitRequest{}),
			reflect.TypeOf(ProjectInitInput{}),
			reflect.TypeOf(StandardsInput{}),
			reflect.TypeOf(ApproveRequest{}),
		},
	}
}
