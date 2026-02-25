package workflowapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestExtractSlugAndEndpoint(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		wantSlug     string
		wantEndpoint string
	}{
		{
			name:         "standard path",
			path:         "/workflow-api/plans/authentication-options/reviews",
			wantSlug:     "authentication-options",
			wantEndpoint: "reviews",
		},
		{
			name:         "with trailing slash",
			path:         "/workflow-api/plans/my-feature/reviews/",
			wantSlug:     "my-feature",
			wantEndpoint: "reviews",
		},
		{
			name:         "no endpoint",
			path:         "/workflow-api/plans/test-slug",
			wantSlug:     "test-slug",
			wantEndpoint: "",
		},
		{
			name:         "empty path",
			path:         "",
			wantSlug:     "",
			wantEndpoint: "",
		},
		{
			name:         "no plans segment",
			path:         "/workflow-api/something/else",
			wantSlug:     "",
			wantEndpoint: "",
		},
		{
			name:         "slug with dashes",
			path:         "/workflow-api/plans/add-user-auth-flow/reviews",
			wantSlug:     "add-user-auth-flow",
			wantEndpoint: "reviews",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSlug, gotEndpoint := extractSlugAndEndpoint(tt.path)
			if gotSlug != tt.wantSlug {
				t.Errorf("extractSlugAndEndpoint() slug = %q, want %q", gotSlug, tt.wantSlug)
			}
			if gotEndpoint != tt.wantEndpoint {
				t.Errorf("extractSlugAndEndpoint() endpoint = %q, want %q", gotEndpoint, tt.wantEndpoint)
			}
		})
	}
}

func TestFindReviewResult(t *testing.T) {
	c := &Component{}

	tests := []struct {
		name     string
		exec     *WorkflowExecution
		wantName string
		wantNil  bool
	}{
		{
			name: "finds review step",
			exec: &WorkflowExecution{
				StepResults: map[string]*StepResult{
					"review": {
						StepName: "review",
						Status:   "success",
						Output:   json.RawMessage(`{"verdict":"approved"}`),
					},
				},
			},
			wantName: "review",
			wantNil:  false,
		},
		{
			name: "finds review-synthesis step",
			exec: &WorkflowExecution{
				StepResults: map[string]*StepResult{
					"review-synthesis": {
						StepName: "review-synthesis",
						Status:   "success",
						Output:   json.RawMessage(`{"verdict":"approved"}`),
					},
				},
			},
			wantName: "review-synthesis",
			wantNil:  false,
		},
		{
			name: "ignores failed review",
			exec: &WorkflowExecution{
				StepResults: map[string]*StepResult{
					"review": {
						StepName: "review",
						Status:   "failed",
						Output:   json.RawMessage(`{"error":"timeout"}`),
					},
				},
			},
			wantNil: true,
		},
		{
			name: "no step results",
			exec: &WorkflowExecution{
				StepResults: nil,
			},
			wantNil: true,
		},
		{
			name: "empty step results",
			exec: &WorkflowExecution{
				StepResults: map[string]*StepResult{},
			},
			wantNil: true,
		},
		{
			name: "non-review step",
			exec: &WorkflowExecution{
				StepResults: map[string]*StepResult{
					"implement": {
						StepName: "implement",
						Status:   "success",
						Output:   json.RawMessage(`{"result":"done"}`),
					},
				},
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := c.findReviewResult(tt.exec)
			if tt.wantNil {
				if result != nil {
					t.Errorf("findReviewResult() expected nil, got %+v", result)
				}
				return
			}
			if result == nil {
				t.Error("findReviewResult() got nil, expected non-nil")
				return
			}
			if result.StepName != tt.wantName {
				t.Errorf("findReviewResult() step name = %q, want %q", result.StepName, tt.wantName)
			}
		})
	}
}

func TestTriggerPayloadParsing(t *testing.T) {
	tests := []struct {
		name     string
		trigger  string
		wantSlug string
		wantOk   bool
	}{
		{
			name:     "valid trigger with data",
			trigger:  `{"workflow_id":"review","data":{"slug":"my-feature","title":"My Feature"}}`,
			wantSlug: "my-feature",
			wantOk:   true,
		},
		{
			name:     "trigger without data",
			trigger:  `{"workflow_id":"review"}`,
			wantSlug: "",
			wantOk:   true,
		},
		{
			name:     "invalid JSON",
			trigger:  `{invalid}`,
			wantSlug: "",
			wantOk:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var trigger TriggerPayload
			err := json.Unmarshal([]byte(tt.trigger), &trigger)
			if tt.wantOk {
				if err != nil {
					t.Errorf("Unmarshal() error = %v, want nil", err)
					return
				}
				gotSlug := trigger.GetSlug()
				if gotSlug != tt.wantSlug {
					t.Errorf("slug = %q, want %q", gotSlug, tt.wantSlug)
				}
			} else {
				if err == nil {
					t.Error("Unmarshal() expected error, got nil")
				}
			}
		})
	}
}

func TestExtractSlugTaskAndAction(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantSlug   string
		wantTaskID string
		wantAction string
	}{
		{
			name:       "get task",
			path:       "/workflow-api/plans/my-feature/tasks/task.my-feature.1",
			wantSlug:   "my-feature",
			wantTaskID: "task.my-feature.1",
			wantAction: "",
		},
		{
			name:       "approve task",
			path:       "/workflow-api/plans/my-feature/tasks/task.my-feature.1/approve",
			wantSlug:   "my-feature",
			wantTaskID: "task.my-feature.1",
			wantAction: "approve",
		},
		{
			name:       "reject task",
			path:       "/workflow-api/plans/my-feature/tasks/task.my-feature.1/reject",
			wantSlug:   "my-feature",
			wantTaskID: "task.my-feature.1",
			wantAction: "reject",
		},
		{
			name:       "with trailing slash",
			path:       "/workflow-api/plans/test-slug/tasks/task.test-slug.2/approve/",
			wantSlug:   "test-slug",
			wantTaskID: "task.test-slug.2",
			wantAction: "approve",
		},
		{
			name:       "invalid - missing tasks segment",
			path:       "/workflow-api/plans/test-slug/something/task.test.1",
			wantSlug:   "",
			wantTaskID: "",
			wantAction: "",
		},
		{
			name:       "invalid - insufficient parts",
			path:       "/workflow-api/plans/test-slug/tasks",
			wantSlug:   "",
			wantTaskID: "",
			wantAction: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSlug, gotTaskID, gotAction := extractSlugTaskAndAction(tt.path)
			if gotSlug != tt.wantSlug {
				t.Errorf("extractSlugTaskAndAction() slug = %q, want %q", gotSlug, tt.wantSlug)
			}
			if gotTaskID != tt.wantTaskID {
				t.Errorf("extractSlugTaskAndAction() taskID = %q, want %q", gotTaskID, tt.wantTaskID)
			}
			if gotAction != tt.wantAction {
				t.Errorf("extractSlugTaskAndAction() action = %q, want %q", gotAction, tt.wantAction)
			}
		})
	}
}

// setupTestComponent creates a component for testing HTTP handlers.
func setupTestComponent(t *testing.T) *Component {
	t.Helper()

	c := &Component{
		logger: slog.Default(),
	}

	return c
}

// setupTestPlanWithTasks creates a test plan with tasks in the specified statuses.
func setupTestPlanWithTasks(t *testing.T, slug string, taskStatuses []workflow.TaskStatus) {
	t.Helper()

	ctx := context.Background()
	tmpDir := t.TempDir()

	// Set SEMSPEC_REPO_PATH so getManager() finds our temp directory
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)

	// Create and approve plan
	plan, err := m.CreatePlan(ctx, slug, "Test Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	if err := m.ApprovePlan(ctx, plan); err != nil {
		t.Fatalf("ApprovePlan() error = %v", err)
	}

	// Create tasks with specified statuses using CreateTask
	tasks := make([]workflow.Task, len(taskStatuses))
	for i, status := range taskStatuses {
		task, err := workflow.CreateTask(plan.ID, slug, i+1, "Test task")
		if err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}
		// Override the default pending status
		task.Status = status
		tasks[i] = *task
	}

	if err := m.SaveTasks(ctx, tasks, slug); err != nil {
		t.Fatalf("SaveTasks() error = %v", err)
	}
}

func TestHandleGetTask(t *testing.T) {
	tests := []struct {
		name           string
		setupFunc      func(t *testing.T) string
		taskID         string
		wantStatusCode int
		wantStatus     workflow.TaskStatus
	}{
		{
			name: "success - task exists",
			setupFunc: func(t *testing.T) string {
				slug := "get-task-exists"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{
					workflow.TaskStatusPendingApproval,
				})
				return slug
			},
			taskID:         workflow.TaskEntityID("get-task-exists", 1),
			wantStatusCode: http.StatusOK,
			wantStatus:     workflow.TaskStatusPendingApproval,
		},
		{
			name: "not found - task doesn't exist",
			setupFunc: func(t *testing.T) string {
				slug := "get-task-not-found"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{
					workflow.TaskStatusPending,
				})
				return slug
			},
			taskID:         workflow.TaskEntityID("get-task-not-found", 999),
			wantStatusCode: http.StatusNotFound,
		},
		{
			name: "not found - plan doesn't exist",
			setupFunc: func(t *testing.T) string {
				// Don't create any plan
				t.Setenv("SEMSPEC_REPO_PATH", t.TempDir())
				return "nonexistent-plan"
			},
			taskID:         workflow.TaskEntityID("nonexistent-plan", 1),
			wantStatusCode: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slug := tt.setupFunc(t)
			c := setupTestComponent(t)

			req := httptest.NewRequest(http.MethodGet, "/workflow-api/plans/"+slug+"/tasks/"+tt.taskID, nil)
			w := httptest.NewRecorder()

			c.handleGetTask(w, req, slug, tt.taskID)

			if w.Code != tt.wantStatusCode {
				t.Errorf("handleGetTask() status = %d, want %d", w.Code, tt.wantStatusCode)
			}

			if tt.wantStatusCode == http.StatusOK {
				var task workflow.Task
				if err := json.NewDecoder(w.Body).Decode(&task); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				if task.ID != tt.taskID {
					t.Errorf("task.ID = %q, want %q", task.ID, tt.taskID)
				}

				if task.Status != tt.wantStatus {
					t.Errorf("task.Status = %q, want %q", task.Status, tt.wantStatus)
				}
			}
		})
	}
}

func TestHandleApproveTask(t *testing.T) {
	tests := []struct {
		name           string
		setupFunc      func(t *testing.T) string
		taskID         string
		requestBody    string
		wantStatusCode int
		wantStatus     workflow.TaskStatus
		wantApprovedBy string
	}{
		{
			name: "success - pending_approval task with approver",
			setupFunc: func(t *testing.T) string {
				slug := "approve-task-success"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{
					workflow.TaskStatusPendingApproval,
				})
				return slug
			},
			taskID:         workflow.TaskEntityID("approve-task-success", 1),
			requestBody:    `{"approved_by":"user-123"}`,
			wantStatusCode: http.StatusOK,
			wantStatus:     workflow.TaskStatusApproved,
			wantApprovedBy: "user-123",
		},
		{
			name: "success - pending_approval task without approver defaults to system",
			setupFunc: func(t *testing.T) string {
				slug := "approve-task-default"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{
					workflow.TaskStatusPendingApproval,
				})
				return slug
			},
			taskID:         workflow.TaskEntityID("approve-task-default", 1),
			requestBody:    `{}`,
			wantStatusCode: http.StatusOK,
			wantStatus:     workflow.TaskStatusApproved,
			wantApprovedBy: "system",
		},
		{
			name: "conflict - task not pending_approval (pending)",
			setupFunc: func(t *testing.T) string {
				slug := "approve-task-pending"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{
					workflow.TaskStatusPending,
				})
				return slug
			},
			taskID:         workflow.TaskEntityID("approve-task-pending", 1),
			requestBody:    `{"approved_by":"user-123"}`,
			wantStatusCode: http.StatusConflict,
		},
		{
			name: "conflict - task already approved",
			setupFunc: func(t *testing.T) string {
				slug := "approve-task-already"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{
					workflow.TaskStatusApproved,
				})
				return slug
			},
			taskID:         workflow.TaskEntityID("approve-task-already", 1),
			requestBody:    `{"approved_by":"user-123"}`,
			wantStatusCode: http.StatusConflict,
		},
		{
			name: "not found - task doesn't exist",
			setupFunc: func(t *testing.T) string {
				slug := "approve-task-not-found"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{
					workflow.TaskStatusPendingApproval,
				})
				return slug
			},
			taskID:         workflow.TaskEntityID("approve-task-not-found", 999),
			requestBody:    `{"approved_by":"user-123"}`,
			wantStatusCode: http.StatusNotFound,
		},
		{
			name: "bad request - invalid JSON",
			setupFunc: func(t *testing.T) string {
				slug := "approve-task-bad-json"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{
					workflow.TaskStatusPendingApproval,
				})
				return slug
			},
			taskID:         workflow.TaskEntityID("approve-task-bad-json", 1),
			requestBody:    `{invalid json}`,
			wantStatusCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slug := tt.setupFunc(t)
			c := setupTestComponent(t)

			req := httptest.NewRequest(
				http.MethodPost,
				"/workflow-api/plans/"+slug+"/tasks/"+tt.taskID+"/approve",
				bytes.NewBufferString(tt.requestBody),
			)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			c.handleApproveTask(w, req, slug, tt.taskID)

			if w.Code != tt.wantStatusCode {
				t.Errorf("handleApproveTask() status = %d, want %d", w.Code, tt.wantStatusCode)
				t.Logf("Response body: %s", w.Body.String())
			}

			if tt.wantStatusCode == http.StatusOK {
				var task workflow.Task
				if err := json.NewDecoder(w.Body).Decode(&task); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				if task.ID != tt.taskID {
					t.Errorf("task.ID = %q, want %q", task.ID, tt.taskID)
				}

				if task.Status != tt.wantStatus {
					t.Errorf("task.Status = %q, want %q", task.Status, tt.wantStatus)
				}

				if task.ApprovedBy != tt.wantApprovedBy {
					t.Errorf("task.ApprovedBy = %q, want %q", task.ApprovedBy, tt.wantApprovedBy)
				}

				if task.ApprovedAt == nil {
					t.Error("task.ApprovedAt should be set")
				}

				// Verify persistence
				ctx := context.Background()
				repoPath := os.Getenv("SEMSPEC_REPO_PATH")
				m := workflow.NewManager(repoPath)
				loaded, err := m.GetTask(ctx, slug, tt.taskID)
				if err != nil {
					t.Fatalf("Failed to load task after approval: %v", err)
				}

				if loaded.Status != workflow.TaskStatusApproved {
					t.Errorf("Persisted task status = %q, want %q", loaded.Status, workflow.TaskStatusApproved)
				}

				if loaded.ApprovedBy != tt.wantApprovedBy {
					t.Errorf("Persisted task approved_by = %q, want %q", loaded.ApprovedBy, tt.wantApprovedBy)
				}
			}
		})
	}
}

func TestHandleRejectTask(t *testing.T) {
	tests := []struct {
		name                string
		setupFunc           func(t *testing.T) string
		taskID              string
		requestBody         string
		wantStatusCode      int
		wantStatus          workflow.TaskStatus
		wantRejectionReason string
	}{
		{
			name: "success - pending_approval task with reason",
			setupFunc: func(t *testing.T) string {
				slug := "reject-task-success"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{
					workflow.TaskStatusPendingApproval,
				})
				return slug
			},
			taskID:              workflow.TaskEntityID("reject-task-success", 1),
			requestBody:         `{"reason":"Acceptance criteria unclear"}`,
			wantStatusCode:      http.StatusOK,
			wantStatus:          workflow.TaskStatusRejected,
			wantRejectionReason: "Acceptance criteria unclear",
		},
		{
			name: "bad request - missing reason",
			setupFunc: func(t *testing.T) string {
				slug := "reject-task-no-reason"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{
					workflow.TaskStatusPendingApproval,
				})
				return slug
			},
			taskID:         workflow.TaskEntityID("reject-task-no-reason", 1),
			requestBody:    `{}`,
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name: "bad request - empty reason",
			setupFunc: func(t *testing.T) string {
				slug := "reject-task-empty-reason"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{
					workflow.TaskStatusPendingApproval,
				})
				return slug
			},
			taskID:         workflow.TaskEntityID("reject-task-empty-reason", 1),
			requestBody:    `{"reason":""}`,
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name: "conflict - task not pending_approval (pending)",
			setupFunc: func(t *testing.T) string {
				slug := "reject-task-pending"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{
					workflow.TaskStatusPending,
				})
				return slug
			},
			taskID:         workflow.TaskEntityID("reject-task-pending", 1),
			requestBody:    `{"reason":"Too complex"}`,
			wantStatusCode: http.StatusConflict,
		},
		{
			name: "conflict - task already approved",
			setupFunc: func(t *testing.T) string {
				slug := "reject-task-approved"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{
					workflow.TaskStatusApproved,
				})
				return slug
			},
			taskID:         workflow.TaskEntityID("reject-task-approved", 1),
			requestBody:    `{"reason":"Changed my mind"}`,
			wantStatusCode: http.StatusConflict,
		},
		{
			name: "not found - task doesn't exist",
			setupFunc: func(t *testing.T) string {
				slug := "reject-task-not-found"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{
					workflow.TaskStatusPendingApproval,
				})
				return slug
			},
			taskID:         workflow.TaskEntityID("reject-task-not-found", 999),
			requestBody:    `{"reason":"Doesn't matter"}`,
			wantStatusCode: http.StatusNotFound,
		},
		{
			name: "bad request - invalid JSON",
			setupFunc: func(t *testing.T) string {
				slug := "reject-task-bad-json"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{
					workflow.TaskStatusPendingApproval,
				})
				return slug
			},
			taskID:         workflow.TaskEntityID("reject-task-bad-json", 1),
			requestBody:    `{invalid json}`,
			wantStatusCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slug := tt.setupFunc(t)
			c := setupTestComponent(t)

			req := httptest.NewRequest(
				http.MethodPost,
				"/workflow-api/plans/"+slug+"/tasks/"+tt.taskID+"/reject",
				bytes.NewBufferString(tt.requestBody),
			)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			c.handleRejectTask(w, req, slug, tt.taskID)

			if w.Code != tt.wantStatusCode {
				t.Errorf("handleRejectTask() status = %d, want %d", w.Code, tt.wantStatusCode)
				t.Logf("Response body: %s", w.Body.String())
			}

			if tt.wantStatusCode == http.StatusOK {
				var task workflow.Task
				if err := json.NewDecoder(w.Body).Decode(&task); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				if task.ID != tt.taskID {
					t.Errorf("task.ID = %q, want %q", task.ID, tt.taskID)
				}

				if task.Status != tt.wantStatus {
					t.Errorf("task.Status = %q, want %q", task.Status, tt.wantStatus)
				}

				if task.RejectionReason != tt.wantRejectionReason {
					t.Errorf("task.RejectionReason = %q, want %q", task.RejectionReason, tt.wantRejectionReason)
				}

				// Verify persistence
				ctx := context.Background()
				repoPath := os.Getenv("SEMSPEC_REPO_PATH")
				m := workflow.NewManager(repoPath)
				loaded, err := m.GetTask(ctx, slug, tt.taskID)
				if err != nil {
					t.Fatalf("Failed to load task after rejection: %v", err)
				}

				if loaded.Status != workflow.TaskStatusRejected {
					t.Errorf("Persisted task status = %q, want %q", loaded.Status, workflow.TaskStatusRejected)
				}

				if loaded.RejectionReason != tt.wantRejectionReason {
					t.Errorf("Persisted task rejection_reason = %q, want %q", loaded.RejectionReason, tt.wantRejectionReason)
				}
			}
		})
	}
}

func TestHandleCreateTask(t *testing.T) {
	tests := []struct {
		name           string
		setupFunc      func(t *testing.T) string
		requestBody    string
		wantStatusCode int
		wantType       workflow.TaskType
		wantFilesCount int
	}{
		{
			name: "success - create implement task with full details",
			setupFunc: func(t *testing.T) string {
				slug := "create-task-success"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{})
				return slug
			},
			requestBody: `{
				"description": "Implement user authentication",
				"type": "implement",
				"acceptance_criteria": [
					{"description": "User can log in", "met": false}
				],
				"files": ["auth.go", "auth_test.go"],
				"depends_on": []
			}`,
			wantStatusCode: http.StatusCreated,
			wantType:       workflow.TaskTypeImplement,
			wantFilesCount: 2,
		},
		{
			name: "success - create test task minimal",
			setupFunc: func(t *testing.T) string {
				slug := "create-task-minimal"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{})
				return slug
			},
			requestBody: `{
				"description": "Write integration tests",
				"type": "test"
			}`,
			wantStatusCode: http.StatusCreated,
			wantType:       workflow.TaskTypeTest,
			wantFilesCount: 0,
		},
		{
			name: "success - create task without type defaults to implement",
			setupFunc: func(t *testing.T) string {
				slug := "create-task-default-type"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{})
				return slug
			},
			requestBody:    `{"description": "Task with default type"}`,
			wantStatusCode: http.StatusCreated,
			wantType:       workflow.TaskTypeImplement,
			wantFilesCount: 0,
		},
		{
			name: "bad request - missing description",
			setupFunc: func(t *testing.T) string {
				slug := "create-task-no-desc"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{})
				return slug
			},
			requestBody:    `{"type": "implement"}`,
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name: "bad request - empty description",
			setupFunc: func(t *testing.T) string {
				slug := "create-task-empty-desc"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{})
				return slug
			},
			requestBody:    `{"description": "", "type": "implement"}`,
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name: "bad request - invalid task type",
			setupFunc: func(t *testing.T) string {
				slug := "create-task-invalid-type"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{})
				return slug
			},
			requestBody:    `{"description": "Do something", "type": "invalid"}`,
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name: "not found - plan doesn't exist",
			setupFunc: func(t *testing.T) string {
				t.Setenv("SEMSPEC_REPO_PATH", t.TempDir())
				return "nonexistent-plan"
			},
			requestBody:    `{"description": "Task for nonexistent plan", "type": "implement"}`,
			wantStatusCode: http.StatusNotFound,
		},
		{
			name: "bad request - invalid JSON",
			setupFunc: func(t *testing.T) string {
				slug := "create-task-bad-json"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{})
				return slug
			},
			requestBody:    `{invalid json}`,
			wantStatusCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slug := tt.setupFunc(t)
			c := setupTestComponent(t)

			req := httptest.NewRequest(
				http.MethodPost,
				"/workflow-api/plans/"+slug+"/tasks",
				bytes.NewBufferString(tt.requestBody),
			)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			c.handleCreateTask(w, req, slug)

			if w.Code != tt.wantStatusCode {
				t.Errorf("handleCreateTask() status = %d, want %d", w.Code, tt.wantStatusCode)
				t.Logf("Response body: %s", w.Body.String())
			}

			if tt.wantStatusCode == http.StatusCreated {
				var task workflow.Task
				if err := json.NewDecoder(w.Body).Decode(&task); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				if task.Type != tt.wantType {
					t.Errorf("task.Type = %q, want %q", task.Type, tt.wantType)
				}

				if len(task.Files) != tt.wantFilesCount {
					t.Errorf("len(task.Files) = %d, want %d", len(task.Files), tt.wantFilesCount)
				}

				if task.Status != workflow.TaskStatusPending {
					t.Errorf("task.Status = %q, want %q", task.Status, workflow.TaskStatusPending)
				}

				// Verify persistence
				ctx := context.Background()
				repoPath := os.Getenv("SEMSPEC_REPO_PATH")
				m := workflow.NewManager(repoPath)
				loaded, err := m.GetTask(ctx, slug, task.ID)
				if err != nil {
					t.Fatalf("Failed to load task after creation: %v", err)
				}

				if loaded.Description != task.Description {
					t.Errorf("Persisted task description = %q, want %q", loaded.Description, task.Description)
				}
			}
		})
	}
}

func TestHandleUpdateTask(t *testing.T) {
	tests := []struct {
		name            string
		setupFunc       func(t *testing.T) (string, string)
		requestBody     string
		wantStatusCode  int
		wantDescription string
		wantSequence    int
	}{
		{
			name: "success - update description",
			setupFunc: func(t *testing.T) (string, string) {
				slug := "update-task-desc"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{workflow.TaskStatusPending})
				return slug, workflow.TaskEntityID(slug, 1)
			},
			requestBody:     `{"description": "Updated task description"}`,
			wantStatusCode:  http.StatusOK,
			wantDescription: "Updated task description",
			wantSequence:    1,
		},
		{
			name: "success - update files and dependencies",
			setupFunc: func(t *testing.T) (string, string) {
				slug := "update-task-files"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{workflow.TaskStatusPending, workflow.TaskStatusPending})
				return slug, workflow.TaskEntityID(slug, 2)
			},
			requestBody:    `{"files": ["main.go", "main_test.go"], "depends_on": ["task.update-task-files.1"]}`,
			wantStatusCode: http.StatusOK,
			wantSequence:   2,
		},
		{
			name: "success - update sequence (reorder)",
			setupFunc: func(t *testing.T) (string, string) {
				slug := "update-task-seq"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{
					workflow.TaskStatusPending,
					workflow.TaskStatusPending,
					workflow.TaskStatusPending,
				})
				return slug, workflow.TaskEntityID(slug, 3)
			},
			requestBody:    `{"sequence": 1}`,
			wantStatusCode: http.StatusOK,
			wantSequence:   1,
		},
		{
			name: "conflict - task is in_progress",
			setupFunc: func(t *testing.T) (string, string) {
				slug := "update-task-in-progress"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{workflow.TaskStatusInProgress})
				return slug, workflow.TaskEntityID(slug, 1)
			},
			requestBody:    `{"description": "Cannot update"}`,
			wantStatusCode: http.StatusConflict,
		},
		{
			name: "conflict - task is completed",
			setupFunc: func(t *testing.T) (string, string) {
				slug := "update-task-completed"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{workflow.TaskStatusCompleted})
				return slug, workflow.TaskEntityID(slug, 1)
			},
			requestBody:    `{"description": "Cannot update"}`,
			wantStatusCode: http.StatusConflict,
		},
		{
			name: "conflict - task is failed",
			setupFunc: func(t *testing.T) (string, string) {
				slug := "update-task-failed"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{workflow.TaskStatusFailed})
				return slug, workflow.TaskEntityID(slug, 1)
			},
			requestBody:    `{"description": "Cannot update"}`,
			wantStatusCode: http.StatusConflict,
		},
		{
			name: "not found - task doesn't exist",
			setupFunc: func(t *testing.T) (string, string) {
				slug := "update-task-not-found"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{workflow.TaskStatusPending})
				return slug, workflow.TaskEntityID(slug, 999)
			},
			requestBody:    `{"description": "Updated"}`,
			wantStatusCode: http.StatusNotFound,
		},
		{
			name: "bad request - invalid task type",
			setupFunc: func(t *testing.T) (string, string) {
				slug := "update-task-invalid-type"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{workflow.TaskStatusPending})
				return slug, workflow.TaskEntityID(slug, 1)
			},
			requestBody:    `{"type": "invalid"}`,
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name: "bad request - invalid JSON",
			setupFunc: func(t *testing.T) (string, string) {
				slug := "update-task-bad-json"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{workflow.TaskStatusPending})
				return slug, workflow.TaskEntityID(slug, 1)
			},
			requestBody:    `{invalid json}`,
			wantStatusCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slug, taskID := tt.setupFunc(t)
			c := setupTestComponent(t)

			req := httptest.NewRequest(
				http.MethodPatch,
				"/workflow-api/plans/"+slug+"/tasks/"+taskID,
				bytes.NewBufferString(tt.requestBody),
			)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			c.handleUpdateTask(w, req, slug, taskID)

			if w.Code != tt.wantStatusCode {
				t.Errorf("handleUpdateTask() status = %d, want %d", w.Code, tt.wantStatusCode)
				t.Logf("Response body: %s", w.Body.String())
			}

			if tt.wantStatusCode == http.StatusOK {
				var task workflow.Task
				if err := json.NewDecoder(w.Body).Decode(&task); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				if tt.wantDescription != "" && task.Description != tt.wantDescription {
					t.Errorf("task.Description = %q, want %q", task.Description, tt.wantDescription)
				}

				if task.Sequence != tt.wantSequence {
					t.Errorf("task.Sequence = %d, want %d", task.Sequence, tt.wantSequence)
				}

				// Verify persistence
				ctx := context.Background()
				repoPath := os.Getenv("SEMSPEC_REPO_PATH")
				m := workflow.NewManager(repoPath)
				loaded, err := m.GetTask(ctx, slug, taskID)
				if err != nil {
					t.Fatalf("Failed to load task after update: %v", err)
				}

				if tt.wantDescription != "" && loaded.Description != tt.wantDescription {
					t.Errorf("Persisted task description = %q, want %q", loaded.Description, tt.wantDescription)
				}
			}
		})
	}
}

func TestHandleDeleteTask(t *testing.T) {
	tests := []struct {
		name           string
		setupFunc      func(t *testing.T) (string, string)
		wantStatusCode int
	}{
		{
			name: "success - delete pending task",
			setupFunc: func(t *testing.T) (string, string) {
				slug := "delete-task-success"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{
					workflow.TaskStatusPending,
					workflow.TaskStatusPending,
				})
				return slug, workflow.TaskEntityID(slug, 1)
			},
			wantStatusCode: http.StatusNoContent,
		},
		{
			name: "success - delete approved task",
			setupFunc: func(t *testing.T) (string, string) {
				slug := "delete-task-approved"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{
					workflow.TaskStatusApproved,
				})
				return slug, workflow.TaskEntityID(slug, 1)
			},
			wantStatusCode: http.StatusNoContent,
		},
		{
			name: "conflict - task is in_progress",
			setupFunc: func(t *testing.T) (string, string) {
				slug := "delete-task-in-progress"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{
					workflow.TaskStatusInProgress,
				})
				return slug, workflow.TaskEntityID(slug, 1)
			},
			wantStatusCode: http.StatusConflict,
		},
		{
			name: "conflict - task is completed",
			setupFunc: func(t *testing.T) (string, string) {
				slug := "delete-task-completed"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{
					workflow.TaskStatusCompleted,
				})
				return slug, workflow.TaskEntityID(slug, 1)
			},
			wantStatusCode: http.StatusConflict,
		},
		{
			name: "conflict - task is failed",
			setupFunc: func(t *testing.T) (string, string) {
				slug := "delete-task-failed"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{
					workflow.TaskStatusFailed,
				})
				return slug, workflow.TaskEntityID(slug, 1)
			},
			wantStatusCode: http.StatusConflict,
		},
		{
			name: "not found - task doesn't exist",
			setupFunc: func(t *testing.T) (string, string) {
				slug := "delete-task-not-found"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{
					workflow.TaskStatusPending,
				})
				return slug, workflow.TaskEntityID(slug, 999)
			},
			wantStatusCode: http.StatusNotFound,
		},
		{
			name: "not found - plan doesn't exist",
			setupFunc: func(t *testing.T) (string, string) {
				t.Setenv("SEMSPEC_REPO_PATH", t.TempDir())
				return "nonexistent-plan", workflow.TaskEntityID("nonexistent-plan", 1)
			},
			wantStatusCode: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slug, taskID := tt.setupFunc(t)
			c := setupTestComponent(t)

			req := httptest.NewRequest(
				http.MethodDelete,
				"/workflow-api/plans/"+slug+"/tasks/"+taskID,
				nil,
			)
			w := httptest.NewRecorder()

			c.handleDeleteTask(w, req, slug, taskID)

			if w.Code != tt.wantStatusCode {
				t.Errorf("handleDeleteTask() status = %d, want %d", w.Code, tt.wantStatusCode)
				t.Logf("Response body: %s", w.Body.String())
			}

			if tt.wantStatusCode == http.StatusNoContent {
				// Verify task is actually deleted
				ctx := context.Background()
				repoPath := os.Getenv("SEMSPEC_REPO_PATH")
				m := workflow.NewManager(repoPath)
				_, err := m.GetTask(ctx, slug, taskID)
				if !errors.Is(err, workflow.ErrTaskNotFound) {
					t.Errorf("Task should be deleted, got error: %v", err)
				}
			}
		})
	}
}

func TestHandleUpdatePlan(t *testing.T) {
	tests := []struct {
		name           string
		setupFunc      func(t *testing.T) string
		requestBody    string
		wantStatusCode int
		wantTitle      string
		wantGoal       string
		wantContext    string
	}{
		{
			name: "success - update title",
			setupFunc: func(t *testing.T) string {
				slug := "update-plan-title"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{})
				return slug
			},
			requestBody:    `{"title": "Updated Title"}`,
			wantStatusCode: http.StatusOK,
			wantTitle:      "Updated Title",
		},
		{
			name: "success - update goal",
			setupFunc: func(t *testing.T) string {
				slug := "update-plan-goal"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{})
				return slug
			},
			requestBody:    `{"goal": "Updated goal description"}`,
			wantStatusCode: http.StatusOK,
			wantGoal:       "Updated goal description",
		},
		{
			name: "success - update context",
			setupFunc: func(t *testing.T) string {
				slug := "update-plan-context"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{})
				return slug
			},
			requestBody:    `{"context": "Updated context information"}`,
			wantStatusCode: http.StatusOK,
			wantContext:    "Updated context information",
		},
		{
			name: "success - update all fields",
			setupFunc: func(t *testing.T) string {
				slug := "update-plan-all"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{})
				return slug
			},
			requestBody: `{
				"title": "New Title",
				"goal": "New Goal",
				"context": "New Context"
			}`,
			wantStatusCode: http.StatusOK,
			wantTitle:      "New Title",
			wantGoal:       "New Goal",
			wantContext:    "New Context",
		},
		{
			name: "success - partial update (empty request)",
			setupFunc: func(t *testing.T) string {
				slug := "update-plan-partial"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{})
				return slug
			},
			requestBody:    `{}`,
			wantStatusCode: http.StatusOK,
		},
		{
			name: "not found - plan doesn't exist",
			setupFunc: func(t *testing.T) string {
				t.Setenv("SEMSPEC_REPO_PATH", t.TempDir())
				return "nonexistent-plan"
			},
			requestBody:    `{"title": "New Title"}`,
			wantStatusCode: http.StatusNotFound,
		},
		{
			name: "conflict - plan is implementing",
			setupFunc: func(t *testing.T) string {
				slug := "update-plan-implementing"
				tmpDir := t.TempDir()
				t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
				m := workflow.NewManager(tmpDir)
				ctx := context.Background()
				plan, _ := m.CreatePlan(ctx, slug, "Test Plan")
				plan.Status = workflow.StatusImplementing
				_ = m.SavePlan(ctx, plan)
				return slug
			},
			requestBody:    `{"title": "Cannot update"}`,
			wantStatusCode: http.StatusConflict,
		},
		{
			name: "conflict - plan is complete",
			setupFunc: func(t *testing.T) string {
				slug := "update-plan-complete"
				tmpDir := t.TempDir()
				t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
				m := workflow.NewManager(tmpDir)
				ctx := context.Background()
				plan, _ := m.CreatePlan(ctx, slug, "Test Plan")
				plan.Status = workflow.StatusComplete
				_ = m.SavePlan(ctx, plan)
				return slug
			},
			requestBody:    `{"title": "Cannot update"}`,
			wantStatusCode: http.StatusConflict,
		},
		{
			name: "conflict - plan is archived",
			setupFunc: func(t *testing.T) string {
				slug := "update-plan-archived"
				tmpDir := t.TempDir()
				t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
				m := workflow.NewManager(tmpDir)
				ctx := context.Background()
				plan, _ := m.CreatePlan(ctx, slug, "Test Plan")
				plan.Status = workflow.StatusArchived
				_ = m.SavePlan(ctx, plan)
				return slug
			},
			requestBody:    `{"title": "Cannot update"}`,
			wantStatusCode: http.StatusConflict,
		},
		{
			name: "conflict - task is in_progress",
			setupFunc: func(t *testing.T) string {
				slug := "update-plan-task-in-progress"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{
					workflow.TaskStatusInProgress,
				})
				return slug
			},
			requestBody:    `{"title": "Cannot update"}`,
			wantStatusCode: http.StatusConflict,
		},
		{
			name: "bad request - invalid JSON",
			setupFunc: func(t *testing.T) string {
				slug := "update-plan-bad-json"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{})
				return slug
			},
			requestBody:    `{invalid json}`,
			wantStatusCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slug := tt.setupFunc(t)
			c := setupTestComponent(t)

			req := httptest.NewRequest(
				http.MethodPatch,
				"/workflow-api/plans/"+slug,
				bytes.NewBufferString(tt.requestBody),
			)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			c.handleUpdatePlan(w, req, slug)

			if w.Code != tt.wantStatusCode {
				t.Errorf("handleUpdatePlan() status = %d, want %d", w.Code, tt.wantStatusCode)
				t.Logf("Response body: %s", w.Body.String())
			}

			if tt.wantStatusCode == http.StatusOK {
				var resp PlanWithStatus
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				if tt.wantTitle != "" && resp.Plan.Title != tt.wantTitle {
					t.Errorf("plan.Title = %q, want %q", resp.Plan.Title, tt.wantTitle)
				}

				if tt.wantGoal != "" && resp.Plan.Goal != tt.wantGoal {
					t.Errorf("plan.Goal = %q, want %q", resp.Plan.Goal, tt.wantGoal)
				}

				if tt.wantContext != "" && resp.Plan.Context != tt.wantContext {
					t.Errorf("plan.Context = %q, want %q", resp.Plan.Context, tt.wantContext)
				}

				// Verify persistence
				ctx := context.Background()
				repoPath := os.Getenv("SEMSPEC_REPO_PATH")
				m := workflow.NewManager(repoPath)
				loaded, err := m.LoadPlan(ctx, slug)
				if err != nil {
					t.Fatalf("Failed to load plan after update: %v", err)
				}

				if tt.wantTitle != "" && loaded.Title != tt.wantTitle {
					t.Errorf("Persisted plan.Title = %q, want %q", loaded.Title, tt.wantTitle)
				}
			}
		})
	}
}

func TestHandleDeletePlan(t *testing.T) {
	tests := []struct {
		name           string
		setupFunc      func(t *testing.T) string
		archiveParam   string
		wantStatusCode int
		verifyArchived bool
	}{
		{
			name: "success - hard delete plan",
			setupFunc: func(t *testing.T) string {
				slug := "delete-plan-hard"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{})
				return slug
			},
			archiveParam:   "",
			wantStatusCode: http.StatusNoContent,
			verifyArchived: false,
		},
		{
			name: "success - hard delete with explicit false",
			setupFunc: func(t *testing.T) string {
				slug := "delete-plan-false"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{})
				return slug
			},
			archiveParam:   "false",
			wantStatusCode: http.StatusNoContent,
			verifyArchived: false,
		},
		{
			name: "success - soft delete (archive)",
			setupFunc: func(t *testing.T) string {
				slug := "delete-plan-archive"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{})
				return slug
			},
			archiveParam:   "true",
			wantStatusCode: http.StatusNoContent,
			verifyArchived: true,
		},
		{
			name: "not found - plan doesn't exist",
			setupFunc: func(t *testing.T) string {
				t.Setenv("SEMSPEC_REPO_PATH", t.TempDir())
				return "nonexistent-plan"
			},
			archiveParam:   "",
			wantStatusCode: http.StatusNotFound,
		},
		{
			name: "conflict - plan is implementing",
			setupFunc: func(t *testing.T) string {
				slug := "delete-plan-implementing"
				tmpDir := t.TempDir()
				t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
				m := workflow.NewManager(tmpDir)
				ctx := context.Background()
				plan, _ := m.CreatePlan(ctx, slug, "Test Plan")
				plan.Status = workflow.StatusImplementing
				_ = m.SavePlan(ctx, plan)
				return slug
			},
			archiveParam:   "",
			wantStatusCode: http.StatusConflict,
		},
		{
			name: "conflict - plan is complete",
			setupFunc: func(t *testing.T) string {
				slug := "delete-plan-complete"
				tmpDir := t.TempDir()
				t.Setenv("SEMSPEC_REPO_PATH", tmpDir)
				m := workflow.NewManager(tmpDir)
				ctx := context.Background()
				plan, _ := m.CreatePlan(ctx, slug, "Test Plan")
				plan.Status = workflow.StatusComplete
				_ = m.SavePlan(ctx, plan)
				return slug
			},
			archiveParam:   "",
			wantStatusCode: http.StatusConflict,
		},
		{
			name: "conflict - task is in_progress",
			setupFunc: func(t *testing.T) string {
				slug := "delete-plan-task-in-progress"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{
					workflow.TaskStatusInProgress,
				})
				return slug
			},
			archiveParam:   "",
			wantStatusCode: http.StatusConflict,
		},
		{
			name: "conflict - task is completed",
			setupFunc: func(t *testing.T) string {
				slug := "delete-plan-task-completed"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{
					workflow.TaskStatusCompleted,
				})
				return slug
			},
			archiveParam:   "",
			wantStatusCode: http.StatusConflict,
		},
		{
			name: "conflict - archive plan with active tasks",
			setupFunc: func(t *testing.T) string {
				slug := "archive-plan-active-tasks"
				setupTestPlanWithTasks(t, slug, []workflow.TaskStatus{
					workflow.TaskStatusInProgress,
				})
				return slug
			},
			archiveParam:   "true",
			wantStatusCode: http.StatusConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slug := tt.setupFunc(t)
			c := setupTestComponent(t)

			url := "/workflow-api/plans/" + slug
			if tt.archiveParam != "" {
				url += "?archive=" + tt.archiveParam
			}

			req := httptest.NewRequest(http.MethodDelete, url, nil)
			w := httptest.NewRecorder()

			c.handleDeletePlan(w, req, slug)

			if w.Code != tt.wantStatusCode {
				t.Errorf("handleDeletePlan() status = %d, want %d", w.Code, tt.wantStatusCode)
				t.Logf("Response body: %s", w.Body.String())
			}

			if tt.wantStatusCode == http.StatusNoContent {
				ctx := context.Background()
				repoPath := os.Getenv("SEMSPEC_REPO_PATH")
				m := workflow.NewManager(repoPath)

				if tt.verifyArchived {
					// Verify plan exists with archived status
					plan, err := m.LoadPlan(ctx, slug)
					if err != nil {
						t.Fatalf("Archived plan should still exist: %v", err)
					}
					if plan.Status != workflow.StatusArchived {
						t.Errorf("Plan status = %s, want %s", plan.Status, workflow.StatusArchived)
					}
				} else {
					// Verify plan is actually deleted
					_, err := m.LoadPlan(ctx, slug)
					if !errors.Is(err, workflow.ErrPlanNotFound) {
						t.Errorf("Plan should be deleted, got error: %v", err)
					}
				}
			}
		})
	}
}
