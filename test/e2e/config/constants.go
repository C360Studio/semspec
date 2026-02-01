// Package config provides configuration constants for e2e tests.
package config

import "time"

// Default connection URLs.
const (
	DefaultNATSURL    = "nats://localhost:4222"
	DefaultMetricsURL = "http://localhost:9090"
)

// Default timeouts.
const (
	DefaultCommandTimeout = 30 * time.Second
	DefaultSetupTimeout   = 60 * time.Second
	DefaultStageTimeout   = 30 * time.Second
	DefaultPollInterval   = 500 * time.Millisecond
	DefaultWaitTimeout    = 10 * time.Second
)

// NATS subjects for semspec commands.
const (
	// UserMessageSubjectPrefix is the prefix for user message subjects.
	// Full subject: user.message.{channel_type}.{channel_id}
	UserMessageSubjectPrefix = "user.message"

	// ToolExecuteSubjectPrefix is the prefix for tool execution subjects.
	ToolExecuteSubjectPrefix = "tool.execute"

	// ToolResultSubjectPrefix is the prefix for tool result subjects.
	ToolResultSubjectPrefix = "tool.result"

	// ToolRegisterSubjectPrefix is for tool registration.
	ToolRegisterSubjectPrefix = "tool.register"
)

// E2E test identifiers.
const (
	E2EChannelType = "e2e"
	E2EUserID      = "e2e-runner"
)

// Workflow file names.
const (
	SemspecDir     = ".semspec"
	ChangesDir     = "changes"
	MetadataFile   = "metadata.json"
	ProposalFile   = "proposal.md"
	DesignFile     = "design.md"
	SpecFile       = "spec.md"
	TasksFile      = "tasks.md"
	ConstitutionMD = "constitution.md"
)

// Config holds the e2e test configuration.
type Config struct {
	NATSURL        string        `json:"nats_url"`
	MetricsURL     string        `json:"metrics_url"`
	WorkspacePath  string        `json:"workspace_path"`
	CommandTimeout time.Duration `json:"command_timeout"`
	SetupTimeout   time.Duration `json:"setup_timeout"`
	StageTimeout   time.Duration `json:"stage_timeout"`
}

// DefaultConfig returns a Config with default values.
func DefaultConfig() *Config {
	return &Config{
		NATSURL:        DefaultNATSURL,
		MetricsURL:     DefaultMetricsURL,
		WorkspacePath:  "/workspace",
		CommandTimeout: DefaultCommandTimeout,
		SetupTimeout:   DefaultSetupTimeout,
		StageTimeout:   DefaultStageTimeout,
	}
}
