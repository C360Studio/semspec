package questionanswerer

import (
	"fmt"
	"reflect"
	"time"

	"github.com/c360studio/semstreams/component"
)

// answererSchema defines the configuration schema.
var answererSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the question answerer component.
type Config struct {
	// StreamName is the JetStream stream for consuming tasks and publishing answers.
	StreamName string `json:"stream_name" schema:"type:string,description:JetStream stream for tasks and answers,category:basic,default:AGENT"`

	// ConsumerName is the durable consumer name for task consumption.
	ConsumerName string `json:"consumer_name" schema:"type:string,description:Durable consumer name for task consumption,category:basic,default:question-answerer"`

	// TaskSubject is the subject pattern for question-answering tasks.
	TaskSubject string `json:"task_subject" schema:"type:string,description:Subject pattern for question-answering tasks,category:basic,default:agent.task.question-answerer"`

	// DefaultCapability is the model capability to use if not specified in the task.
	DefaultCapability string `json:"default_capability" schema:"type:string,description:Default model capability if not specified,category:basic,default:planning"`

	// ContextSubjectPrefix is the subject prefix for context build requests.
	ContextSubjectPrefix string `json:"context_subject_prefix" schema:"type:string,description:Subject prefix for context build requests,category:advanced,default:context.build"`

	// ContextResponseBucket is the KV bucket for context responses.
	ContextResponseBucket string `json:"context_response_bucket" schema:"type:string,description:KV bucket for context responses,category:advanced,default:CONTEXT_RESPONSES"`

	// ContextTimeout is the timeout for context building.
	ContextTimeout string `json:"context_timeout" schema:"type:string,description:Timeout for context building,category:advanced,default:30s"`

	// Ports contains input/output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Input/output port definitions,category:basic"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		StreamName:            "AGENT",
		ConsumerName:          "question-answerer",
		TaskSubject:           "agent.task.question-answerer",
		DefaultCapability:     "planning",
		ContextSubjectPrefix:  "context.build",
		ContextResponseBucket: "CONTEXT_RESPONSES",
		ContextTimeout:        "30s",
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

// GetContextTimeout parses the context timeout duration.
func (c *Config) GetContextTimeout() time.Duration {
	if c.ContextTimeout == "" {
		return 30 * time.Second
	}
	d, err := time.ParseDuration(c.ContextTimeout)
	if err != nil {
		return 30 * time.Second
	}
	return d
}
