package gatherers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	vocab "github.com/c360studio/semspec/vocabulary/source"
)

// FileGatherer gathers context from filesystem files.
type FileGatherer struct {
	repoPath string
}

// NewFileGatherer creates a new file gatherer.
func NewFileGatherer(repoPath string) *FileGatherer {
	return &FileGatherer{
		repoPath: repoPath,
	}
}

// FileContent represents a file with its content and metadata.
type FileContent struct {
	Path    string
	Content string
	Size    int64
	Tokens  int // Estimated tokens
}

// ReadFile reads a single file and returns its content.
// Validates that the path is within the repository and doesn't escape via symlinks.
func (g *FileGatherer) ReadFile(ctx context.Context, path string) (*FileContent, error) {
	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Resolve path relative to repo
	fullPath := path
	if !filepath.IsAbs(path) {
		fullPath = filepath.Join(g.repoPath, path)
	}

	// Get absolute paths
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}
	absRepo, err := filepath.Abs(g.repoPath)
	if err != nil {
		return nil, fmt.Errorf("resolve repo path: %w", err)
	}

	// Initial prefix check (before symlink resolution)
	if !strings.HasPrefix(absPath, absRepo+string(filepath.Separator)) && absPath != absRepo {
		return nil, fmt.Errorf("path %q is outside repository", path)
	}

	// Resolve symlinks to detect path traversal attacks
	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		// File might not exist yet, or symlink is broken
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found: %s", path)
		}
		return nil, fmt.Errorf("resolve symlinks: %w", err)
	}

	realRepo, err := filepath.EvalSymlinks(absRepo)
	if err != nil {
		return nil, fmt.Errorf("resolve repo symlinks: %w", err)
	}

	// Check that resolved path is still within resolved repo
	if !strings.HasPrefix(realPath, realRepo+string(filepath.Separator)) && realPath != realRepo {
		return nil, fmt.Errorf("path %q resolves outside repository (symlink escape)", path)
	}

	// Read file
	info, err := os.Stat(realPath)
	if err != nil {
		return nil, fmt.Errorf("stat file: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("path is a directory: %s", path)
	}

	content, err := os.ReadFile(realPath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	return &FileContent{
		Path:    path,
		Content: string(content),
		Size:    info.Size(),
		Tokens:  len(content) / 4, // Rough estimate
	}, nil
}

// ReadFiles reads multiple files, respecting a total token budget.
// Returns files in order until budget is exhausted.
func (g *FileGatherer) ReadFiles(ctx context.Context, paths []string, maxTokens int) (map[string]string, int, error) {
	if len(paths) == 0 {
		return nil, 0, nil
	}

	result := make(map[string]string)
	totalTokens := 0

	for _, path := range paths {
		// Check context
		if err := ctx.Err(); err != nil {
			return result, totalTokens, err
		}

		file, err := g.ReadFile(ctx, path)
		if err != nil {
			// Skip files that can't be read
			continue
		}

		// Check if this file fits in budget
		if maxTokens > 0 && totalTokens+file.Tokens > maxTokens {
			// File doesn't fit, stop here
			break
		}

		result[path] = file.Content
		totalTokens += file.Tokens
	}

	return result, totalTokens, nil
}

// ReadFilesPartial reads files, truncating as needed to fit within budget.
// Unlike ReadFiles, this will include partial content from the last file.
func (g *FileGatherer) ReadFilesPartial(ctx context.Context, paths []string, maxTokens int) (map[string]string, int, bool, error) {
	if len(paths) == 0 {
		return nil, 0, false, nil
	}

	result := make(map[string]string)
	totalTokens := 0
	truncated := false

	for _, path := range paths {
		// Check context
		if err := ctx.Err(); err != nil {
			return result, totalTokens, truncated, err
		}

		file, err := g.ReadFile(ctx, path)
		if err != nil {
			continue
		}

		remaining := maxTokens - totalTokens
		if maxTokens > 0 && file.Tokens > remaining {
			// Truncate this file to fit
			if remaining > 100 { // Only include if we have meaningful space
				charLimit := remaining * 4 // Rough conversion back to chars
				content := file.Content
				if len(content) > charLimit {
					content = content[:charLimit] + "\n...[truncated]"
					truncated = true
				}
				result[path] = content
				totalTokens += remaining
			}
			break
		}

		result[path] = file.Content
		totalTokens += file.Tokens
	}

	return result, totalTokens, truncated, nil
}

// FindTestFiles finds test files related to the given source files.
func (g *FileGatherer) FindTestFiles(ctx context.Context, sourceFiles []string) ([]string, error) {
	testFiles := make([]string, 0)

	for _, src := range sourceFiles {
		// Check context
		if err := ctx.Err(); err != nil {
			return testFiles, err
		}

		dir := filepath.Dir(src)
		base := filepath.Base(src)
		ext := filepath.Ext(base)
		name := strings.TrimSuffix(base, ext)

		// Common test file patterns
		patterns := []string{
			filepath.Join(dir, name+"_test"+ext),  // Go style
			filepath.Join(dir, name+".test"+ext),  // JS/TS style
			filepath.Join(dir, name+".spec"+ext),  // JS/TS spec style
			filepath.Join(dir, "__tests__", base), // Jest style
			filepath.Join(dir, "test_"+base),      // Python style
			filepath.Join(dir, "tests", name+"_test"+ext),
		}

		for _, pattern := range patterns {
			fullPath := filepath.Join(g.repoPath, pattern)
			if _, err := os.Stat(fullPath); err == nil {
				testFiles = append(testFiles, pattern)
			}
		}
	}

	// Remove duplicates
	seen := make(map[string]bool)
	unique := make([]string, 0, len(testFiles))
	for _, f := range testFiles {
		if !seen[f] {
			seen[f] = true
			unique = append(unique, f)
		}
	}

	return unique, nil
}

// ListFiles lists files matching a pattern.
func (g *FileGatherer) ListFiles(ctx context.Context, pattern string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fullPattern := filepath.Join(g.repoPath, pattern)
	matches, err := filepath.Glob(fullPattern)
	if err != nil {
		return nil, fmt.Errorf("glob pattern: %w", err)
	}

	// Convert to relative paths
	result := make([]string, 0, len(matches))
	for _, m := range matches {
		rel, err := filepath.Rel(g.repoPath, m)
		if err != nil {
			continue
		}
		result = append(result, rel)
	}

	sort.Strings(result)
	return result, nil
}

// ListFilesRecursive walks the repository and returns all file paths (relative).
// Skips common non-source directories (.git, node_modules, vendor, etc.)
// so the LLM sees the actual project structure for scope generation.
func (g *FileGatherer) ListFilesRecursive(ctx context.Context) ([]string, error) {
	skipDirs := map[string]bool{
		".git":         true,
		".semspec":     true,
		"node_modules": true,
		"vendor":       true,
		"__pycache__":  true,
		".venv":        true,
		"venv":         true,
		"dist":         true,
		"build":        true,
		".next":        true,
		".svelte-kit":  true,
		".idea":        true,
		".vscode":      true,
		"coverage":     true,
		"target":       true, // Java/Rust build
		"bin":          true,
		".terraform":   true,
	}

	var files []string
	err := filepath.WalkDir(g.repoPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip entries we can't read
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Skip hidden and known non-source directories
		if d.IsDir() {
			name := d.Name()
			if skipDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(g.repoPath, path)
		if err != nil {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return files, fmt.Errorf("walk repo: %w", err)
	}

	sort.Strings(files)
	return files, nil
}

// FileExists checks if a file exists.
func (g *FileGatherer) FileExists(path string) bool {
	fullPath := filepath.Join(g.repoPath, path)
	_, err := os.Stat(fullPath)
	return err == nil
}

// pathDomainPatterns maps file path patterns to semantic domains.
// Used to infer domains from changed files during code review.
var pathDomainPatterns = map[string][]string{
	"auth":           {"auth/", "authentication/", "login/", "session/", "oauth/", "jwt/", "token/"},
	"security":       {"security/", "crypto/", "secrets/", "encrypt/", "ssl/", "tls/"},
	"database":       {"db/", "database/", "migrations/", "models/", "sql/", "store/", "repository/"},
	"api":            {"api/", "handlers/", "routes/", "endpoints/", "controller/", "rest/", "grpc/"},
	"messaging":      {"nats/", "messaging/", "pubsub/", "events/", "queue/", "kafka/", "amqp/"},
	"testing":        {"test/", "tests/", "_test.go", ".test.", ".spec.", "__tests__/"},
	"logging":        {"log/", "logger/", "observability/", "metrics/", "tracing/", "telemetry/"},
	"deployment":     {"deploy/", "ci/", "cd/", ".github/", "docker/", "k8s/", "kubernetes/", "helm/"},
	"config":         {"config/", "configs/", "settings/", "env/"},
	"performance":    {"cache/", "caching/", "benchmark/", "perf/", "optimize/"},
	"error-handling": {"error/", "errors/", "recovery/", "retry/", "circuit/"},
	"validation":     {"validate/", "validation/", "sanitize/", "schema/"},
}

// InferDomains infers semantic domains from file paths.
// Used during code review to determine which domain-specific SOPs apply.
func (g *FileGatherer) InferDomains(ctx context.Context, files []string) []string {
	domains := make(map[string]bool)

	for _, file := range files {
		// Check for context cancellation on large file lists
		if ctx.Err() != nil {
			break
		}
		fileLower := strings.ToLower(file)
		for domain, patterns := range pathDomainPatterns {
			for _, pattern := range patterns {
				if strings.Contains(fileLower, pattern) {
					domains[domain] = true
					break
				}
			}
		}
	}

	// Convert map to slice
	result := make([]string, 0, len(domains))
	for d := range domains {
		result = append(result, d)
	}

	sort.Strings(result)
	return result
}

// ExpandRelatedDomains expands a list of domains to include related domains.
// Uses the canonical RelatedDomains map from the vocabulary package.
// E.g., ["auth"] → ["auth", "security", "validation"]
func (g *FileGatherer) ExpandRelatedDomains(domains []string) []string {
	expanded := make(map[string]bool)

	// Include original domains
	for _, d := range domains {
		expanded[d] = true
	}

	// Add related domains from vocabulary package's canonical map
	for _, d := range domains {
		if related, ok := vocab.RelatedDomains[vocab.DomainType(d)]; ok {
			for _, r := range related {
				expanded[string(r)] = true
			}
		}
	}

	// Convert map to slice
	result := make([]string, 0, len(expanded))
	for d := range expanded {
		result = append(result, d)
	}

	sort.Strings(result)
	return result
}
