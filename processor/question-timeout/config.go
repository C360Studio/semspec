package questiontimeout

import (
	"fmt"
	"reflect"
	"time"

	"github.com/c360studio/semstreams/component"
)

// timeoutSchema defines the configuration schema.
var timeoutSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the question timeout component.
type Config struct {
	// CheckInterval is how often to check for timed-out questions.
	CheckInterval time.Duration `json:"check_interval"`

	// DefaultSLA is the default SLA if not specified by the route.
	DefaultSLA time.Duration `json:"default_sla"`

	// AnswererConfigPath is the path to answerers.yaml configuration.
	AnswererConfigPath string `json:"answerer_config_path,omitempty"`

	// Ports contains input/output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		CheckInterval: 1 * time.Minute,
		DefaultSLA:    24 * time.Hour,
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "question-events",
					Type:        "kv-watch",
					Subject:     "QUESTIONS",
					Description: "Watch for question changes in KV bucket",
					Required:    true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "timeout-events",
					Type:        "jetstream",
					Subject:     "question.timeout.>",
					StreamName:  "AGENT",
					Description: "Publish timeout events",
					Required:    true,
				},
				{
					Name:        "escalation-events",
					Type:        "jetstream",
					Subject:     "question.escalate.>",
					StreamName:  "AGENT",
					Description: "Publish escalation events",
					Required:    true,
				},
			},
		},
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.CheckInterval <= 0 {
		return fmt.Errorf("check_interval must be positive")
	}
	if c.DefaultSLA <= 0 {
		return fmt.Errorf("default_sla must be positive")
	}
	return nil
}
