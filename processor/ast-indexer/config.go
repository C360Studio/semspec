package astindexer

import (
	"fmt"
	"time"

	"github.com/c360studio/semspec/processor/ast"
	"github.com/c360studio/semstreams/component"
)

// WatchPathConfig configures a single watch path with its parsing options.
type WatchPathConfig struct {
	// Path supports glob patterns: "./services/*", "./libs/**"
	Path string `json:"path" schema:"type:string,description:Path or glob pattern to watch,required:true"`

	// Org is the organization for entity IDs
	Org string `json:"org" schema:"type:string,description:Organization for entity IDs,required:true"`

	// Project is the project name for entity IDs
	Project string `json:"project" schema:"type:string,description:Project name for entity IDs,required:true"`

	// Languages to parse (registered parser names: go, typescript, javascript)
	Languages []string `json:"languages" schema:"type:array,description:Languages to parse,default:[go]"`

	// Excludes are directory names to skip
	Excludes []string `json:"excludes" schema:"type:array,description:Directory names to skip"`
}

// Validate checks the WatchPathConfig for errors.
func (w *WatchPathConfig) Validate() error {
	if w.Path == "" {
		return fmt.Errorf("path is required")
	}
	if w.Org == "" {
		return fmt.Errorf("org is required")
	}
	if w.Project == "" {
		return fmt.Errorf("project is required")
	}

	// Validate that all specified languages are registered
	for _, lang := range w.Languages {
		if !ast.DefaultRegistry.HasParser(lang) {
			return fmt.Errorf("unknown language: %s (registered: %v)", lang, ast.DefaultRegistry.ListParsers())
		}
	}

	return nil
}

// GetFileExtensions returns the file extensions for this watch path based on languages.
func (w *WatchPathConfig) GetFileExtensions() []string {
	var extensions []string
	seen := make(map[string]bool)

	languages := w.Languages
	if len(languages) == 0 {
		languages = []string{"go"}
	}

	for _, lang := range languages {
		for _, ext := range ast.DefaultRegistry.GetExtensionsForParser(lang) {
			if !seen[ext] {
				seen[ext] = true
				extensions = append(extensions, ext)
			}
		}
	}

	return extensions
}

// Config holds configuration for the ast-indexer processor component
type Config struct {
	Ports *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`

	// WatchPaths defines multiple paths to watch with per-path configuration.
	// Takes precedence over legacy single-path fields (RepoPath, Org, Project, Languages, ExcludePatterns).
	WatchPaths []WatchPathConfig `json:"watch_paths" schema:"type:array,description:Watch paths with per-path configuration,category:basic"`

	// Legacy single-path configuration (deprecated, use WatchPaths instead)
	RepoPath        string   `json:"repo_path"        schema:"type:string,description:Repository path to index (deprecated: use watch_paths),category:basic,default:."`
	Org             string   `json:"org"              schema:"type:string,description:Organization for entity IDs (deprecated: use watch_paths),category:basic"`
	Project         string   `json:"project"          schema:"type:string,description:Project name for entity IDs (deprecated: use watch_paths),category:basic"`
	Languages       []string `json:"languages"        schema:"type:array,description:Languages to index (deprecated: use watch_paths),category:basic,default:[go]"`
	ExcludePatterns []string `json:"exclude_patterns" schema:"type:array,description:Directory patterns to exclude (deprecated: use watch_paths),category:advanced"`

	// Global settings
	WatchEnabled  bool   `json:"watch_enabled"  schema:"type:bool,description:Enable file watcher for real-time updates,category:basic,default:true"`
	IndexInterval string `json:"index_interval" schema:"type:string,description:Full reindex interval (e.g. 5m),category:advanced,default:5m"`
	StreamName    string `json:"stream_name"    schema:"type:string,description:JetStream stream name,category:advanced,default:AGENT"`
}

// Validate checks the configuration for errors
func (c *Config) Validate() error {
	// If WatchPaths is configured, validate each path
	if len(c.WatchPaths) > 0 {
		for i, wp := range c.WatchPaths {
			if err := wp.Validate(); err != nil {
				return fmt.Errorf("watch_paths[%d]: %w", i, err)
			}
		}
	} else {
		// Legacy mode: validate single-path fields
		if c.RepoPath == "" {
			return fmt.Errorf("repo_path is required (or use watch_paths)")
		}
		if c.Org == "" {
			return fmt.Errorf("org is required (or use watch_paths)")
		}
		if c.Project == "" {
			return fmt.Errorf("project is required (or use watch_paths)")
		}
	}

	// Validate index interval if provided
	if c.IndexInterval != "" {
		d, err := time.ParseDuration(c.IndexInterval)
		if err != nil {
			return fmt.Errorf("invalid index_interval format: %w", err)
		}
		if d <= 0 {
			return fmt.Errorf("index_interval must be positive")
		}
	}

	return nil
}

// GetWatchPaths returns the effective watch paths.
// If WatchPaths is configured, returns it directly.
// Otherwise, converts legacy single-path config to WatchPathConfig.
func (c *Config) GetWatchPaths() []WatchPathConfig {
	if len(c.WatchPaths) > 0 {
		return c.WatchPaths
	}

	// Convert legacy config to WatchPathConfig
	languages := c.Languages
	if len(languages) == 0 {
		languages = []string{"go"}
	}

	return []WatchPathConfig{
		{
			Path:      c.RepoPath,
			Org:       c.Org,
			Project:   c.Project,
			Languages: languages,
			Excludes:  c.ExcludePatterns,
		},
	}
}

// DefaultConfig returns default configuration for ast-indexer processor
func DefaultConfig() Config {
	outputDefs := []component.PortDefinition{
		{
			Name:        "graph.ingest",
			Type:        "jetstream",
			Subject:     "graph.ingest.entity",
			StreamName:  "GRAPH",
			Required:    true,
			Description: "Entity state updates for graph ingestion",
		},
	}

	return Config{
		Ports: &component.PortConfig{
			Outputs: outputDefs,
		},
		RepoPath:        ".",
		Languages:       []string{"go"},
		WatchEnabled:    true,
		IndexInterval:   "5m",
		StreamName:      "GRAPH",
		ExcludePatterns: []string{"vendor", "node_modules", "dist", ".next", "build", "coverage"},
	}
}

// GetFileExtensions returns the file extensions to watch based on configured languages.
// This is kept for backward compatibility but GetWatchPaths should be preferred.
func (c *Config) GetFileExtensions() []string {
	var extensions []string
	seen := make(map[string]bool)

	for _, lang := range c.Languages {
		var langExts []string
		switch lang {
		case "go":
			langExts = []string{".go"}
		case "typescript":
			langExts = []string{".ts", ".tsx", ".mts", ".cts"}
		case "javascript":
			langExts = []string{".js", ".jsx", ".mjs", ".cjs"}
		}
		for _, ext := range langExts {
			if !seen[ext] {
				seen[ext] = true
				extensions = append(extensions, ext)
			}
		}
	}

	if len(extensions) == 0 {
		extensions = []string{".go"}
	}

	return extensions
}
