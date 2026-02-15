package webingester

import (
	"fmt"
	"time"

	"github.com/c360studio/semstreams/component"
)

// Config holds configuration for the web-ingester processor component.
type Config struct {
	Ports *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`

	// StreamName is the JetStream stream for web source ingestion messages.
	StreamName string `json:"stream_name" schema:"type:string,description:JetStream stream name,category:basic,default:SOURCES"`

	// ConsumerName is the durable consumer name.
	ConsumerName string `json:"consumer_name" schema:"type:string,description:Durable consumer name,category:basic,default:web-ingester"`

	// FetchTimeout is the maximum time for fetching a web page.
	FetchTimeout string `json:"fetch_timeout" schema:"type:string,description:HTTP fetch timeout,category:advanced,default:30s"`

	// MaxContentSize is the maximum response body size in bytes.
	MaxContentSize int64 `json:"max_content_size" schema:"type:int,description:Maximum content size in bytes,category:advanced,default:10485760"`

	// UserAgent is the User-Agent header for HTTP requests.
	UserAgent string `json:"user_agent" schema:"type:string,description:HTTP User-Agent header,category:advanced,default:semspec-web-ingester/1.0"`

	// ChunkConfig holds document chunking configuration.
	ChunkConfig ChunkConfig `json:"chunk_config" schema:"type:object,description:Content chunking configuration,category:advanced"`

	// RefreshCheckInterval is how often to check for stale sources that need refresh.
	RefreshCheckInterval string `json:"refresh_check_interval" schema:"type:string,description:Interval for checking stale sources,category:advanced,default:5m"`

	// AnalysisEnabled enables LLM metadata extraction for web sources.
	// When enabled, web sources get scope, category, domain, keywords classification.
	AnalysisEnabled bool `json:"analysis_enabled" schema:"type:bool,description:Enable LLM metadata extraction,category:advanced,default:true"`

	// AnalysisTimeout is the maximum time for LLM analysis per web page.
	AnalysisTimeout string `json:"analysis_timeout" schema:"type:string,description:LLM analysis timeout,category:advanced,default:30s"`
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
	if c.FetchTimeout != "" {
		if _, err := time.ParseDuration(c.FetchTimeout); err != nil {
			return fmt.Errorf("invalid fetch_timeout format: %w", err)
		}
	}
	if c.RefreshCheckInterval != "" {
		if _, err := time.ParseDuration(c.RefreshCheckInterval); err != nil {
			return fmt.Errorf("invalid refresh_check_interval format: %w", err)
		}
	}
	if c.MaxContentSize < 0 {
		return fmt.Errorf("max_content_size must be non-negative")
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

// parseDurationOrDefault parses a duration string and returns the default if empty or invalid.
func parseDurationOrDefault(s string, defaultVal time.Duration) time.Duration {
	if s == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return defaultVal
	}
	return d
}

// GetFetchTimeout returns the fetch timeout as a duration.
func (c *Config) GetFetchTimeout() time.Duration {
	return parseDurationOrDefault(c.FetchTimeout, 30*time.Second)
}

// GetRefreshCheckInterval returns the refresh check interval as a duration.
func (c *Config) GetRefreshCheckInterval() time.Duration {
	return parseDurationOrDefault(c.RefreshCheckInterval, 5*time.Minute)
}

// GetMaxContentSize returns the max content size with default.
func (c *Config) GetMaxContentSize() int64 {
	if c.MaxContentSize <= 0 {
		return 10 * 1024 * 1024 // 10MB default
	}
	return c.MaxContentSize
}

// GetUserAgent returns the user agent with default.
func (c *Config) GetUserAgent() string {
	if c.UserAgent == "" {
		return "semspec-web-ingester/1.0"
	}
	return c.UserAgent
}

// GetAnalysisTimeout returns the analysis timeout as a duration.
func (c *Config) GetAnalysisTimeout() time.Duration {
	return parseDurationOrDefault(c.AnalysisTimeout, 30*time.Second)
}

// DefaultConfig returns default configuration for web-ingester processor.
func DefaultConfig() Config {
	inputDefs := []component.PortDefinition{
		{
			Name:        "web.in",
			Type:        "jetstream",
			Subject:     "source.web.ingest.>",
			StreamName:  "SOURCES",
			Required:    true,
			Description: "Web source ingestion requests",
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
		StreamName:           "SOURCES",
		ConsumerName:         "web-ingester",
		FetchTimeout:         "30s",
		MaxContentSize:       10 * 1024 * 1024, // 10MB
		UserAgent:            "semspec-web-ingester/1.0",
		RefreshCheckInterval: "5m",
		AnalysisEnabled:      true,
		AnalysisTimeout:      "30s",
		ChunkConfig: ChunkConfig{
			TargetTokens: 1000,
			MaxTokens:    1500,
			MinTokens:    200,
		},
	}
}
