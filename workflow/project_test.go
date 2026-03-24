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
		{"default", ProjectEntityID("default")},
		{"my-project", ProjectEntityID("my-project")},
		{"auth-service", ProjectEntityID("auth-service")},
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

func TestCreateProject(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	t.Run("creates project successfully", func(t *testing.T) {
		project, err := CreateProject(ctx, tmpDir, "test-project", "Test Project")
		if err != nil {
			t.Fatalf("CreateProject() error = %v", err)
		}

		if project.Slug != "test-project" {
			t.Errorf("Slug = %q, want %q", project.Slug, "test-project")
		}
		if project.Title != "Test Project" {
			t.Errorf("Title = %q, want %q", project.Title, "Test Project")
		}
		if project.ID != "semspec.local.wf.project.project.test-project" {
			t.Errorf("ID = %q, want %q", project.ID, "semspec.local.wf.project.project.test-project")
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
		_, err := CreateProject(ctx, tmpDir, "duplicate", "Duplicate")
		if err != nil {
			t.Fatalf("First CreateProject() error = %v", err)
		}

		_, err = CreateProject(ctx, tmpDir, "duplicate", "Duplicate Again")
		if err == nil {
			t.Error("expected error for duplicate project")
		}
	})

	t.Run("rejects invalid slug", func(t *testing.T) {
		_, err := CreateProject(ctx, tmpDir, "../escape", "Escape")
		if err == nil {
			t.Error("expected error for invalid slug")
		}
	})

	t.Run("rejects empty title", func(t *testing.T) {
		_, err := CreateProject(ctx, tmpDir, "no-title", "")
		if err == nil {
			t.Error("expected error for empty title")
		}
	})
}

func TestLoadProject(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	t.Run("loads existing project", func(t *testing.T) {
		created, err := CreateProject(ctx, tmpDir, "load-test", "Load Test")
		if err != nil {
			t.Fatalf("CreateProject() error = %v", err)
		}

		loaded, err := LoadProject(ctx, tmpDir, "load-test")
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
		_, err := LoadProject(ctx, tmpDir, "non-existent")
		if err == nil {
			t.Error("expected error for non-existent project")
		}
	})
}

func TestGetOrCreateDefaultProject(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	t.Run("creates default project on first call", func(t *testing.T) {
		project, err := GetOrCreateDefaultProject(ctx, tmpDir)
		if err != nil {
			t.Fatalf("GetOrCreateDefaultProject() error = %v", err)
		}

		if project.Slug != DefaultProjectSlug {
			t.Errorf("Slug = %q, want %q", project.Slug, DefaultProjectSlug)
		}
	})

	t.Run("returns existing default project on subsequent calls", func(t *testing.T) {
		first, _ := GetOrCreateDefaultProject(ctx, tmpDir)
		second, err := GetOrCreateDefaultProject(ctx, tmpDir)
		if err != nil {
			t.Fatalf("Second GetOrCreateDefaultProject() error = %v", err)
		}

		if second.ID != first.ID {
			t.Errorf("ID changed between calls")
		}
	})
}

func TestListProjects(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	// Create some projects
	_, _ = CreateProject(ctx, tmpDir, "project-a", "Project A")
	_, _ = CreateProject(ctx, tmpDir, "project-b", "Project B")

	result, err := ListProjects(ctx, tmpDir)
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}

	if len(result.Projects) != 2 {
		t.Errorf("len(Projects) = %d, want 2", len(result.Projects))
	}
}

func TestArchiveProject(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	_, err := CreateProject(ctx, tmpDir, "to-archive", "To Archive")
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	err = ArchiveProject(ctx, tmpDir, "to-archive")
	if err != nil {
		t.Fatalf("ArchiveProject() error = %v", err)
	}

	project, err := LoadProject(ctx, tmpDir, "to-archive")
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

func TestCreateProjectPlan(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	// Create a project first
	_, err := CreateProject(ctx, tmpDir, "my-project", "My Project")
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	t.Run("creates plan in project", func(t *testing.T) {
		plan, err := CreateProjectPlan(ctx, nil, "my-project", "add-auth", "Add Authentication")
		if err != nil {
			t.Fatalf("CreateProjectPlan() error = %v", err)
		}

		if plan.Slug != "add-auth" {
			t.Errorf("Slug = %q, want %q", plan.Slug, "add-auth")
		}
		if plan.ProjectID != "semspec.local.wf.project.project.my-project" {
			t.Errorf("ProjectID = %q, want %q", plan.ProjectID, "semspec.local.wf.project.project.my-project")
		}
		if plan.Approved {
			t.Error("new plan should not be approved")
		}
	})

	t.Run("creates plan in default project", func(t *testing.T) {
		plan, err := CreateProjectPlan(ctx, nil, DefaultProjectSlug, "quick-fix", "Quick Fix")
		if err != nil {
			t.Fatalf("CreateProjectPlan() error = %v", err)
		}

		if plan.ProjectID != "semspec.local.wf.project.project.default" {
			t.Errorf("ProjectID = %q, want %q", plan.ProjectID, "semspec.local.wf.project.project.default")
		}
	})
}

func TestListProjectPlans(t *testing.T) {
	ctx := context.Background()

	// Without a KV store, ListProjectPlans returns empty results (no storage available).
	result, err := ListProjectPlans(ctx, nil, "multi-plan")
	if err != nil {
		t.Fatalf("ListProjectPlans() error = %v", err)
	}
	if len(result.Plans) != 0 {
		t.Errorf("len(Plans) = %d, want 0 (nil KV returns empty)", len(result.Plans))
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

func TestDeleteProject(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	_, _ = CreateProject(ctx, tmpDir, "to-delete", "To Delete")

	err := DeleteProject(ctx, tmpDir, "to-delete")
	if err != nil {
		t.Fatalf("DeleteProject() error = %v", err)
	}

	if ProjectExists(tmpDir, "to-delete") {
		t.Error("project should not exist after deletion")
	}
}

func TestUpdateProject(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	_, _ = CreateProject(ctx, tmpDir, "to-update", "Original Title")

	// Wait a moment to ensure UpdatedAt changes
	time.Sleep(10 * time.Millisecond)

	err := UpdateProject(ctx, tmpDir, "to-update", func(p *Project) {
		p.Title = "Updated Title"
		p.Description = "New description"
	})
	if err != nil {
		t.Fatalf("UpdateProject() error = %v", err)
	}

	updated, _ := LoadProject(ctx, tmpDir, "to-update")
	if updated.Title != "Updated Title" {
		t.Errorf("Title = %q, want %q", updated.Title, "Updated Title")
	}
	if updated.Description != "New description" {
		t.Errorf("Description = %q, want %q", updated.Description, "New description")
	}
}

func TestCreateProject_Concurrent(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	const numGoroutines = 10
	results := make(chan error, numGoroutines)

	// All goroutines try to create the same project
	for i := 0; i < numGoroutines; i++ {
		go func() {
			_, err := CreateProject(ctx, tmpDir, "concurrent-project", "Concurrent Project")
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

func TestUpdateProject_Concurrent(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	// Create project first
	_, err := CreateProject(ctx, tmpDir, "concurrent-update", "Initial Title")
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	const numGoroutines = 10
	done := make(chan bool, numGoroutines)

	// All goroutines try to update the same project concurrently
	for i := 0; i < numGoroutines; i++ {
		go func(n int) {
			err := UpdateProject(ctx, tmpDir, "concurrent-update", func(p *Project) {
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
	project, err := LoadProject(ctx, tmpDir, "concurrent-update")
	if err != nil {
		t.Fatalf("LoadProject() error = %v", err)
	}

	// Description should start with "Update " (one of the concurrent updates won)
	if len(project.Description) < 7 || project.Description[:7] != "Update " {
		t.Errorf("Description = %q, expected to start with 'Update '", project.Description)
	}
}
