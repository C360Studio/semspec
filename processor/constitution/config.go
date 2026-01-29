package constitution

import (
	"fmt"

	"github.com/c360/semstreams/component"
)

// Config holds configuration for the constitution processor component
type Config struct {
	Ports       *component.PortConfig `json:"ports"        schema:"type:ports,description:Port configuration,category:basic"`
	Project     string                `json:"project"      schema:"type:string,description:Project name for constitution,category:basic,required:true"`
	Org         string                `json:"org"          schema:"type:string,description:Organization for entity IDs,category:basic,required:true"`
	FilePath    string                `json:"file_path"    schema:"type:string,description:Path to constitution YAML/JSON file,category:basic"`
	AutoReload  bool                  `json:"auto_reload"  schema:"type:bool,description:Watch file for changes,category:advanced,default:true"`
	EnforceMode string                `json:"enforce_mode" schema:"type:string,description:Enforcement mode (strict|warn|off),category:advanced,default:warn"`
	StreamName  string                `json:"stream_name"  schema:"type:string,description:JetStream stream name,category:advanced,default:AGENT"`
}

// Validate checks the configuration for errors
func (c *Config) Validate() error {
	if c.Project == "" {
		return fmt.Errorf("project is required")
	}

	if c.Org == "" {
		return fmt.Errorf("org is required")
	}

	switch c.EnforceMode {
	case "", "strict", "warn", "off":
		// valid
	default:
		return fmt.Errorf("enforce_mode must be one of: strict, warn, off")
	}

	return nil
}

// DefaultConfig returns default configuration for constitution processor
func DefaultConfig() Config {
	outputDefs := []component.PortDefinition{
		{
			Name:        "graph.ingest",
			Type:        "jetstream",
			Subject:     "graph.ingest.entity",
			StreamName:  "AGENT",
			Required:    true,
			Description: "Constitution entity updates for graph storage",
		},
	}

	inputDefs := []component.PortDefinition{
		{
			Name:        "check.request",
			Type:        "jetstream",
			Subject:     "constitution.check.request",
			StreamName:  "AGENT",
			Required:    false,
			Description: "Incoming requests to check content against constitution",
		},
	}

	return Config{
		Ports: &component.PortConfig{
			Inputs:  inputDefs,
			Outputs: outputDefs,
		},
		AutoReload:  true,
		EnforceMode: "warn",
		StreamName:  "AGENT",
	}
}
