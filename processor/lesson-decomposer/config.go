package lessondecomposer

import (
	"fmt"
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// lessonDecomposerSchema defines the configuration schema.
var lessonDecomposerSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the lesson-decomposer component.
//
// Phase 2a: skeleton wiring only — receives requests and logs them. Phase 2b
// adds trajectory fetch, LLM dispatch, and lesson emission. The Enabled flag
// keeps the component switchable per-deployment so legacy keyword-classifier
// lessons (extractLessons in execution-manager) remain the default until the
// decomposer's quality is manually validated against real-LLM runs.
type Config struct {
	// Enabled controls whether the decomposer subscribes to incoming requests.
	// When false, the consumer is created but the handler is a no-op (so
	// triggering paths can publish freely without coupling to feature state).
	// Default true so that the wire is observable in mock e2e by default.
	Enabled bool `json:"enabled" schema:"type:boolean,description:Whether the decomposer processes requests; false acks-and-skips,category:basic,default:true"`

	// StreamName is the JetStream stream for consuming decompose requests.
	StreamName string `json:"stream_name" schema:"type:string,description:JetStream stream for decompose requests,category:basic,default:WORKFLOW"`

	// ConsumerName is the durable consumer name.
	ConsumerName string `json:"consumer_name" schema:"type:string,description:Durable consumer name,category:basic,default:lesson-decomposer"`

	// FilterSubject is the subject pattern this component subscribes to.
	FilterSubject string `json:"filter_subject" schema:"type:string,description:NATS subject pattern,category:basic,default:workflow.events.lesson.decompose.requested.>"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:       true,
		StreamName:    "WORKFLOW",
		ConsumerName:  "lesson-decomposer",
		FilterSubject: "workflow.events.lesson.decompose.requested.>",
	}
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	if c.StreamName == "" {
		return fmt.Errorf("stream_name is required")
	}
	if c.ConsumerName == "" {
		return fmt.Errorf("consumer_name is required")
	}
	if c.FilterSubject == "" {
		return fmt.Errorf("filter_subject is required")
	}
	return nil
}
