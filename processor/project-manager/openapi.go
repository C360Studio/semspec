package projectmanager

import (
	"reflect"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/service"
)

func init() {
	service.RegisterOpenAPISpec("project-manager", projectAPIOpenAPISpec())
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
			"/api/project/config": {
				PATCH: &service.OperationSpec{
					Summary:     "Update project config",
					Description: "Updates project.json fields. Org and platform changes are only allowed before the first plan is created to prevent entity ID divergence.",
					Tags:        []string{"Project"},
					RequestBody: &service.RequestBodySpec{
						Description: "Fields to update (all optional)",
						Required:    true,
						ContentType: "application/json",
						SchemaRef:   "#/components/schemas/ConfigUpdateRequest",
					},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Updated project config",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/ProjectConfig",
						},
						"400": {Description: "Invalid request body"},
						"404": {Description: "project.json not found — run init first"},
						"409": {Description: "Cannot change org/platform after plans exist"},
					},
				},
			},
			"/api/project/checklist": {
				GET: &service.OperationSpec{
					Summary:     "Get checklist",
					Description: "Returns the current checklist.json with quality gate checks",
					Tags:        []string{"Project"},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Current checklist",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/Checklist",
						},
						"404": {Description: "checklist.json not found"},
					},
				},
				PATCH: &service.OperationSpec{
					Summary:     "Update checklist",
					Description: "Replaces the checks array in checklist.json. Preserves version, timestamps, and approval state.",
					Tags:        []string{"Project"},
					RequestBody: &service.RequestBodySpec{
						Description: "New checks array",
						Required:    true,
						ContentType: "application/json",
						SchemaRef:   "#/components/schemas/ChecklistUpdateRequest",
					},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Updated checklist",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/Checklist",
						},
						"400": {Description: "Invalid request body"},
						"404": {Description: "checklist.json not found — run init first"},
					},
				},
			},
			"/api/project/standards": {
				GET: &service.OperationSpec{
					Summary:     "Get standards",
					Description: "Returns the current standards.json with project rules",
					Tags:        []string{"Project"},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Current standards",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/Standards",
						},
						"404": {Description: "standards.json not found"},
					},
				},
				PATCH: &service.OperationSpec{
					Summary:     "Update standards",
					Description: "Replaces the rules array in standards.json. Recalculates token estimate. Preserves version, timestamps, and approval state.",
					Tags:        []string{"Project"},
					RequestBody: &service.RequestBodySpec{
						Description: "New rules array",
						Required:    true,
						ContentType: "application/json",
						SchemaRef:   "#/components/schemas/StandardsUpdateRequest",
					},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Updated standards",
							ContentType: "application/json",
							SchemaRef:   "#/components/schemas/Standards",
						},
						"400": {Description: "Invalid request body"},
						"404": {Description: "standards.json not found — run init first"},
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
			reflect.TypeOf(workflow.ProjectConfig{}),
			reflect.TypeOf(workflow.Checklist{}),
			reflect.TypeOf(workflow.Standards{}),
		},
		RequestBodyTypes: []reflect.Type{
			reflect.TypeOf(ScaffoldRequest{}),
			reflect.TypeOf(GenerateStandardsRequest{}),
			reflect.TypeOf(InitRequest{}),
			reflect.TypeOf(ProjectInitInput{}),
			reflect.TypeOf(StandardsInput{}),
			reflect.TypeOf(ApproveRequest{}),
			reflect.TypeOf(ConfigUpdateRequest{}),
			reflect.TypeOf(ChecklistUpdateRequest{}),
			reflect.TypeOf(StandardsUpdateRequest{}),
		},
	}
}
