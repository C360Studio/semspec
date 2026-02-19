package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/c360studio/semspec/source"
	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
	sourceVocab "github.com/c360studio/semspec/vocabulary/source"
)

// TodoAppScenario tests the brownfield experience:
// setup Go+Svelte todo app → ingest SOP → create plan for due dates →
// verify plan references existing code → approve → generate tasks →
// verify task ordering and SOP compliance → capture trajectory.
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

func (s *TodoAppScenario) Name() string        { return s.name }
func (s *TodoAppScenario) Description() string  { return s.description }

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

	stages := []struct {
		name    string
		fn      func(context.Context, *Result) error
		timeout time.Duration
	}{
		{"setup-project", s.stageSetupProject, 30 * time.Second},
		{"check-not-initialized", s.stageCheckNotInitialized, 10 * time.Second},
		{"detect-stack", s.stageDetectStack, 30 * time.Second},
		{"init-project", s.stageInitProject, 30 * time.Second},
		{"verify-initialized", s.stageVerifyInitialized, 10 * time.Second},
		{"ingest-sop", s.stageIngestSOP, 30 * time.Second},
		{"verify-sop-ingested", s.stageVerifySOPIngested, 60 * time.Second},
		{"create-plan", s.stageCreatePlan, 30 * time.Second},
		{"wait-for-plan", s.stageWaitForPlan, 300 * time.Second},
		{"verify-plan-semantics", s.stageVerifyPlanSemantics, 10 * time.Second},
		{"approve-plan", s.stageApprovePlan, 240 * time.Second},
		{"generate-tasks", s.stageGenerateTasks, 30 * time.Second},
		{"wait-for-tasks", s.stageWaitForTasks, 300 * time.Second},
		{"verify-tasks-semantics", s.stageVerifyTasksSemantics, 10 * time.Second},
		{"capture-trajectory", s.stageCaptureTrajectory, 30 * time.Second},
		{"generate-report", s.stageGenerateReport, 10 * time.Second},
	}

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

	if err := s.fs.WriteFileRelative(".semspec/sources/docs/model-change-sop.md", sopContent); err != nil {
		return fmt.Errorf("write SOP file: %w", err)
	}

	req := source.IngestRequest{
		Path:      "model-change-sop.md",
		ProjectID: "default",
		AddedBy:   "e2e-test",
	}
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal ingest request: %w", err)
	}

	if err := s.nats.PublishToStream(ctx, config.SourceIngestSubject, data); err != nil {
		return fmt.Errorf("publish ingest request: %w", err)
	}

	result.SetDetail("sop_file_written", true)
	result.SetDetail("sop_ingest_published", true)
	return nil
}

// stageVerifySOPIngested polls the message-logger for graph.ingest.entity entries
// containing SOP-related content.
func (s *TodoAppScenario) stageVerifySOPIngested(ctx context.Context, result *Result) error {
	ticker := time.NewTicker(kvPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("SOP entity never appeared in graph: %w", ctx.Err())
		case <-ticker.C:
			entries, err := s.http.GetMessageLogEntries(ctx, 50, "graph.ingest.entity")
			if err != nil {
				continue
			}
			if len(entries) == 0 {
				continue
			}

			sopEntities := 0
			for _, entry := range entries {
				raw := string(entry.RawData)
				if strings.Contains(raw, sourceVocab.DocCategory) {
					sopEntities++
				}
			}

			if sopEntities > 0 {
				result.SetDetail("sop_entities_found", sopEntities)
				result.SetDetail("total_graph_entities", len(entries))
				return nil
			}
		}
	}
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
	return nil
}

// stageWaitForPlan waits for plan.json with a non-empty goal.
func (s *TodoAppScenario) stageWaitForPlan(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	if err := s.fs.WaitForPlan(ctx, slug); err != nil {
		return fmt.Errorf("plan directory not created: %w", err)
	}

	if err := s.fs.WaitForPlanFile(ctx, slug, "plan.json"); err != nil {
		return fmt.Errorf("plan.json not created: %w", err)
	}

	planPath := s.fs.DefaultProjectPlanPath(slug) + "/plan.json"
	ticker := time.NewTicker(kvPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("plan.json never received goal from LLM: %w", ctx.Err())
		case <-ticker.C:
			var plan map[string]any
			if err := s.fs.ReadJSON(planPath, &plan); err != nil {
				continue
			}
			if goal, ok := plan["goal"].(string); ok && goal != "" {
				result.SetDetail("plan_file_exists", true)
				return nil
			}
		}
	}
}

// stageVerifyPlanSemantics validates that the plan references existing code
// and understands the brownfield context.
func (s *TodoAppScenario) stageVerifyPlanSemantics(_ context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")
	planPath := s.fs.DefaultProjectPlanPath(slug) + "/plan.json"

	var plan map[string]any
	if err := s.fs.ReadJSON(planPath, &plan); err != nil {
		return fmt.Errorf("read plan.json: %w", err)
	}

	goal, _ := plan["goal"].(string)
	planJSON, _ := json.Marshal(plan)
	planStr := string(planJSON)

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

// stageApprovePlan approves the plan via the REST API.
// Retries up to maxReviewAttempts if the plan-reviewer returns needs_changes.
func (s *TodoAppScenario) stageApprovePlan(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	for attempt := 1; attempt <= maxReviewAttempts; attempt++ {
		resp, err := s.http.PromotePlan(ctx, slug)
		if err != nil {
			return fmt.Errorf("promote plan (attempt %d): %w", attempt, err)
		}

		if resp.Error != "" {
			return fmt.Errorf("promote returned error: %s", resp.Error)
		}

		result.SetDetail("review_verdict", resp.ReviewVerdict)
		result.SetDetail("review_summary", resp.ReviewSummary)
		result.SetDetail("review_stage", resp.Stage)
		result.SetDetail("review_findings_count", len(resp.ReviewFindings))
		result.SetDetail("review_attempts", attempt)

		if resp.IsApproved() {
			result.SetDetail("approve_response", resp)
			return nil
		}

		// Review returned needs_changes — record findings
		for i, f := range resp.ReviewFindings {
			result.SetDetail(fmt.Sprintf("finding_%d", i), map[string]string{
				"sop_id":   f.SOPID,
				"severity": f.Severity,
				"status":   f.Status,
				"issue":    f.Issue,
			})
		}

		if attempt < maxReviewAttempts {
			result.AddWarning(fmt.Sprintf("plan review attempt %d/%d returned needs_changes: %s — retrying",
				attempt, maxReviewAttempts, resp.ReviewSummary))
			select {
			case <-ctx.Done():
				return fmt.Errorf("context cancelled during review retry: %w", ctx.Err())
			case <-time.After(reviewRetryBackoff * time.Duration(attempt)):
			}
		} else {
			// Final attempt exhausted — plan was never approved
			return fmt.Errorf("plan review rejected after %d attempts: %s", maxReviewAttempts, resp.ReviewSummary)
		}
	}

	return nil
}

// stageGenerateTasks triggers task generation via the REST API.
func (s *TodoAppScenario) stageGenerateTasks(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	resp, err := s.http.GenerateTasks(ctx, slug)
	if err != nil {
		return fmt.Errorf("generate tasks: %w", err)
	}

	if resp.Error != "" {
		return fmt.Errorf("generate tasks returned error: %s", resp.Error)
	}

	result.SetDetail("generate_response", resp)
	return nil
}

// stageWaitForTasks waits for tasks.json to be created.
func (s *TodoAppScenario) stageWaitForTasks(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	if err := s.fs.WaitForPlanFile(ctx, slug, "tasks.json"); err != nil {
		return fmt.Errorf("tasks.json not created: %w", err)
	}

	return nil
}

// stageVerifyTasksSemantics validates task ordering, coverage, and SOP compliance.
func (s *TodoAppScenario) stageVerifyTasksSemantics(_ context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")
	tasksPath := s.fs.DefaultProjectPlanPath(slug) + "/tasks.json"

	var tasks []map[string]any
	if err := s.fs.ReadJSON(tasksPath, &tasks); err != nil {
		return fmt.Errorf("read tasks.json: %w", err)
	}

	// Known files for reference checking
	knownFiles := []string{
		"api/main.go", "api/handlers.go", "api/models.go", "api/go.mod",
		"ui/src/routes/+page.svelte", "ui/src/lib/api.ts", "ui/src/lib/types.ts",
	}

	report := &SemanticReport{}

	// At least 3 tasks (model + handler + API client + component is minimum 3-4)
	report.Add("minimum-tasks",
		len(tasks) >= 3,
		fmt.Sprintf("got %d tasks, need >= 3", len(tasks)))

	// Tasks cover both api/ and ui/
	report.Add("tasks-cover-both-dirs",
		tasksCoverBothDirs(tasks, "api", "ui"),
		"tasks should span both api/ and ui/ directories")

	// Tasks are ordered: backend before frontend
	report.Add("tasks-ordered-backend-first",
		tasksAreOrdered(tasks, "api", "ui"),
		"backend tasks should precede frontend tasks")

	// Tasks reference actual existing files, not hallucinated paths
	report.Add("tasks-reference-known-files",
		tasksReferenceExistingFiles(tasks, knownFiles, 2),
		"at least 2 tasks should reference known project files")

	// Tasks mention due date concept
	report.Add("tasks-mention-due-dates",
		tasksHaveKeywordInDescription(tasks, "due date", "due_date", "deadline", "date"),
		"tasks should mention due dates")

	// SOP compliance: model changes need migration plan
	// Uses tasksHaveKeyword (broader) to check description, files, and acceptance_criteria
	hasMigration := tasksHaveKeyword(tasks, "migration", "schema", "migrate")
	report.Add("sop-migration-compliance",
		hasMigration,
		"SOP requires migration plan for model changes")

	// SOP compliance: new field in both API and UI types
	// Uses tasksHaveKeyword (broader) to check description, files, and acceptance_criteria
	hasBothTypes := tasksHaveKeyword(tasks, "types.ts", "type") &&
		tasksHaveKeyword(tasks, "models.go", "model", "struct")
	report.Add("sop-type-sync-compliance",
		hasBothTypes,
		"SOP requires new fields in both API types and UI types")

	// Every task has a description
	allValid := true
	for i, task := range tasks {
		desc, _ := task["description"].(string)
		if desc == "" {
			allValid = false
			report.Add(fmt.Sprintf("task-%d-has-description", i), false, "missing description")
			break
		}
	}
	if allValid {
		report.Add("all-tasks-have-description", true, "")
	}

	// Record all checks
	result.SetDetail("task_count", len(tasks))
	for _, check := range report.Checks {
		result.SetDetail("task_semantic_"+check.Name, check.Passed)
	}
	result.SetDetail("task_semantic_pass_rate", report.PassRate())

	if report.HasFailures() {
		return fmt.Errorf("task semantic validation failed (%.0f%% pass rate): %s",
			report.PassRate()*100, report.Error())
	}
	return nil
}

// stageCaptureTrajectory polls the LLM_CALLS KV bucket and retrieves trajectory data.
func (s *TodoAppScenario) stageCaptureTrajectory(ctx context.Context, result *Result) error {
	ticker := time.NewTicker(kvPollInterval)
	defer ticker.Stop()

	var kvEntries *client.KVEntriesResponse
	var lastErr error

	for kvEntries == nil {
		select {
		case <-ctx.Done():
			if lastErr != nil {
				result.AddWarning(fmt.Sprintf("trajectory capture timed out: %v (last error: %v)", ctx.Err(), lastErr))
			} else {
				result.AddWarning(fmt.Sprintf("trajectory capture timed out: %v", ctx.Err()))
			}
			return nil
		case <-ticker.C:
			entries, err := s.http.GetKVEntries(ctx, "LLM_CALLS")
			if err != nil {
				lastErr = err
				continue
			}
			if len(entries.Entries) > 0 {
				kvEntries = entries
			}
		}
	}

	firstKey := kvEntries.Entries[0].Key
	parts := strings.SplitN(firstKey, ".", 2)
	if len(parts) < 2 {
		result.AddWarning(fmt.Sprintf("LLM_CALLS key %q doesn't contain trace prefix", firstKey))
		return nil
	}

	traceID := parts[0]
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
