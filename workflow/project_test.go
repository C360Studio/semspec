package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProjectEntityID(t *testing.T) {
	tests := []struct {
		slug     string
		expected string
	}{
		{"default", "semspec.local.project.default"},
		{"my-project", "semspec.local.project.my-project"},
		{"auth-service", "semspec.local.project.auth-service"},
	}

	for _, tt := range tests {
		t.Run(tt.slug, func(t *testing.T) {
			got := ProjectEntityID(tt.slug)
			if got != tt.expected {
				t.Errorf("ProjectEntityID(%q) = %q, want %q", tt.slug, got, tt.expected)
			}
		})
	}
}

func TestManager_CreateProject(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)
	ctx := context.Background()

	t.Run("creates project successfully", func(t *testing.T) {
		project, err := m.CreateProject(ctx, "test-project", "Test Project")
		if err != nil {
			t.Fatalf("CreateProject() error = %v", err)
		}

		if project.Slug != "test-project" {
			t.Errorf("Slug = %q, want %q", project.Slug, "test-project")
		}
		if project.Title != "Test Project" {
			t.Errorf("Title = %q, want %q", project.Title, "Test Project")
		}
		if project.ID != "semspec.local.project.test-project" {
			t.Errorf("ID = %q, want %q", project.ID, "semspec.local.project.test-project")
		}
		if project.Status != ProjectStatusActive {
			t.Errorf("Status = %q, want %q", project.Status, ProjectStatusActive)
		}

		// Verify directory structure
		projectDir := filepath.Join(tmpDir, ".semspec", "projects", "test-project")
		if _, err := os.Stat(projectDir); os.IsNotExist(err) {
			t.Error("project directory was not created")
		}

		plansDir := filepath.Join(projectDir, "plans")
		if _, err := os.Stat(plansDir); os.IsNotExist(err) {
			t.Error("plans directory was not created")
		}
	})

	t.Run("rejects duplicate project", func(t *testing.T) {
		_, err := m.CreateProject(ctx, "duplicate", "Duplicate")
		if err != nil {
			t.Fatalf("First CreateProject() error = %v", err)
		}

		_, err = m.CreateProject(ctx, "duplicate", "Duplicate Again")
		if err == nil {
			t.Error("expected error for duplicate project")
		}
	})

	t.Run("rejects invalid slug", func(t *testing.T) {
		_, err := m.CreateProject(ctx, "../escape", "Escape")
		if err == nil {
			t.Error("expected error for invalid slug")
		}
	})

	t.Run("rejects empty title", func(t *testing.T) {
		_, err := m.CreateProject(ctx, "no-title", "")
		if err == nil {
			t.Error("expected error for empty title")
		}
	})
}

func TestManager_LoadProject(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)
	ctx := context.Background()

	t.Run("loads existing project", func(t *testing.T) {
		created, err := m.CreateProject(ctx, "load-test", "Load Test")
		if err != nil {
			t.Fatalf("CreateProject() error = %v", err)
		}

		loaded, err := m.LoadProject(ctx, "load-test")
		if err != nil {
			t.Fatalf("LoadProject() error = %v", err)
		}

		if loaded.ID != created.ID {
			t.Errorf("ID = %q, want %q", loaded.ID, created.ID)
		}
		if loaded.Slug != created.Slug {
			t.Errorf("Slug = %q, want %q", loaded.Slug, created.Slug)
		}
	})

	t.Run("returns error for non-existent project", func(t *testing.T) {
		_, err := m.LoadProject(ctx, "non-existent")
		if err == nil {
			t.Error("expected error for non-existent project")
		}
	})
}

func TestManager_GetOrCreateDefaultProject(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)
	ctx := context.Background()

	t.Run("creates default project on first call", func(t *testing.T) {
		project, err := m.GetOrCreateDefaultProject(ctx)
		if err != nil {
			t.Fatalf("GetOrCreateDefaultProject() error = %v", err)
		}

		if project.Slug != DefaultProjectSlug {
			t.Errorf("Slug = %q, want %q", project.Slug, DefaultProjectSlug)
		}
	})

	t.Run("returns existing default project on subsequent calls", func(t *testing.T) {
		first, _ := m.GetOrCreateDefaultProject(ctx)
		second, err := m.GetOrCreateDefaultProject(ctx)
		if err != nil {
			t.Fatalf("Second GetOrCreateDefaultProject() error = %v", err)
		}

		if second.ID != first.ID {
			t.Errorf("ID changed between calls")
		}
	})
}

func TestManager_ListProjects(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)
	ctx := context.Background()

	// Create some projects
	_, _ = m.CreateProject(ctx, "project-a", "Project A")
	_, _ = m.CreateProject(ctx, "project-b", "Project B")

	result, err := m.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}

	if len(result.Projects) != 2 {
		t.Errorf("len(Projects) = %d, want 2", len(result.Projects))
	}
}

func TestManager_ArchiveProject(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)
	ctx := context.Background()

	_, err := m.CreateProject(ctx, "to-archive", "To Archive")
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	err = m.ArchiveProject(ctx, "to-archive")
	if err != nil {
		t.Fatalf("ArchiveProject() error = %v", err)
	}

	project, err := m.LoadProject(ctx, "to-archive")
	if err != nil {
		t.Fatalf("LoadProject() error = %v", err)
	}

	if project.Status != ProjectStatusArchived {
		t.Errorf("Status = %q, want %q", project.Status, ProjectStatusArchived)
	}
	if project.ArchivedAt == nil {
		t.Error("ArchivedAt should be set")
	}
}

func TestManager_CreateProjectPlan(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)
	ctx := context.Background()

	// Create a project first
	_, err := m.CreateProject(ctx, "my-project", "My Project")
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	t.Run("creates plan in project", func(t *testing.T) {
		plan, err := m.CreateProjectPlan(ctx, "my-project", "add-auth", "Add Authentication")
		if err != nil {
			t.Fatalf("CreateProjectPlan() error = %v", err)
		}

		if plan.Slug != "add-auth" {
			t.Errorf("Slug = %q, want %q", plan.Slug, "add-auth")
		}
		if plan.ProjectID != "semspec.local.project.my-project" {
			t.Errorf("ProjectID = %q, want %q", plan.ProjectID, "semspec.local.project.my-project")
		}
		if plan.Approved {
			t.Error("new plan should not be approved")
		}

		// Verify file was created
		planFile := filepath.Join(tmpDir, ".semspec", "projects", "my-project", "plans", "add-auth", "plan.json")
		if _, err := os.Stat(planFile); os.IsNotExist(err) {
			t.Error("plan.json was not created")
		}
	})

	t.Run("creates plan in default project auto-creating it", func(t *testing.T) {
		tmpDir2 := t.TempDir()
		m2 := NewManager(tmpDir2)

		plan, err := m2.CreateProjectPlan(ctx, DefaultProjectSlug, "quick-fix", "Quick Fix")
		if err != nil {
			t.Fatalf("CreateProjectPlan() error = %v", err)
		}

		if plan.ProjectID != "semspec.local.project.default" {
			t.Errorf("ProjectID = %q, want %q", plan.ProjectID, "semspec.local.project.default")
		}

		// Verify default project was created
		if !m2.ProjectExists(DefaultProjectSlug) {
			t.Error("default project was not created")
		}
	})

	t.Run("rejects plan for non-existent project", func(t *testing.T) {
		_, err := m.CreateProjectPlan(ctx, "non-existent", "some-plan", "Some Plan")
		if err == nil {
			t.Error("expected error for non-existent project")
		}
	})
}

func TestManager_ListProjectPlans(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)
	ctx := context.Background()

	_, _ = m.CreateProject(ctx, "multi-plan", "Multi Plan Project")
	_, _ = m.CreateProjectPlan(ctx, "multi-plan", "plan-1", "Plan One")
	_, _ = m.CreateProjectPlan(ctx, "multi-plan", "plan-2", "Plan Two")

	result, err := m.ListProjectPlans(ctx, "multi-plan")
	if err != nil {
		t.Fatalf("ListProjectPlans() error = %v", err)
	}

	if len(result.Plans) != 2 {
		t.Errorf("len(Plans) = %d, want 2", len(result.Plans))
	}
}

func TestProject_IsArchived(t *testing.T) {
	active := &Project{Status: ProjectStatusActive}
	archived := &Project{Status: ProjectStatusArchived}

	if active.IsArchived() {
		t.Error("active project should not be archived")
	}
	if !archived.IsArchived() {
		t.Error("archived project should be archived")
	}
}

func TestManager_DeleteProject(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)
	ctx := context.Background()

	_, _ = m.CreateProject(ctx, "to-delete", "To Delete")

	err := m.DeleteProject(ctx, "to-delete")
	if err != nil {
		t.Fatalf("DeleteProject() error = %v", err)
	}

	if m.ProjectExists("to-delete") {
		t.Error("project should not exist after deletion")
	}
}

func TestManager_UpdateProject(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)
	ctx := context.Background()

	_, _ = m.CreateProject(ctx, "to-update", "Original Title")

	// Wait a moment to ensure UpdatedAt changes
	time.Sleep(10 * time.Millisecond)

	err := m.UpdateProject(ctx, "to-update", func(p *Project) {
		p.Title = "Updated Title"
		p.Description = "New description"
	})
	if err != nil {
		t.Fatalf("UpdateProject() error = %v", err)
	}

	updated, _ := m.LoadProject(ctx, "to-update")
	if updated.Title != "Updated Title" {
		t.Errorf("Title = %q, want %q", updated.Title, "Updated Title")
	}
	if updated.Description != "New description" {
		t.Errorf("Description = %q, want %q", updated.Description, "New description")
	}
}

func TestManager_CreateProject_Concurrent(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)
	ctx := context.Background()

	const numGoroutines = 10
	results := make(chan error, numGoroutines)

	// All goroutines try to create the same project
	for i := 0; i < numGoroutines; i++ {
		go func() {
			_, err := m.CreateProject(ctx, "concurrent-project", "Concurrent Project")
			results <- err
		}()
	}

	var successCount, existsCount int
	for i := 0; i < numGoroutines; i++ {
		err := <-results
		if err == nil {
			successCount++
		} else if errors.Is(err, ErrProjectExists) {
			existsCount++
		} else {
			t.Errorf("unexpected error: %v", err)
		}
	}

	if successCount != 1 {
		t.Errorf("expected exactly 1 success, got %d", successCount)
	}
	if existsCount != numGoroutines-1 {
		t.Errorf("expected %d ErrProjectExists, got %d", numGoroutines-1, existsCount)
	}
}

func TestManager_UpdateProject_Concurrent(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)
	ctx := context.Background()

	// Create project first
	_, err := m.CreateProject(ctx, "concurrent-update", "Initial Title")
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	const numGoroutines = 10
	done := make(chan bool, numGoroutines)

	// All goroutines try to update the same project concurrently
	for i := 0; i < numGoroutines; i++ {
		go func(n int) {
			err := m.UpdateProject(ctx, "concurrent-update", func(p *Project) {
				p.Description = fmt.Sprintf("Update %d", n)
			})
			if err != nil {
				t.Errorf("UpdateProject() error = %v", err)
			}
			done <- true
		}(i)
	}

	// Wait for all updates to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify project is in a consistent state (description should be one of the updates)
	project, err := m.LoadProject(ctx, "concurrent-update")
	if err != nil {
		t.Fatalf("LoadProject() error = %v", err)
	}

	// Description should start with "Update " (one of the concurrent updates won)
	if len(project.Description) < 7 || project.Description[:7] != "Update " {
		t.Errorf("Description = %q, expected to start with 'Update '", project.Description)
	}
}

func TestManager_CreateProjectPlan_Concurrent(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir)
	ctx := context.Background()

	// Create project first
	_, err := m.CreateProject(ctx, "plan-concurrent", "Plan Concurrent Project")
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	const numGoroutines = 10
	results := make(chan error, numGoroutines)

	// All goroutines try to create the same plan
	for i := 0; i < numGoroutines; i++ {
		go func() {
			_, err := m.CreateProjectPlan(ctx, "plan-concurrent", "same-plan", "Same Plan")
			results <- err
		}()
	}

	var successCount, existsCount int
	for i := 0; i < numGoroutines; i++ {
		err := <-results
		if err == nil {
			successCount++
		} else if errors.Is(err, ErrPlanExists) {
			existsCount++
		} else {
			t.Errorf("unexpected error: %v", err)
		}
	}

	if successCount != 1 {
		t.Errorf("expected exactly 1 success, got %d", successCount)
	}
	if existsCount != numGoroutines-1 {
		t.Errorf("expected %d ErrPlanExists, got %d", numGoroutines-1, existsCount)
	}
}
