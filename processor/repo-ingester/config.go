package repoingester

import (
	"fmt"
	"time"

	"github.com/c360studio/semstreams/component"
)

// Config holds configuration for the repo-ingester processor component.
type Config struct {
	Ports *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`

	// StreamName is the JetStream stream for repository ingestion messages.
	StreamName string `json:"stream_name" schema:"type:string,description:JetStream stream name,category:basic,default:SOURCES"`

	// ConsumerName is the durable consumer name.
	ConsumerName string `json:"consumer_name" schema:"type:string,description:Durable consumer name,category:basic,default:repo-ingester"`

	// ReposDir is the base directory for cloned repositories.
	ReposDir string `json:"repos_dir" schema:"type:string,description:Base directory for cloned repositories,category:basic,default:.semspec/sources/repos"`

	// CloneTimeout is the maximum time for cloning a repository.
	CloneTimeout string `json:"clone_timeout" schema:"type:string,description:Repository clone timeout,category:advanced,default:5m"`

	// CloneDepth sets the depth for shallow clones (0 = full clone).
	CloneDepth int `json:"clone_depth" schema:"type:int,description:Shallow clone depth (0 for full),category:advanced,default:0"`

	// IndexTimeout is the maximum time for indexing a repository.
	IndexTimeout string `json:"index_timeout" schema:"type:string,description:Repository indexing timeout,category:advanced,default:10m"`

	// ASTIndexerSubject is the subject to trigger AST indexing.
	ASTIndexerSubject string `json:"ast_indexer_subject" schema:"type:string,description:Subject to trigger AST indexer,category:advanced,default:ast.index.request"`
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	if c.StreamName == "" {
		return fmt.Errorf("stream_name is required")
	}
	if c.ConsumerName == "" {
		return fmt.Errorf("consumer_name is required")
	}
	if c.ReposDir == "" {
		return fmt.Errorf("repos_dir is required")
	}
	if c.CloneTimeout != "" {
		if _, err := time.ParseDuration(c.CloneTimeout); err != nil {
			return fmt.Errorf("invalid clone_timeout format: %w", err)
		}
	}
	if c.IndexTimeout != "" {
		if _, err := time.ParseDuration(c.IndexTimeout); err != nil {
			return fmt.Errorf("invalid index_timeout format: %w", err)
		}
	}
	return nil
}

// GetCloneTimeout returns the clone timeout as a duration.
func (c *Config) GetCloneTimeout() time.Duration {
	if c.CloneTimeout == "" {
		return 5 * time.Minute
	}
	d, err := time.ParseDuration(c.CloneTimeout)
	if err != nil {
		return 5 * time.Minute
	}
	return d
}

// GetIndexTimeout returns the index timeout as a duration.
func (c *Config) GetIndexTimeout() time.Duration {
	if c.IndexTimeout == "" {
		return 10 * time.Minute
	}
	d, err := time.ParseDuration(c.IndexTimeout)
	if err != nil {
		return 10 * time.Minute
	}
	return d
}

// DefaultConfig returns default configuration for repo-ingester processor.
func DefaultConfig() Config {
	inputDefs := []component.PortDefinition{
		{
			Name:        "repos.in",
			Type:        "jetstream",
			Subject:     "source.repo.>",
			StreamName:  "SOURCES",
			Required:    true,
			Description: "Repository ingestion and management requests",
		},
	}

	outputDefs := []component.PortDefinition{
		{
			Name:        "graph.out",
			Type:        "jetstream",
			Subject:     "graph.ingest.entity",
			StreamName:  "GRAPH",
			Required:    true,
			Description: "Entity state updates for graph ingestion",
		},
	}

	return Config{
		Ports: &component.PortConfig{
			Inputs:  inputDefs,
			Outputs: outputDefs,
		},
		StreamName:        "SOURCES",
		ConsumerName:      "repo-ingester",
		ReposDir:          ".semspec/sources/repos",
		CloneTimeout:      "5m",
		CloneDepth:        0,
		IndexTimeout:      "10m",
		ASTIndexerSubject: "ast.index.request",
	}
}
