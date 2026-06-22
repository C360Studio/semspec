package planreviewer

import (
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// planReviewerSchema defines the configuration schema.
var planReviewerSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the plan reviewer component.
type Config struct {
	// DefaultCapability is the model capability to use for plan review.
	DefaultCapability string `json:"default_capability" schema:"type:string,description:Default model capability for plan review,category:basic,default:plan_review"`

	// AutoApprove controls whether the reviewer automatically sends the
	// approved mutation after a successful round-1 review. When false, the
	// plan stays at StatusReviewed and waits for human approval via the
	// /promote endpoint. Default true preserves backward compatibility.
	AutoApprove *bool `json:"auto_approve" schema:"type:bool,description:Skip human approval gate after review,category:basic,default:true"`

	// PlanStateBucket is the KV bucket name to watch for plan state transitions
	// (KV twofer). The plan-reviewer self-triggers when a plan reaches "drafted"
	// (round 1) or "scenarios_generated" (round 2).
	PlanStateBucket string `json:"plan_state_bucket" schema:"type:string,description:KV bucket to watch for plan state transitions,category:advanced,default:PLAN_STATES"`

	// MaxReviewRetries is the maximum number of times to retry a review when
	// the agent loop fails or the output cannot be parsed. Default 2.
	MaxReviewRetries int `json:"max_review_retries" schema:"type:integer,description:Max retries on review failure (parse error or loop failure),category:advanced,default:2"`

	// RetryBackoffMs is the floor of the jittered delay before re-dispatching
	// after a review failure. See workflow/dispatchretry for semantics.
	// Default 200ms; non-positive values fall back to the default.
	RetryBackoffMs int `json:"retry_backoff_ms" schema:"type:integer,description:Floor of jittered backoff between review retries (ms),category:advanced,default:200"`

	// SandboxURL is the URL of the sandbox container. When set, the reviewer
	// fetches a `git ls-files` snapshot of the project at dispatch time and
	// injects it into the user prompt as ground-truth file inventory. Without
	// this the reviewer's R1 scope-validity criterion ("compare scope.include
	// against the project file tree") fires against a tree it never received
	// and weak models default to flagging real files as hallucinated. Caught
	// 2026-05-08 take 20 on openrouter llama-3.3-70b @easy. Greenfield-safe:
	// empty output skips the section silently.
	SandboxURL string `json:"sandbox_url,omitempty" schema:"type:string,description:Sandbox URL for project file tree snapshot,category:advanced"`

	// ArchitectureReviewEnabled gates the ADR-051 Slice 3 adversarial
	// architecture review round (R-arch). When true the plan-reviewer claims
	// architecture_generated → reviewing_architecture, runs a deterministic
	// preflight + an architecture-shaped LLM review, then advances to
	// architecture_reviewed (approve) or back to requirements_generated
	// (reject — re-run the architect). When false (default) no component claims
	// reviewing_architecture and architecture_generated → preparing_stories
	// directly, exactly as before this slice.
	//
	// CROSS-COMPONENT INVARIANT: story-preparer carries the SAME flag and MUST
	// agree with this one. Source both from the one ARCHITECTURE_REVIEW_ENABLED
	// env var in semspec.json. If they disagree the architecture phase wedges:
	// reviewer-on/preparer-off leaves architecture_reviewed unclaimed, and
	// reviewer-off/preparer-on leaves architecture_generated unclaimed.
	ArchitectureReviewEnabled bool `json:"architecture_review_enabled" schema:"type:bool,description:Enable the ADR-051 adversarial architecture review round (must match story-preparer),category:advanced,default:false"`

	// RequirementsReviewEnabled gates the ADR-051 Slice 4 adversarial
	// requirements review round (R-req). When true the plan-reviewer claims
	// requirements_generated → reviewing_requirements, runs a deterministic
	// preflight + a requirements-shaped LLM review, then advances to
	// requirements_reviewed (approve) or back to approved (reject — re-run the
	// requirement-generator). When false (default) no component claims
	// reviewing_requirements and requirements_generated → generating_architecture
	// directly, exactly as before this slice.
	//
	// CROSS-COMPONENT INVARIANT: architecture-generator carries the SAME flag and
	// MUST agree with this one. Source both from the one
	// REQUIREMENTS_REVIEW_ENABLED env var in semspec.json. A mismatch wedges the
	// requirements phase (one of requirements_generated / requirements_reviewed
	// ends up with no claimant).
	RequirementsReviewEnabled bool `json:"requirements_review_enabled" schema:"type:bool,description:Enable the ADR-051 adversarial requirements review round (must match architecture-generator),category:advanced,default:false"`

	// Ports contains input/output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Input/output port definitions,category:basic"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		DefaultCapability: "plan_review",
		PlanStateBucket:   "PLAN_STATES",
		MaxReviewRetries:  2,
		RetryBackoffMs:    200,
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{},
			Outputs: []component.PortDefinition{
				{
					Name:        "review-results",
					Type:        "nats",
					Subject:     "workflow.result.plan-reviewer.>",
					Description: "Publish plan review results",
					Required:    false,
				},
			},
		},
	}
}

// IsAutoApprove returns true when the reviewer should automatically approve
// plans after a successful review. Defaults to true when not set.
func (c *Config) IsAutoApprove() bool {
	if c.AutoApprove == nil {
		return true
	}
	return *c.AutoApprove
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	return nil
}
