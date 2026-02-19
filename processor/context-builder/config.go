package contextbuilder

import (
	"fmt"
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// contextBuilderSchema defines the configuration schema.
var contextBuilderSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the context builder processor component.
type Config struct {
	// StreamName is the JetStream stream for consuming requests and publishing results.
	StreamName string `json:"stream_name" schema:"type:string,description:JetStream stream for context requests,category:basic,default:AGENT"`

	// ConsumerName is the durable consumer name for request consumption.
	ConsumerName string `json:"consumer_name" schema:"type:string,description:Durable consumer name for context requests,category:basic,default:context-builder"`

	// InputSubjectPattern is the subject pattern for context build requests.
	InputSubjectPattern string `json:"input_subject_pattern" schema:"type:string,description:Subject pattern for context build requests,category:basic,default:context.build.>"`

	// OutputSubjectPrefix is the subject prefix for built context responses.
	OutputSubjectPrefix string `json:"output_subject_prefix" schema:"type:string,description:Subject prefix for context responses,category:basic,default:context.built"`

	// DefaultTokenBudget is the default budget when no model/capability specified.
	DefaultTokenBudget int `json:"default_token_budget" schema:"type:int,description:Default token budget when no model specified,category:advanced,default:32000,min:1000,max:200000"`

	// HeadroomTokens is the safety buffer subtracted from model context window.
	HeadroomTokens int `json:"headroom_tokens" schema:"type:int,description:Safety buffer tokens for model response,category:advanced,default:6400,min:1000,max:32000"`

	// GraphGatewayURL is the URL of the graph gateway for queries.
	GraphGatewayURL string `json:"graph_gateway_url" schema:"type:string,description:Graph gateway URL for entity queries,category:basic,default:http://localhost:8082"`

	// RepoPath is the path to the repository for git operations.
	RepoPath string `json:"repo_path" schema:"type:string,description:Repository path for git and file operations,category:basic"`

	// DefaultCapability is the default model capability for budget calculation.
	DefaultCapability string `json:"default_capability" schema:"type:string,description:Default model capability,category:basic,default:reviewing"`

	// SOPEntityPrefix is the entity ID prefix for finding SOP entities in the graph.
	SOPEntityPrefix string `json:"sop_entity_prefix" schema:"type:string,description:Entity ID prefix for SOP entities,category:advanced,default:c360.semspec.source.doc"`

	// Ports contains input/output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Input/output port definitions,category:basic"`

	// ResponseBucketName is the KV bucket name for storing context responses.
	ResponseBucketName string `json:"response_bucket_name" schema:"type:string,description:KV bucket for context responses,category:advanced,default:CONTEXT_RESPONSES"`

	// ResponseTTL is the TTL for context responses in the KV bucket (in hours).
	ResponseTTLHours int `json:"response_ttl_hours" schema:"type:int,description:TTL for context responses in hours,category:advanced,default:24,min:1,max:168"`

	// BlockingTimeoutSeconds is the maximum time to wait for Q&A answers (in seconds).
	BlockingTimeoutSeconds int `json:"blocking_timeout_seconds" schema:"type:int,description:Max time to wait for Q&A answers in seconds,category:advanced,default:300,min:30,max:3600"`

	// AllowBlocking enables blocking behavior when context is insufficient.
	AllowBlocking bool `json:"allow_blocking" schema:"type:bool,description:Enable blocking to wait for Q&A answers,category:advanced,default:true"`

	// AnswerersConfigPath is the path to the answerers.yaml configuration file.
	AnswerersConfigPath string `json:"answerers_config_path" schema:"type:string,description:Path to answerers.yaml for question routing,category:advanced,default:configs/answerers.yaml"`

	// GraphReadinessBudget is the maximum time to wait for the graph pipeline to become
	// ready on the first context build request. Uses time.ParseDuration format (e.g. "15s").
	// The probe exercises the full NATS request-reply path (not just HTTP).
	GraphReadinessBudget string `json:"graph_readiness_budget" schema:"type:string,description:Max time to wait for graph readiness on first request,category:advanced,default:15s"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		StreamName:             "AGENT",
		ConsumerName:           "context-builder",
		InputSubjectPattern:    "context.build.>",
		OutputSubjectPrefix:    "context.built",
		DefaultTokenBudget:     32000,
		HeadroomTokens:         6400,
		GraphGatewayURL:        "http://localhost:8082",
		DefaultCapability:      "reviewing",
		SOPEntityPrefix:        "c360.semspec.source.doc",
		ResponseBucketName:     "CONTEXT_RESPONSES",
		ResponseTTLHours:       24,
		BlockingTimeoutSeconds: 300,
		AllowBlocking:          true,
		AnswerersConfigPath:    "configs/answerers.yaml",
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "context-requests",
					Type:        "jetstream",
					Subject:     "context.build.>",
					StreamName:  "AGENT",
					Description: "Receive context build requests",
					Required:    true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "context-responses",
					Type:        "nats",
					Subject:     "context.built.>",
					Description: "Publish built context responses",
					Required:    false,
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
	if c.InputSubjectPattern == "" {
		return fmt.Errorf("input_subject_pattern is required")
	}
	if c.OutputSubjectPrefix == "" {
		return fmt.Errorf("output_subject_prefix is required")
	}
	if c.DefaultTokenBudget <= 0 {
		return fmt.Errorf("default_token_budget must be positive")
	}
	if c.HeadroomTokens < 0 {
		return fmt.Errorf("headroom_tokens cannot be negative")
	}
	if c.GraphGatewayURL == "" {
		return fmt.Errorf("graph_gateway_url is required")
	}
	return nil
}
