package questionanswerer

import (
	"fmt"
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// answererSchema defines the configuration schema.
var answererSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the question answerer component.
type Config struct {
	// StreamName is the JetStream stream for consuming tasks and publishing answers.
	StreamName string `json:"stream_name"`

	// ConsumerName is the durable consumer name for task consumption.
	ConsumerName string `json:"consumer_name"`

	// TaskSubject is the subject pattern for question-answering tasks.
	TaskSubject string `json:"task_subject"`

	// DefaultCapability is the model capability to use if not specified in the task.
	DefaultCapability string `json:"default_capability"`

	// Ports contains input/output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		StreamName:        "AGENT",
		ConsumerName:      "question-answerer",
		TaskSubject:       "agent.task.question-answerer",
		DefaultCapability: "planning",
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "question-tasks",
					Type:        "jetstream",
					Subject:     "agent.task.question-answerer",
					StreamName:  "AGENT",
					Description: "Receive question-answering tasks",
					Required:    true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "question-answers",
					Type:        "jetstream",
					Subject:     "question.answer.>",
					StreamName:  "AGENT",
					Description: "Publish question answers",
					Required:    true,
				},
			},
		},
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.StreamName == "" {
		return fmt.Errorf("stream_name is required")
	}
	if c.ConsumerName == "" {
		return fmt.Errorf("consumer_name is required")
	}
	if c.TaskSubject == "" {
		return fmt.Errorf("task_subject is required")
	}
	return nil
}
