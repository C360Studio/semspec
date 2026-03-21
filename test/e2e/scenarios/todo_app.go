package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/c360studio/semspec/processor/context-builder/gatherers"
	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
	"github.com/c360studio/semspec/workflow"
)

// TodoAppScenario tests the brownfield experience:
// setup Go+Svelte todo app → ingest SOP → create plan for due dates →
// verify plan references existing code → approve → verify requirements/scenarios →
// exercise requirement/scenario CRUD → capture trajectory.
type TodoAppScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	fs          *client.FilesystemClient
	nats        *client.NATSClient
}

// NewTodoAppScenario creates a brownfield todo-app scenario.
func NewTodoAppScenario(cfg *config.Config) *TodoAppScenario {
	return &TodoAppScenario{
		name:        "todo-app",
		description: "Brownfield Go+Svelte: add due dates with semantic validation",
		config:      cfg,
	}
}

// timeout returns fast if FastTimeouts is enabled, otherwise normal.
func (s *TodoAppScenario) timeout(normalSec, fastSec int) time.Duration {
	if s.config.FastTimeouts {
		return time.Duration(fastSec) * time.Second
	}
	return time.Duration(normalSec) * time.Second
}

// Name returns the scenario name.
func (s *TodoAppScenario) Name() string { return s.name }

// Description returns the scenario description.
func (s *TodoAppScenario) Description() string { return s.description }

// Setup prepares the scenario environment.
func (s *TodoAppScenario) Setup(ctx context.Context) error {
	s.fs = client.NewFilesystemClient(s.config.WorkspacePath)
	if err := s.fs.SetupWorkspace(); err != nil {
		return fmt.Errorf("setup workspace: %w", err)
	}

	s.http = client.NewHTTPClient(s.config.HTTPBaseURL)

	if err := s.http.WaitForHealthy(ctx); err != nil {
		return fmt.Errorf("service not healthy: %w", err)
	}

	natsClient, err := client.NewNATSClient(ctx, s.config.NATSURL)
	if err != nil {
		return fmt.Errorf("create NATS client: %w", err)
	}
	s.nats = natsClient

	return nil
}

// Execute runs the todo-app scenario.
func (s *TodoAppScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	t := s.timeout // shorthand

	stages := s.buildStages(t)

	for _, stage := range stages {
		stageStart := time.Now()
		stageCtx, cancel := context.WithTimeout(ctx, stage.timeout)
		err := stage.fn(stageCtx, result)
		cancel()
		stageDuration := time.Since(stageStart)
		result.SetMetric(fmt.Sprintf("%s_duration_us", stage.name), stageDuration.Microseconds())
		if err != nil {
			result.AddStage(stage.name, false, stageDuration, err.Error())
			result.AddError(fmt.Sprintf("%s: %v", stage.name, err))
			result.Error = fmt.Sprintf("%s failed: %v", stage.name, err)
			return result, nil
		}
		result.AddStage(stage.name, true, stageDuration, "")
	}

	result.Success = true
	return result, nil
}

// Teardown cleans up after the scenario.
func (s *TodoAppScenario) Teardown(ctx context.Context) error {
	if s.nats != nil {
		return s.nats.Close(ctx)
	}
	return nil
}

// stageSetupProject creates a Go+Svelte todo app in the workspace (~200 lines).
func (s *TodoAppScenario) stageSetupProject(_ context.Context, result *Result) error {
	ws := s.config.WorkspacePath

	// --- Go API ---

	mainGo := `package main

import (
	"log"
	"net/http"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /todos", ListTodos)
	mux.HandleFunc("POST /todos", CreateTodo)
	mux.HandleFunc("PUT /todos/{id}", UpdateTodo)
	mux.HandleFunc("DELETE /todos/{id}", DeleteTodo)

	log.Println("Server starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
`
	if err := s.fs.WriteFile(filepath.Join(ws, "api", "main.go"), mainGo); err != nil {
		return fmt.Errorf("write api/main.go: %w", err)
	}

	handlersGo := `package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

var (
	todos   = make(map[string]*Todo)
	todosMu sync.RWMutex
	nextID  = 1
)

// ListTodos returns all todos as JSON.
func ListTodos(w http.ResponseWriter, r *http.Request) {
	todosMu.RLock()
	defer todosMu.RUnlock()
	list := make([]*Todo, 0, len(todos))
	for _, t := range todos {
		list = append(list, t)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// CreateTodo creates a new todo from the request body.
func CreateTodo(w http.ResponseWriter, r *http.Request) {
	var t Todo
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	todosMu.Lock()
	t.ID = fmt.Sprintf("%d", nextID)
	nextID++
	todos[t.ID] = &t
	todosMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(t)
}

// UpdateTodo updates an existing todo by ID.
func UpdateTodo(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	todosMu.Lock()
	defer todosMu.Unlock()

	existing, ok := todos[id]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	var updates Todo
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if updates.Title != "" {
		existing.Title = updates.Title
	}
	if updates.Description != "" {
		existing.Description = updates.Description
	}
	existing.Completed = updates.Completed

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(existing)
}

// DeleteTodo removes a todo by ID.
func DeleteTodo(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	todosMu.Lock()
	defer todosMu.Unlock()

	if _, ok := todos[id]; !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	delete(todos, id)
	w.WriteHeader(http.StatusNoContent)
}
`
	if err := s.fs.WriteFile(filepath.Join(ws, "api", "handlers.go"), handlersGo); err != nil {
		return fmt.Errorf("write api/handlers.go: %w", err)
	}

	modelsGo := `package main

// Todo represents a todo item.
type Todo struct {
	ID          string ` + "`" + `json:"id"` + "`" + `
	Title       string ` + "`" + `json:"title"` + "`" + `
	Description string ` + "`" + `json:"description,omitempty"` + "`" + `
	Completed   bool   ` + "`" + `json:"completed"` + "`" + `
}
`
	if err := s.fs.WriteFile(filepath.Join(ws, "api", "models.go"), modelsGo); err != nil {
		return fmt.Errorf("write api/models.go: %w", err)
	}

	goMod := `module todo-app

go 1.22
`
	if err := s.fs.WriteFile(filepath.Join(ws, "api", "go.mod"), goMod); err != nil {
		return fmt.Errorf("write api/go.mod: %w", err)
	}

	// --- Svelte/TypeScript UI ---

	pageSvelte := `<script>
	import { onMount } from 'svelte';
	import { listTodos, createTodo, updateTodo, deleteTodo } from '$lib/api';

	let todos = $state([]);
	let newTitle = $state('');

	onMount(async () => {
		todos = await listTodos();
	});

	async function addTodo() {
		if (!newTitle.trim()) return;
		const todo = await createTodo({ title: newTitle });
		todos = [...todos, todo];
		newTitle = '';
	}

	async function toggleComplete(todo) {
		const updated = await updateTodo(todo.id, { completed: !todo.completed });
		todos = todos.map(t => t.id === updated.id ? updated : t);
	}

	async function removeTodo(id) {
		await deleteTodo(id);
		todos = todos.filter(t => t.id !== id);
	}
</script>

<h1>Todo App</h1>

<form onsubmit|preventDefault={addTodo}>
	<input bind:value={newTitle} placeholder="New todo..." />
	<button type="submit">Add</button>
</form>

{#each todos as todo}
	<div class="todo" class:completed={todo.completed}>
		<input type="checkbox" checked={todo.completed} onchange={() => toggleComplete(todo)} />
		<span>{todo.title}</span>
		<button onclick={() => removeTodo(todo.id)}>Delete</button>
	</div>
{/each}
`
	if err := s.fs.WriteFile(filepath.Join(ws, "ui", "src", "routes", "+page.svelte"), pageSvelte); err != nil {
		return fmt.Errorf("write +page.svelte: %w", err)
	}

	apiTS := `const BASE_URL = '/api';

export interface Todo {
	id: string;
	title: string;
	description?: string;
	completed: boolean;
}

export async function listTodos(): Promise<Todo[]> {
	const res = await fetch(` + "`" + `${BASE_URL}/todos` + "`" + `);
	return res.json();
}

export async function createTodo(data: Partial<Todo>): Promise<Todo> {
	const res = await fetch(` + "`" + `${BASE_URL}/todos` + "`" + `, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(data),
	});
	return res.json();
}

export async function updateTodo(id: string, data: Partial<Todo>): Promise<Todo> {
	const res = await fetch(` + "`" + `${BASE_URL}/todos/${id}` + "`" + `, {
		method: 'PUT',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(data),
	});
	return res.json();
}

export async function deleteTodo(id: string): Promise<void> {
	await fetch(` + "`" + `${BASE_URL}/todos/${id}` + "`" + `, { method: 'DELETE' });
}
`
	if err := s.fs.WriteFile(filepath.Join(ws, "ui", "src", "lib", "api.ts"), apiTS); err != nil {
		return fmt.Errorf("write api.ts: %w", err)
	}

	typesTS := `export interface Todo {
	id: string;
	title: string;
	description?: string;
	completed: boolean;
}
`
	if err := s.fs.WriteFile(filepath.Join(ws, "ui", "src", "lib", "types.ts"), typesTS); err != nil {
		return fmt.Errorf("write types.ts: %w", err)
	}

	packageJSON := `{
	"name": "todo-ui",
	"private": true,
	"type": "module",
	"dependencies": {
		"@sveltejs/kit": "^2.0.0",
		"svelte": "^5.0.0"
	}
}
`
	if err := s.fs.WriteFile(filepath.Join(ws, "ui", "package.json"), packageJSON); err != nil {
		return fmt.Errorf("write package.json: %w", err)
	}

	svelteConfig := `import adapter from '@sveltejs/adapter-auto';

/** @type {import('@sveltejs/kit').Config} */
export default {
	kit: {
		adapter: adapter()
	}
};
`
	if err := s.fs.WriteFile(filepath.Join(ws, "ui", "svelte.config.js"), svelteConfig); err != nil {
		return fmt.Errorf("write svelte.config.js: %w", err)
	}

	readme := `# Todo App

A Go backend + SvelteKit frontend todo application.

## API Endpoints

- GET /todos - List all todos
- POST /todos - Create a todo
- PUT /todos/{id} - Update a todo
- DELETE /todos/{id} - Delete a todo

## Running

` + "```" + `bash
# Backend
cd api && go run .

# Frontend
cd ui && npm install && npm run dev
` + "```" + `
`
	if err := s.fs.WriteFile(filepath.Join(ws, "README.md"), readme); err != nil {
		return fmt.Errorf("write README.md: %w", err)
	}

	if err := s.fs.InitGit(); err != nil {
		return fmt.Errorf("init git: %w", err)
	}
	if err := s.fs.GitAdd("."); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	if err := s.fs.GitCommit("Initial commit"); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	result.SetDetail("project_ready", true)
	return nil
}

// stageCheckNotInitialized verifies the project is NOT initialized before setup wizard.
func (s *TodoAppScenario) stageCheckNotInitialized(ctx context.Context, result *Result) error {
	status, err := s.http.GetProjectStatus(ctx)
	if err != nil {
		return fmt.Errorf("get project status: %w", err)
	}

	if status.Initialized {
		return fmt.Errorf("expected project NOT to be initialized, but it is")
	}

	result.SetDetail("pre_init_initialized", status.Initialized)
	result.SetDetail("pre_init_has_project_json", status.HasProjectJSON)
	result.SetDetail("pre_init_has_checklist", status.HasChecklist)
	result.SetDetail("pre_init_has_standards", status.HasStandards)
	return nil
}

// stageDetectStack runs filesystem-based stack detection on the workspace.
// For todo-app, we expect Go (from api/go.mod) and JavaScript (from ui/package.json).
func (s *TodoAppScenario) stageDetectStack(ctx context.Context, result *Result) error {
	detection, err := s.http.DetectProject(ctx)
	if err != nil {
		return fmt.Errorf("detect project: %w", err)
	}

	// The workspace has api/go.mod and ui/package.json with SvelteKit — subdirectory
	// detection should find Go from api/go.mod and TypeScript from ui/ at minimum.
	if len(detection.Languages) == 0 {
		return fmt.Errorf("no languages detected (expected Go from api/go.mod and TypeScript from ui/ via subdirectory scanning)")
	}

	// Record what was detected
	var langNames []string
	for _, lang := range detection.Languages {
		langNames = append(langNames, lang.Name)
	}
	result.SetDetail("detected_languages", langNames)
	result.SetDetail("detected_frameworks_count", len(detection.Frameworks))
	result.SetDetail("detected_tooling_count", len(detection.Tooling))
	result.SetDetail("detected_docs_count", len(detection.ExistingDocs))
	result.SetDetail("proposed_checks_count", len(detection.ProposedChecklist))

	// Store detection for use in init stage
	result.SetDetail("detection_result", detection)
	return nil
}

// stageInitProject initializes the project using detection results.
func (s *TodoAppScenario) stageInitProject(ctx context.Context, result *Result) error {
	detectionRaw, ok := result.GetDetail("detection_result")
	if !ok {
		return fmt.Errorf("detection_result not found in result details")
	}
	detection := detectionRaw.(*client.ProjectDetectionResult)

	// Build language list from detection
	var languages []string
	for _, lang := range detection.Languages {
		languages = append(languages, lang.Name)
	}
	var frameworks []string
	for _, fw := range detection.Frameworks {
		frameworks = append(frameworks, fw.Name)
	}

	initReq := &client.ProjectInitRequest{
		Project: client.ProjectInitInput{
			Name:        "Todo App",
			Description: "A Go backend + SvelteKit frontend todo application",
			Languages:   languages,
			Frameworks:  frameworks,
		},
		Checklist: detection.ProposedChecklist,
		Standards: client.StandardsInput{
			Version: "1.0.0",
			Rules:   []any{},
		},
	}

	resp, err := s.http.InitProject(ctx, initReq)
	if err != nil {
		return fmt.Errorf("init project: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("init project returned success=false")
	}

	result.SetDetail("init_success", resp.Success)
	result.SetDetail("init_files_written", resp.FilesWritten)
	return nil
}

// stageVerifyInitialized confirms the project is now fully initialized.
func (s *TodoAppScenario) stageVerifyInitialized(ctx context.Context, result *Result) error {
	status, err := s.http.GetProjectStatus(ctx)
	if err != nil {
		return fmt.Errorf("get project status: %w", err)
	}

	if !status.Initialized {
		missing := []string{}
		if !status.HasProjectJSON {
			missing = append(missing, "project.json")
		}
		if !status.HasChecklist {
			missing = append(missing, "checklist.json")
		}
		if !status.HasStandards {
			missing = append(missing, "standards.json")
		}
		return fmt.Errorf("project not fully initialized — missing: %s", strings.Join(missing, ", "))
	}

	result.SetDetail("post_init_initialized", status.Initialized)
	result.SetDetail("post_init_has_project_json", status.HasProjectJSON)
	result.SetDetail("post_init_has_checklist", status.HasChecklist)
	result.SetDetail("post_init_has_standards", status.HasStandards)

	// Verify the files exist on disk
	ws := s.config.WorkspacePath
	projectJSON := filepath.Join(ws, ".semspec", "project.json")
	if _, err := os.Stat(projectJSON); os.IsNotExist(err) {
		return fmt.Errorf(".semspec/project.json not found on disk")
	}

	checklistJSON := filepath.Join(ws, ".semspec", "checklist.json")
	if _, err := os.Stat(checklistJSON); os.IsNotExist(err) {
		return fmt.Errorf(".semspec/checklist.json not found on disk")
	}

	standardsJSON := filepath.Join(ws, ".semspec", "standards.json")
	if _, err := os.Stat(standardsJSON); os.IsNotExist(err) {
		return fmt.Errorf(".semspec/standards.json not found on disk")
	}

	result.SetDetail("project_files_on_disk", true)
	return nil
}

// stageIngestSOP writes a model-change SOP and publishes an ingestion request.
func (s *TodoAppScenario) stageIngestSOP(ctx context.Context, result *Result) error {
	sopContent := `---
category: sop
scope: all
severity: error
applies_to:
  - "api/**/*.go"
  - "ui/src/**"
domain:
  - data-modeling
  - code-patterns
requirements:
  - "All model changes require a migration plan or migration notes"
  - "Follow existing code patterns and conventions"
  - "New fields must be added to both API types and UI types"
---

# Model Change SOP

## Ground Truth

- Backend models are defined in api/models.go (Go structs with json tags)
- Frontend types are defined in ui/src/lib/types.ts (TypeScript interfaces)
- API handlers are in api/handlers.go (net/http handler functions)
- Frontend API client is in ui/src/lib/api.ts (fetch-based async functions)
- The Todo struct and Todo interface must stay synchronized

## Rules

1. When modifying data models, include a migration task documenting schema changes.
2. Follow existing code patterns — use the same naming conventions, file structure, and error handling.
3. Any new field added to the Go struct in api/models.go must also be added to the TypeScript interface in ui/src/lib/types.ts.
4. Backend tasks must be sequenced before frontend tasks (api/ changes before ui/ changes).
5. Plan scope must reference actual project files, not invented paths.

## Violations

- Adding a field to the Go model without a corresponding change to the TypeScript type
- Generating tasks that modify ui/ before api/ is updated
- Referencing files that don't exist (e.g., src/models/todo.go when the project uses api/models.go)
- Omitting migration notes when changing the data shape
`

	// Write to sources/ for semsource to discover in real deployments.
	if err := s.fs.WriteFileRelative("sources/model-change-sop.md", sopContent); err != nil {
		return fmt.Errorf("write SOP file: %w", err)
	}

	result.SetDetail("sop_file_written", true)
	return nil
}

// stageVerifySOPIngested confirms the SOP document was written to disk.
// Graph ingestion is handled by semsource in real deployments.
func (s *TodoAppScenario) stageVerifySOPIngested(ctx context.Context, result *Result) error {
	sopPath := filepath.Join(s.config.WorkspacePath, "sources", "model-change-sop.md")

	data, err := os.ReadFile(sopPath)
	if err != nil {
		return fmt.Errorf("SOP file not found at %s: %w", sopPath, err)
	}

	content := string(data)
	if !strings.Contains(content, "category: sop") {
		return fmt.Errorf("SOP file missing expected frontmatter (category: sop)")
	}

	result.SetDetail("sop_file_verified", true)
	result.SetDetail("sop_file_size", len(data))
	return nil
}

// stageVerifyStandardsPopulated reads standards.json and confirms it exists with
// valid structure. Rules may be empty — semsource handles graph ingestion in production.
func (s *TodoAppScenario) stageVerifyStandardsPopulated(ctx context.Context, result *Result) error {
	standardsPath := filepath.Join(s.config.WorkspacePath, ".semspec", "standards.json")

	data, err := os.ReadFile(standardsPath)
	if err != nil {
		return fmt.Errorf("standards.json not found: %w", err)
	}

	var standards workflow.Standards
	if err := json.Unmarshal(data, &standards); err != nil {
		return fmt.Errorf("standards.json invalid JSON: %w", err)
	}

	if standards.Version == "" {
		return fmt.Errorf("standards.json missing version field")
	}

	result.SetDetail("standards_rules_count", len(standards.Rules))
	result.SetDetail("standards_version", standards.Version)
	return nil
}

// stageVerifyGraphReady polls the graph gateway until it responds, confirming the
// graph pipeline is ready. This prevents plan creation before graph entities are queryable.
func (s *TodoAppScenario) stageVerifyGraphReady(ctx context.Context, result *Result) error {
	gatherer := gatherers.NewGraphGatherer(s.config.GraphURL)

	if err := gatherer.WaitForReady(ctx, 30*time.Second); err != nil {
		return fmt.Errorf("graph not ready: %w", err)
	}

	result.SetDetail("graph_ready", true)
	return nil
}

// stageCreatePlan creates a plan for adding due dates via the REST API.
func (s *TodoAppScenario) stageCreatePlan(ctx context.Context, result *Result) error {
	resp, err := s.http.CreatePlan(ctx, "add due dates to todos — backend field, API update, UI date picker")
	if err != nil {
		return fmt.Errorf("create plan: %w", err)
	}

	if resp.Error != "" {
		return fmt.Errorf("plan creation returned error: %s", resp.Error)
	}

	if resp.Slug == "" {
		return fmt.Errorf("plan creation returned empty slug")
	}

	result.SetDetail("plan_slug", resp.Slug)
	result.SetDetail("plan_response", resp)
	if resp.TraceID != "" {
		result.SetDetail("plan_trace_id", resp.TraceID)
	}
	return nil
}

// stageWaitForPlan waits for the plan to be created via the HTTP API with a
// non-empty Goal field, indicating the planner LLM has finished generating.
func (s *TodoAppScenario) stageWaitForPlan(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	plan, err := s.http.WaitForPlanGoal(ctx, slug)
	if err != nil {
		return fmt.Errorf("plan never received goal from LLM: %w", err)
	}

	result.SetDetail("plan_file_exists", true)
	result.SetDetail("plan_data", plan)
	return nil
}

// stageVerifyPlanSemantics validates that the plan references existing code
// and understands the brownfield context.
func (s *TodoAppScenario) stageVerifyPlanSemantics(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	// Retrieve plan stored by stageWaitForPlan, falling back to API if not present.
	var planTyped *client.Plan
	if raw, ok := result.GetDetail("plan_data"); ok {
		planTyped, _ = raw.(*client.Plan)
	}
	if planTyped == nil {
		var err error
		planTyped, err = s.http.GetPlan(ctx, slug)
		if err != nil {
			return fmt.Errorf("get plan: %w", err)
		}
	}

	// Convert to map[string]any for helpers that require it.
	planJSONBytes, _ := json.Marshal(planTyped)
	var plan map[string]any
	_ = json.Unmarshal(planJSONBytes, &plan)

	goal := planTyped.Goal
	planStr := string(planJSONBytes)

	report := &SemanticReport{}

	// Goal mentions due dates
	report.Add("goal-mentions-due-dates",
		containsAnyCI(goal, "due date", "due_date", "deadline", "duedate"),
		fmt.Sprintf("goal: %s", truncate(goal, 100)))

	// Plan references existing files (warning — reviewer enforces scope)
	refsExisting := containsAnyCI(planStr, "handlers.go", "models.go", "+page.svelte", "api.ts", "types.ts")
	if !refsExisting {
		result.AddWarning("plan does not reference existing codebase files")
	}
	result.SetDetail("references_existing_files", refsExisting)

	// Plan references both api/ and ui/ directories (checks goal, context, and scope)
	report.Add("plan-references-api",
		planReferencesDir(plan, "api"),
		"plan should reference api/ directory in goal, context, or scope")
	report.Add("plan-references-ui",
		planReferencesDir(plan, "ui"),
		"plan should reference ui/ directory in goal, context, or scope")

	// Plan context mentions existing patterns or structure
	report.Add("context-mentions-existing-code",
		containsAnyCI(planStr, "todo", "existing", "current", "svelte", "handlers"),
		"plan context should reference the existing codebase")

	// Scope hallucination detection: record rate as metric, reviewer enforces correctness.
	knownFiles := []string{
		"api/main.go", "api/handlers.go", "api/models.go", "api/go.mod",
		"ui/src/routes/+page.svelte", "ui/src/lib/api.ts", "ui/src/lib/types.ts",
		"ui/package.json", "ui/svelte.config.js",
		"README.md",
	}
	if scope, ok := plan["scope"].(map[string]any); ok {
		hallucinationRate := scopeHallucinationRate(scope, knownFiles)
		result.SetDetail("scope_hallucination_rate", hallucinationRate)
		if hallucinationRate > 0.5 {
			result.AddWarning(fmt.Sprintf("%.0f%% of scope paths are hallucinated — reviewer should catch this", hallucinationRate*100))
		}
	}

	// SOP awareness (best-effort — warn if missing)
	sopAware := containsAnyCI(planStr, "sop", "migration", "model change", "source.doc")
	if !sopAware {
		result.AddWarning("plan does not appear to reference SOPs — context-builder may not have included them")
	}
	result.SetDetail("plan_references_sops", sopAware)

	// Record all checks
	result.SetDetail("plan_goal", goal)
	for _, check := range report.Checks {
		result.SetDetail("semantic_"+check.Name, check.Passed)
	}
	result.SetDetail("semantic_pass_rate", report.PassRate())

	if report.HasFailures() {
		return fmt.Errorf("plan semantic validation failed (%.0f%% pass rate): %s",
			report.PassRate()*100, report.Error())
	}
	return nil
}

// stageApprovePlan waits for the plan-review-loop workflow to approve the plan.
// The workflow drives planner → reviewer → revise cycles via NATS (ADR-005).
// This stage polls the plan's approval status instead of triggering reviews via HTTP.
func (s *TodoAppScenario) stageApprovePlan(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	reviewTimeout := time.Duration(maxReviewAttempts) * 4 * time.Minute
	backoff := reviewRetryBackoff
	if s.config.FastTimeouts {
		reviewTimeout = time.Duration(maxReviewAttempts) * config.FastReviewStepTimeout
		backoff = config.FastReviewBackoff
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, reviewTimeout)
	defer cancel()

	ticker := time.NewTicker(backoff)
	defer ticker.Stop()

	var lastStage string
	lastIterationSeen := 0
	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("plan approval timed out (last stage: %s, iteration: %d/%d)",
				lastStage, lastIterationSeen, maxReviewAttempts)
		case <-ticker.C:
			plan, err := s.http.GetPlan(timeoutCtx, slug)
			if err != nil {
				// Plan might not be queryable yet; keep polling
				continue
			}

			lastStage = plan.Stage
			result.SetDetail("review_stage", plan.Stage)
			result.SetDetail("review_verdict", plan.ReviewVerdict)
			result.SetDetail("review_summary", plan.ReviewSummary)

			if plan.Approved {
				result.SetDetail("approve_response", plan)
				result.SetDetail("review_revisions", lastIterationSeen)
				return nil
			}

			// Track revision cycles by actual iteration number (not poll count)
			if plan.ReviewIteration > lastIterationSeen {
				lastIterationSeen = plan.ReviewIteration
				if plan.ReviewVerdict == "needs_changes" {
					result.AddWarning(fmt.Sprintf("plan review iteration %d/%d returned needs_changes: %s",
						lastIterationSeen, maxReviewAttempts, plan.ReviewSummary))
					if lastIterationSeen >= maxReviewAttempts {
						return fmt.Errorf("plan review exhausted %d revision attempts: %s",
							maxReviewAttempts, plan.ReviewSummary)
					}
				}
			}
		}
	}
}

// stageVerifyRequirements verifies that the plan has requirements after approval.
func (s *TodoAppScenario) stageVerifyRequirements(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	reqs, err := s.http.ListRequirements(ctx, slug)
	if err != nil {
		return fmt.Errorf("list requirements: %w", err)
	}

	if len(reqs) == 0 {
		return fmt.Errorf("no requirements found after plan approval")
	}

	report := &SemanticReport{}
	report.Add("minimum-requirements", len(reqs) >= 1,
		fmt.Sprintf("got %d requirements, need >= 1", len(reqs)))

	for i, req := range reqs {
		report.Add(fmt.Sprintf("req-%d-has-title", i), req.Title != "",
			fmt.Sprintf("requirement %s missing title", req.ID))
		report.Add(fmt.Sprintf("req-%d-active-status", i), req.Status == "active",
			fmt.Sprintf("requirement %s status=%s, want active", req.ID, req.Status))
	}

	result.SetDetail("requirement_count", len(reqs))
	result.SetDetail("first_requirement_id", reqs[0].ID)

	if report.HasFailures() {
		return fmt.Errorf("requirement validation failed: %s", report.Error())
	}
	return nil
}

// stageVerifyScenarios verifies that scenarios exist and have proper structure.
func (s *TodoAppScenario) stageVerifyScenarios(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	scenarios, err := s.http.ListScenarios(ctx, slug, "")
	if err != nil {
		return fmt.Errorf("list scenarios: %w", err)
	}

	if len(scenarios) == 0 {
		return fmt.Errorf("no scenarios found after plan approval")
	}

	report := &SemanticReport{}
	report.Add("minimum-scenarios", len(scenarios) >= 1,
		fmt.Sprintf("got %d scenarios, need >= 1", len(scenarios)))

	for i, sc := range scenarios {
		report.Add(fmt.Sprintf("scenario-%d-has-given", i), sc.Given != "",
			fmt.Sprintf("scenario %s missing Given", sc.ID))
		report.Add(fmt.Sprintf("scenario-%d-has-when", i), sc.When != "",
			fmt.Sprintf("scenario %s missing When", sc.ID))
		report.Add(fmt.Sprintf("scenario-%d-has-then", i), len(sc.Then) > 0,
			fmt.Sprintf("scenario %s missing Then", sc.ID))
		report.Add(fmt.Sprintf("scenario-%d-has-requirement", i), sc.RequirementID != "",
			fmt.Sprintf("scenario %s missing RequirementID", sc.ID))
	}

	result.SetDetail("scenario_count", len(scenarios))
	result.SetDetail("first_scenario_id", scenarios[0].ID)

	if report.HasFailures() {
		return fmt.Errorf("scenario validation failed: %s", report.Error())
	}
	return nil
}

// stageRequirementCRUD exercises create, get, update, deprecate, and delete on requirements.
func (s *TodoAppScenario) stageRequirementCRUD(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	// Create a new requirement
	created, err := s.http.CreateRequirement(ctx, slug, &client.CreateRequirementRequest{
		Title:       "Manual CRUD test requirement",
		Description: "Created by E2E test to verify requirement CRUD",
	})
	if err != nil {
		return fmt.Errorf("create requirement: %w", err)
	}
	if created.ID == "" {
		return fmt.Errorf("created requirement has empty ID")
	}
	if created.Title != "Manual CRUD test requirement" {
		return fmt.Errorf("created requirement title mismatch: got %q", created.Title)
	}

	// Get the requirement by ID
	got, statusCode, err := s.http.GetRequirement(ctx, slug, created.ID)
	if err != nil {
		return fmt.Errorf("get requirement: %w", err)
	}
	if statusCode != 200 {
		return fmt.Errorf("get requirement status=%d, want 200", statusCode)
	}
	if got.ID != created.ID {
		return fmt.Errorf("get requirement ID mismatch: got %q, want %q", got.ID, created.ID)
	}

	// Update the requirement
	newTitle := "Updated CRUD test requirement"
	updated, err := s.http.UpdateRequirement(ctx, slug, created.ID, &client.UpdateRequirementRequest{
		Title: &newTitle,
	})
	if err != nil {
		return fmt.Errorf("update requirement: %w", err)
	}
	if updated.Title != newTitle {
		return fmt.Errorf("updated requirement title mismatch: got %q", updated.Title)
	}

	// Deprecate the requirement
	deprecated, err := s.http.DeprecateRequirement(ctx, slug, created.ID)
	if err != nil {
		return fmt.Errorf("deprecate requirement: %w", err)
	}
	if deprecated.Status != "deprecated" {
		return fmt.Errorf("deprecated requirement status=%q, want deprecated", deprecated.Status)
	}

	// Delete the requirement
	deleteStatus, err := s.http.DeleteRequirement(ctx, slug, created.ID)
	if err != nil {
		return fmt.Errorf("delete requirement: %w", err)
	}
	if deleteStatus != 204 {
		return fmt.Errorf("delete requirement status=%d, want 204", deleteStatus)
	}

	// Verify it's gone
	_, getStatus, _ := s.http.GetRequirement(ctx, slug, created.ID)
	if getStatus != 404 {
		return fmt.Errorf("deleted requirement still accessible, status=%d", getStatus)
	}

	result.SetDetail("requirement_crud_passed", true)
	return nil
}

// stageScenarioCRUD exercises create, get, update, and delete on scenarios.
func (s *TodoAppScenario) stageScenarioCRUD(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")
	reqID, _ := result.GetDetailString("first_requirement_id")

	if reqID == "" {
		return fmt.Errorf("no requirement ID available — stageVerifyRequirements must run first")
	}

	// Create a scenario
	created, err := s.http.CreateScenario(ctx, slug, &client.CreateScenarioRequest{
		RequirementID: reqID,
		Given:         "a todo item exists without a due date",
		When:          "the user sets a due date on the item",
		Then:          []string{"the due date is persisted", "the item shows the due date in the UI"},
	})
	if err != nil {
		return fmt.Errorf("create scenario: %w", err)
	}
	if created.ID == "" {
		return fmt.Errorf("created scenario has empty ID")
	}
	if created.RequirementID != reqID {
		return fmt.Errorf("created scenario requirement_id mismatch: got %q", created.RequirementID)
	}

	// Get the scenario
	got, statusCode, err := s.http.GetScenario(ctx, slug, created.ID)
	if err != nil {
		return fmt.Errorf("get scenario: %w", err)
	}
	if statusCode != 200 {
		return fmt.Errorf("get scenario status=%d, want 200", statusCode)
	}
	if got.Given != "a todo item exists without a due date" {
		return fmt.Errorf("get scenario Given mismatch: got %q", got.Given)
	}

	// Update the scenario
	newWhen := "the user sets a due date and saves"
	updated, err := s.http.UpdateScenario(ctx, slug, created.ID, &client.UpdateScenarioRequest{
		When: &newWhen,
	})
	if err != nil {
		return fmt.Errorf("update scenario: %w", err)
	}
	if updated.When != newWhen {
		return fmt.Errorf("updated scenario When mismatch: got %q", updated.When)
	}

	// List scenarios for the requirement
	scenarios, err := s.http.ListScenarios(ctx, slug, reqID)
	if err != nil {
		return fmt.Errorf("list scenarios by requirement: %w", err)
	}
	found := false
	for _, sc := range scenarios {
		if sc.ID == created.ID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("created scenario not found in list for requirement %s", reqID)
	}

	// Delete the scenario
	deleteStatus, err := s.http.DeleteScenario(ctx, slug, created.ID)
	if err != nil {
		return fmt.Errorf("delete scenario: %w", err)
	}
	if deleteStatus != 204 {
		return fmt.Errorf("delete scenario status=%d, want 204", deleteStatus)
	}

	// Verify it's gone
	_, getStatus, _ := s.http.GetScenario(ctx, slug, created.ID)
	if getStatus != 404 {
		return fmt.Errorf("deleted scenario still accessible, status=%d", getStatus)
	}

	result.SetDetail("scenario_crud_passed", true)
	return nil
}

// stageCaptureTrajectory resolves a trace ID and retrieves trajectory data.
// Uses the plan creation trace ID first, falling back to the workflow trajectory API.
func (s *TodoAppScenario) stageCaptureTrajectory(ctx context.Context, result *Result) error {
	traceID := s.resolveTraceID(ctx, result)
	if traceID == "" {
		return nil
	}
	result.SetDetail("trajectory_trace_id", traceID)

	trajectory, statusCode, err := s.http.GetTrajectoryByTrace(ctx, traceID, true)
	if err != nil {
		if statusCode == 404 {
			result.AddWarning("trajectory-api returned 404 — component may not be enabled")
			return nil
		}
		return fmt.Errorf("get trajectory by trace: %w", err)
	}

	result.SetDetail("trajectory_model_calls", trajectory.ModelCalls)
	result.SetDetail("trajectory_tokens_in", trajectory.TokensIn)
	result.SetDetail("trajectory_tokens_out", trajectory.TokensOut)
	result.SetDetail("trajectory_duration_ms", trajectory.DurationMs)
	result.SetDetail("trajectory_entries_count", len(trajectory.Entries))
	return nil
}

// resolveTraceID gets the trace ID from plan creation or falls back to the
// workflow trajectory API endpoint.
func (s *TodoAppScenario) resolveTraceID(ctx context.Context, result *Result) string {
	traceID, _ := result.GetDetailString("plan_trace_id")
	if traceID != "" {
		return traceID
	}

	// Fallback: discover trace IDs via external workflow trajectory endpoint.
	slug, _ := result.GetDetailString("plan_slug")
	if slug == "" {
		result.AddWarning("no plan_trace_id or plan_slug available for trajectory capture")
		return ""
	}

	ticker := time.NewTicker(kvPollInterval)
	defer ticker.Stop()

	var lastErr error
	for {
		select {
		case <-ctx.Done():
			if lastErr != nil {
				result.AddWarning(fmt.Sprintf("trajectory capture timed out: %v (last error: %v)", ctx.Err(), lastErr))
			} else {
				result.AddWarning(fmt.Sprintf("trajectory capture timed out: %v", ctx.Err()))
			}
			return ""
		case <-ticker.C:
			wt, _, err := s.http.GetWorkflowTrajectory(ctx, slug)
			if err != nil {
				lastErr = err
				continue
			}
			if len(wt.TraceIDs) > 0 {
				return wt.TraceIDs[0]
			}
		}
	}
}

// buildStages returns the ordered stage list for this scenario.
func (s *TodoAppScenario) buildStages(t func(int, int) time.Duration) []stageDefinition {
	// Setup through plan approval
	setup := []stageDefinition{
		{"setup-project", s.stageSetupProject, t(30, 15)},
		{"check-not-initialized", s.stageCheckNotInitialized, t(10, 5)},
		{"detect-stack", s.stageDetectStack, t(30, 15)},
		{"init-project", s.stageInitProject, t(30, 15)},
		{"verify-initialized", s.stageVerifyInitialized, t(10, 5)},
		{"ingest-sop", s.stageIngestSOP, t(30, 15)},
		{"verify-sop-ingested", s.stageVerifySOPIngested, t(60, 15)},
		{"verify-standards-populated", s.stageVerifyStandardsPopulated, t(30, 15)},
		{"verify-graph-ready", s.stageVerifyGraphReady, t(30, 15)},
		{"create-plan", s.stageCreatePlan, t(30, 15)},
		{"wait-for-plan", s.stageWaitForPlan, t(300, 30)},
		{"verify-plan-semantics", s.stageVerifyPlanSemantics, t(10, 5)},
		{"approve-plan", s.stageApprovePlan, t(240, 30)},
	}

	// Requirement and scenario verification + CRUD
	crudStages := []stageDefinition{
		{"verify-requirements", s.stageVerifyRequirements, t(10, 5)},
		{"verify-scenarios", s.stageVerifyScenarios, t(10, 5)},
		{"requirement-crud", s.stageRequirementCRUD, t(30, 15)},
		{"scenario-crud", s.stageScenarioCRUD, t(30, 15)},
	}

	// Shared ending stages
	ending := []stageDefinition{
		{"capture-trajectory", s.stageCaptureTrajectory, t(30, 15)},
		{"generate-report", s.stageGenerateReport, t(10, 5)},
	}

	stages := make([]stageDefinition, 0, len(setup)+len(crudStages)+len(ending))
	stages = append(stages, setup...)
	stages = append(stages, crudStages...)
	stages = append(stages, ending...)
	return stages
}

// stageGenerateReport compiles a summary report with provider and trajectory data.
func (s *TodoAppScenario) stageGenerateReport(_ context.Context, result *Result) error {
	providerName := os.Getenv(config.ProviderNameEnvVar)
	if providerName == "" {
		providerName = config.DefaultProviderName
	}

	taskCount, _ := result.GetDetail("task_count")
	modelCalls, _ := result.GetDetail("trajectory_model_calls")
	tokensIn, _ := result.GetDetail("trajectory_tokens_in")
	tokensOut, _ := result.GetDetail("trajectory_tokens_out")
	durationMs, _ := result.GetDetail("trajectory_duration_ms")

	result.SetDetail("provider", providerName)
	result.SetDetail("report", map[string]any{
		"provider":      providerName,
		"scenario":      "todo-app",
		"model_calls":   modelCalls,
		"tokens_in":     tokensIn,
		"tokens_out":    tokensOut,
		"duration_ms":   durationMs,
		"plan_created":  true,
		"tasks_created": taskCount,
	})
	return nil
}
