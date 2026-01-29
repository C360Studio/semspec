package astindexer

import (
	"fmt"
	"time"

	"github.com/c360/semstreams/component"
)

// Config holds configuration for the ast-indexer processor component
type Config struct {
	Ports         *component.PortConfig `json:"ports"          schema:"type:ports,description:Port configuration,category:basic"`
	RepoPath      string                `json:"repo_path"      schema:"type:string,description:Repository path to index,category:basic,default:."`
	Org           string                `json:"org"            schema:"type:string,description:Organization for entity IDs,category:basic"`
	Project       string                `json:"project"        schema:"type:string,description:Project name for entity IDs,category:basic"`
	WatchEnabled  bool                  `json:"watch_enabled"  schema:"type:bool,description:Enable file watcher for real-time updates,category:basic,default:true"`
	IndexInterval string                `json:"index_interval" schema:"type:string,description:Full reindex interval (e.g. 5m),category:advanced,default:5m"`
	StreamName    string                `json:"stream_name"    schema:"type:string,description:JetStream stream name,category:advanced,default:AGENT"`
}

// Validate checks the configuration for errors
func (c *Config) Validate() error {
	if c.RepoPath == "" {
		return fmt.Errorf("repo_path is required")
	}

	if c.Org == "" {
		return fmt.Errorf("org is required")
	}

	if c.Project == "" {
		return fmt.Errorf("project is required")
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

// DefaultConfig returns default configuration for ast-indexer processor
func DefaultConfig() Config {
	outputDefs := []component.PortDefinition{
		{
			Name:        "graph.ingest",
			Type:        "jetstream",
			Subject:     "graph.ingest.entity",
			StreamName:  "AGENT",
			Required:    true,
			Description: "Entity state updates for graph ingestion",
		},
	}

	return Config{
		Ports: &component.PortConfig{
			Outputs: outputDefs,
		},
		RepoPath:      ".",
		WatchEnabled:  true,
		IndexInterval: "5m",
		StreamName:    "AGENT",
	}
}
