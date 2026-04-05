package githubwatcher

import (
	"fmt"
	"reflect"
	"time"

	"github.com/c360studio/semstreams/component"
)

var githubWatcherSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the github-watcher component.
type Config struct {
	// GitHubToken is the GitHub personal access token or app token.
	GitHubToken string `json:"github_token" schema:"type:string,description:GitHub API token (env var expansion supported),category:basic"`

	// Repository is the GitHub repository in owner/repo format.
	Repository string `json:"repository" schema:"type:string,description:GitHub repository (owner/repo),category:basic"`

	// PollInterval is how often to poll GitHub for new issues.
	PollInterval string `json:"poll_interval" schema:"type:string,description:Poll interval for GitHub issues,category:basic,default:60s"`

	// IssueLabel is the label required on issues to be processed.
	IssueLabel string `json:"issue_label" schema:"type:string,description:Required label on GitHub issues,category:basic,default:semspec"`

	// RequireLabel when true only processes issues with the configured label.
	RequireLabel *bool `json:"require_label" schema:"type:bool,description:Only process labeled issues,category:basic,default:true"`

	// AllowedContributors is a whitelist of GitHub usernames. Empty = allow all.
	AllowedContributors []string `json:"allowed_contributors" schema:"type:array,description:Allowed GitHub usernames (empty=all),category:advanced"`

	// RequireContributor when true only processes issues from whitelisted users.
	RequireContributor bool `json:"require_contributor" schema:"type:bool,description:Only process issues from allowed contributors,category:advanced,default:false"`

	// MaxBodySize is the maximum issue body size in bytes (anti-spam).
	MaxBodySize int `json:"max_body_size" schema:"type:int,description:Max issue body size in bytes,category:advanced,default:10000"`

	// MaxPlansPerHour is the rate limit for plan creation.
	MaxPlansPerHour int `json:"max_plans_per_hour" schema:"type:int,description:Max plans created per hour,category:advanced,default:10"`

	// StreamName is the JetStream stream for publishing triggers.
	StreamName string `json:"stream_name" schema:"type:string,description:JetStream stream for triggers,category:advanced,default:WORKFLOW"`

	// TriggerSubject is the NATS subject for plan creation requests.
	TriggerSubject string `json:"trigger_subject" schema:"type:string,description:Subject for plan creation triggers,category:advanced,default:workflow.trigger.github-plan-create"`

	// IssuesBucket is the KV bucket for tracking processed issues.
	IssuesBucket string `json:"issues_bucket" schema:"type:string,description:KV bucket for issue dedup,category:advanced,default:GITHUB_ISSUES"`
}

// GetPollInterval returns the parsed poll interval duration.
func (c *Config) GetPollInterval() time.Duration {
	d, err := time.ParseDuration(c.PollInterval)
	if err != nil {
		return 60 * time.Second
	}
	return d
}

// IsRequireLabel returns whether the label requirement is active.
func (c *Config) IsRequireLabel() bool {
	if c.RequireLabel == nil {
		return true
	}
	return *c.RequireLabel
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	defaultTrue := true
	return Config{
		PollInterval:    "60s",
		IssueLabel:      "semspec",
		RequireLabel:    &defaultTrue,
		MaxBodySize:     10000,
		MaxPlansPerHour: 10,
		StreamName:      "WORKFLOW",
		TriggerSubject:  "workflow.trigger.github-plan-create",
		IssuesBucket:    "GITHUB_ISSUES",
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
