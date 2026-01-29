package query

import (
	"fmt"

	"github.com/c360/semstreams/component"
)

// Config holds configuration for the query processor component
type Config struct {
	Ports      *component.PortConfig `json:"ports"       schema:"type:ports,description:Port configuration,category:basic"`
	Project    string                `json:"project"     schema:"type:string,description:Project to query,category:basic"`
	Org        string                `json:"org"         schema:"type:string,description:Organization for entity IDs,category:basic"`
	MaxResults int                   `json:"max_results" schema:"type:int,description:Maximum results per query,category:advanced,default:100"`
	StreamName string                `json:"stream_name" schema:"type:string,description:JetStream stream name,category:advanced,default:AGENT"`
}

// Validate checks the configuration for errors
func (c *Config) Validate() error {
	if c.Org == "" {
		return fmt.Errorf("org is required")
	}

	if c.MaxResults < 0 {
		return fmt.Errorf("max_results must be non-negative")
	}

	return nil
}

// DefaultConfig returns default configuration for query processor
func DefaultConfig() Config {
	outputDefs := []component.PortDefinition{
		{
			Name:        "query.result",
			Type:        "jetstream",
			Subject:     "graph.query.result",
			StreamName:  "AGENT",
			Required:    true,
			Description: "Query results",
		},
	}

	inputDefs := []component.PortDefinition{
		{
			Name:        "query.request",
			Type:        "jetstream",
			Subject:     "graph.query.request",
			StreamName:  "AGENT",
			Required:    true,
			Description: "Incoming query requests",
		},
	}

	return Config{
		Ports: &component.PortConfig{
			Inputs:  inputDefs,
			Outputs: outputDefs,
		},
		MaxResults: 100,
		StreamName: "AGENT",
	}
}
