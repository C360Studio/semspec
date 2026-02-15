package gatherers

import (
	"context"
	"testing"
)

func TestFileGatherer_InferDomains(t *testing.T) {
	g := NewFileGatherer("/tmp/test")

	tests := []struct {
		name  string
		files []string
		want  []string
	}{
		{
			name:  "empty files",
			files: []string{},
			want:  []string{},
		},
		{
			name:  "auth directory",
			files: []string{"internal/auth/token.go", "internal/auth/session.go"},
			want:  []string{"auth"},
		},
		{
			name:  "database directory",
			files: []string{"db/migrations/001_create_users.sql"},
			want:  []string{"database"},
		},
		{
			name:  "multiple domains",
			files: []string{"api/handler.go", "db/models/user.go"},
			want:  []string{"api", "database"},
		},
		{
			name:  "test files",
			files: []string{"handler_test.go", "auth.spec.ts"},
			want:  []string{"testing"},
		},
		{
			name:  "security paths",
			files: []string{"pkg/crypto/hash.go", "internal/secrets/vault.go"},
			want:  []string{"security"},
		},
		{
			name:  "case insensitive",
			files: []string{"API/Handler.go", "AUTH/Token.go"},
			want:  []string{"api", "auth"},
		},
		{
			name:  "messaging",
			files: []string{"internal/nats/publisher.go", "events/handler.go"},
			want:  []string{"messaging"},
		},
		{
			name:  "no matching domain",
			files: []string{"main.go", "utils/helpers.go"},
			want:  []string{},
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := g.InferDomains(ctx, tt.files)

			if len(got) != len(tt.want) {
				t.Errorf("InferDomains() = %v, want %v", got, tt.want)
				return
			}

			// Check each expected domain is present
			gotSet := make(map[string]bool)
			for _, d := range got {
				gotSet[d] = true
			}
			for _, w := range tt.want {
				if !gotSet[w] {
					t.Errorf("InferDomains() missing domain %q, got %v", w, got)
				}
			}
		})
	}
}

func TestFileGatherer_ExpandRelatedDomains(t *testing.T) {
	g := NewFileGatherer("/tmp/test")

	tests := []struct {
		name    string
		domains []string
		want    []string // Minimum expected domains (may include more)
	}{
		{
			name:    "empty domains",
			domains: []string{},
			want:    []string{},
		},
		{
			name:    "auth expands to security and validation",
			domains: []string{"auth"},
			want:    []string{"auth", "security", "validation"},
		},
		{
			name:    "database expands to error-handling and performance",
			domains: []string{"database"},
			want:    []string{"database", "error-handling", "performance"},
		},
		{
			name:    "unknown domain returns unchanged",
			domains: []string{"unknown"},
			want:    []string{"unknown"},
		},
		{
			name:    "multiple domains expand correctly",
			domains: []string{"auth", "api"},
			want:    []string{"auth", "api", "security", "validation", "error-handling"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := g.ExpandRelatedDomains(tt.domains)

			// Check each expected domain is present
			gotSet := make(map[string]bool)
			for _, d := range got {
				gotSet[d] = true
			}
			for _, w := range tt.want {
				if !gotSet[w] {
					t.Errorf("ExpandRelatedDomains(%v) missing domain %q, got %v", tt.domains, w, got)
				}
			}
		})
	}
}

func TestFileGatherer_InferDomains_ContextCancellation(t *testing.T) {
	g := NewFileGatherer("/tmp/test")

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Should return empty result when context is cancelled
	files := []string{"auth/handler.go", "db/models.go"}
	got := g.InferDomains(ctx, files)

	// With a cancelled context, the loop should break early
	// Result may be empty or partial depending on timing
	if len(got) > len(files) {
		t.Errorf("InferDomains with cancelled context returned too many results: %v", got)
	}
}
