package sourceingester

import (
	"fmt"
	"time"

	"github.com/c360studio/semstreams/component"
)

// Config holds configuration for the source-ingester processor component.
type Config struct {
	Ports *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`

	// StreamName is the JetStream stream for source ingestion messages.
	StreamName string `json:"stream_name" schema:"type:string,description:JetStream stream name,category:basic,default:SOURCES"`

	// ConsumerName is the durable consumer name.
	ConsumerName string `json:"consumer_name" schema:"type:string,description:Durable consumer name,category:basic,default:source-ingester"`

	// AnalysisTimeout is the maximum time for LLM document analysis.
	AnalysisTimeout string `json:"analysis_timeout" schema:"type:string,description:LLM analysis timeout,category:advanced,default:30s"`

	// SourcesDir is the base directory for document sources.
	SourcesDir string `json:"sources_dir" schema:"type:string,description:Base directory for document sources,category:basic,default:.semspec/sources/docs"`

	// ChunkConfig holds document chunking configuration.
	ChunkConfig ChunkConfig `json:"chunk_config" schema:"type:object,description:Document chunking configuration,category:advanced"`

	// WatchConfig holds file watching configuration.
	WatchConfig WatchConfig `json:"watch_config" schema:"type:object,description:File watching configuration for automatic document indexing,category:advanced"`
}

// ChunkConfig holds chunking-related configuration.
type ChunkConfig struct {
	// TargetTokens is the ideal chunk size in tokens.
	TargetTokens int `json:"target_tokens" schema:"type:int,description:Target chunk size in tokens,default:1000"`

	// MaxTokens is the maximum chunk size.
	MaxTokens int `json:"max_tokens" schema:"type:int,description:Maximum chunk size in tokens,default:1500"`

	// MinTokens is the minimum chunk size (smaller chunks are merged).
	MinTokens int `json:"min_tokens" schema:"type:int,description:Minimum chunk size in tokens,default:200"`
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	if c.StreamName == "" {
		return fmt.Errorf("stream_name is required")
	}
	if c.ConsumerName == "" {
		return fmt.Errorf("consumer_name is required")
	}
	if c.SourcesDir == "" {
		return fmt.Errorf("sources_dir is required")
	}
	if c.AnalysisTimeout != "" {
		if _, err := time.ParseDuration(c.AnalysisTimeout); err != nil {
			return fmt.Errorf("invalid analysis_timeout format: %w", err)
		}
	}
	// Validate chunk config if non-default values are provided
	if c.ChunkConfig.TargetTokens > 0 || c.ChunkConfig.MaxTokens > 0 || c.ChunkConfig.MinTokens > 0 {
		if c.ChunkConfig.MinTokens > 0 && c.ChunkConfig.TargetTokens > 0 &&
			c.ChunkConfig.MinTokens >= c.ChunkConfig.TargetTokens {
			return fmt.Errorf("chunk_config: min_tokens (%d) must be less than target_tokens (%d)",
				c.ChunkConfig.MinTokens, c.ChunkConfig.TargetTokens)
		}
		if c.ChunkConfig.TargetTokens > 0 && c.ChunkConfig.MaxTokens > 0 &&
			c.ChunkConfig.TargetTokens > c.ChunkConfig.MaxTokens {
			return fmt.Errorf("chunk_config: target_tokens (%d) must not exceed max_tokens (%d)",
				c.ChunkConfig.TargetTokens, c.ChunkConfig.MaxTokens)
		}
	}
	return nil
}

// GetAnalysisTimeout returns the analysis timeout as a duration.
func (c *Config) GetAnalysisTimeout() time.Duration {
	if c.AnalysisTimeout == "" {
		return 30 * time.Second
	}
	d, err := time.ParseDuration(c.AnalysisTimeout)
	if err != nil {
		return 30 * time.Second
	}
	return d
}

// DefaultConfig returns default configuration for source-ingester processor.
func DefaultConfig() Config {
	inputDefs := []component.PortDefinition{
		{
			Name:        "sources.in",
			Type:        "jetstream",
			Subject:     "source.ingest.>",
			StreamName:  "SOURCES",
			Required:    true,
			Description: "Document and repository ingestion requests",
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
		StreamName:      "SOURCES",
		ConsumerName:    "source-ingester",
		AnalysisTimeout: "30s",
		SourcesDir:      ".semspec/sources/docs",
		ChunkConfig: ChunkConfig{
			TargetTokens: 1000,
			MaxTokens:    1500,
			MinTokens:    200,
		},
	}
}
