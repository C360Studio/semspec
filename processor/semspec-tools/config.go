package semspectools

import (
	"fmt"
	"time"

	"github.com/c360/semstreams/component"
)

// Config holds configuration for the semspec-tools processor component
type Config struct {
	Ports              *component.PortConfig `json:"ports"                schema:"type:ports,description:Port configuration,category:basic"`
	RepoPath           string                `json:"repo_path"            schema:"type:string,description:Repository path for file and git operations,category:basic,default:."`
	StreamName         string                `json:"stream_name"          schema:"type:string,description:JetStream stream name,category:basic,default:AGENT"`
	Timeout            string                `json:"timeout"              schema:"type:string,description:Tool execution timeout,category:advanced,default:30s"`
	ConsumerNameSuffix string                `json:"consumer_name_suffix" schema:"type:string,description:Suffix appended to consumer names for uniqueness,category:advanced"`
	HeartbeatInterval  string                `json:"heartbeat_interval"   schema:"type:string,description:Heartbeat interval for tool registration,category:advanced,default:10s"`
}

// Validate checks the configuration for errors
func (c *Config) Validate() error {
	if c.RepoPath == "" {
		return fmt.Errorf("repo_path is required")
	}

	// Validate timeout
	if c.Timeout != "" {
		d, err := time.ParseDuration(c.Timeout)
		if err != nil {
			return fmt.Errorf("invalid timeout format: %w", err)
		}
		if d <= 0 {
			return fmt.Errorf("timeout must be positive")
		}
	}

	// Validate heartbeat interval
	if c.HeartbeatInterval != "" {
		d, err := time.ParseDuration(c.HeartbeatInterval)
		if err != nil {
			return fmt.Errorf("invalid heartbeat_interval format: %w", err)
		}
		if d <= 0 {
			return fmt.Errorf("heartbeat_interval must be positive")
		}
	}

	return nil
}

// DefaultConfig returns default configuration for semspec-tools processor
func DefaultConfig() Config {
	inputDefs := []component.PortDefinition{
		{
			Name:        "tool.execute.file",
			Type:        "jetstream",
			Subject:     "tool.execute.file_*",
			StreamName:  "AGENT",
			Required:    true,
			Description: "File tool execution requests",
		},
		{
			Name:        "tool.execute.git",
			Type:        "jetstream",
			Subject:     "tool.execute.git_*",
			StreamName:  "AGENT",
			Required:    true,
			Description: "Git tool execution requests",
		},
	}

	outputDefs := []component.PortDefinition{
		{
			Name:        "tool.result",
			Type:        "jetstream",
			Subject:     "tool.result.*",
			StreamName:  "AGENT",
			Required:    true,
			Description: "Tool execution results",
		},
	}

	return Config{
		Ports: &component.PortConfig{
			Inputs:  inputDefs,
			Outputs: outputDefs,
		},
		RepoPath:          ".",
		StreamName:        "AGENT",
		Timeout:           "30s",
		HeartbeatInterval: "10s",
	}
}
