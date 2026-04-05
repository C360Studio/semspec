package githubsubmitter

import (
	"fmt"
	"reflect"
	"time"

	"github.com/c360studio/semstreams/component"
)

var githubSubmitterSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the github-submitter component.
type Config struct {
	// GitHubToken is the GitHub personal access token or app token.
	GitHubToken string `json:"github_token" schema:"type:string,description:GitHub API token (env var expansion supported),category:basic"`

	// Repository is the GitHub repository in owner/repo format.
	Repository string `json:"repository" schema:"type:string,description:GitHub repository (owner/repo),category:basic"`

	// RemoteName is the git remote to push branches to.
	RemoteName string `json:"remote_name" schema:"type:string,description:Git remote for branch push,category:basic,default:origin"`

	// BranchPrefix is the prefix for plan-level branches.
	BranchPrefix string `json:"branch_prefix" schema:"type:string,description:Branch name prefix,category:basic,default:semspec/"`

	// DraftPR when true creates draft pull requests.
	DraftPR bool `json:"draft_pr" schema:"type:bool,description:Create draft PRs,category:basic,default:true"`

	// CommentOnTransitions when true posts comments on the source issue at plan milestones.
	CommentOnTransitions bool `json:"comment_on_transitions" schema:"type:bool,description:Post issue comments at milestones,category:advanced,default:true"`

	// ReviewPollInterval is how often to poll PR reviews.
	ReviewPollInterval string `json:"review_poll_interval" schema:"type:string,description:Poll interval for PR reviews,category:advanced,default:30s"`

	// MaxPRRevisions is the maximum number of PR feedback rounds before stopping.
	MaxPRRevisions int `json:"max_pr_revisions" schema:"type:int,description:Max PR feedback revision rounds,category:advanced,default:3"`

	// AutoAcceptFeedback when true auto-accepts PR feedback and re-executes.
	// When false, creates ChangeProposals in proposed status requiring manual accept.
	AutoAcceptFeedback *bool `json:"auto_accept_feedback" schema:"type:bool,description:Auto-accept PR feedback and re-execute,category:advanced,default:true"`

	// PlanStateBucket is the KV bucket for plan state.
	PlanStateBucket string `json:"plan_state_bucket" schema:"type:string,description:KV bucket for plan state,category:advanced,default:PLAN_STATES"`

	// StreamName is the JetStream stream for publishing events.
	StreamName string `json:"stream_name" schema:"type:string,description:JetStream stream for events,category:advanced,default:WORKFLOW"`
}

// GetReviewPollInterval returns the parsed review poll interval.
func (c *Config) GetReviewPollInterval() time.Duration {
	d, err := time.ParseDuration(c.ReviewPollInterval)
	if err != nil {
		return 30 * time.Second
	}
	return d
}

// IsAutoAcceptFeedback returns whether PR feedback should be auto-accepted.
func (c *Config) IsAutoAcceptFeedback() bool {
	if c.AutoAcceptFeedback == nil {
		return true
	}
	return *c.AutoAcceptFeedback
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	defaultTrue := true
	return Config{
		RemoteName:           "origin",
		BranchPrefix:         "semspec/",
		DraftPR:              true,
		CommentOnTransitions: true,
		ReviewPollInterval:   "30s",
		MaxPRRevisions:       3,
		AutoAcceptFeedback:   &defaultTrue,
		PlanStateBucket:      "PLAN_STATES",
		StreamName:           "WORKFLOW",
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.GitHubToken == "" {
		return fmt.Errorf("github_token is required")
	}
	if c.Repository == "" {
		return fmt.Errorf("repository is required")
	}
	return nil
}
