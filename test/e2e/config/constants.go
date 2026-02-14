// Package config provides configuration constants for e2e tests.
package config

import "time"

// Default connection URLs.
// Semspec uses distinct ports to avoid conflicts with semstreams on the same machine.
const (
	DefaultNATSURL    = "nats://localhost:4322"
	DefaultHTTPURL    = "http://localhost:8180"
	DefaultMetricsURL = "http://localhost:9190"
)

// Default timeouts.
const (
	DefaultCommandTimeout = 90 * time.Second
	DefaultSetupTimeout   = 90 * time.Second
	DefaultStageTimeout   = 90 * time.Second
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
	HTTPBaseURL    string        `json:"http_base_url"`
	MetricsURL     string        `json:"metrics_url"`
	WorkspacePath  string        `json:"workspace_path"`
	FixturesPath   string        `json:"fixtures_path"`
	BinaryPath     string        `json:"binary_path"`
	ConfigPath     string        `json:"config_path"`
	CommandTimeout time.Duration `json:"command_timeout"`
	SetupTimeout   time.Duration `json:"setup_timeout"`
	StageTimeout   time.Duration `json:"stage_timeout"`
}

// DefaultConfig returns a Config with default values.
func DefaultConfig() *Config {
	return &Config{
		NATSURL:        DefaultNATSURL,
		HTTPBaseURL:    DefaultHTTPURL,
		MetricsURL:     DefaultMetricsURL,
		WorkspacePath:  "/workspace",
		FixturesPath:   "/fixtures",
		BinaryPath:     "./bin/semspec",
		ConfigPath:     "./configs/e2e.json",
		CommandTimeout: DefaultCommandTimeout,
		SetupTimeout:   DefaultSetupTimeout,
		StageTimeout:   DefaultStageTimeout,
	}
}

// GoFixturePath returns the path to the Go fixture project.
func (c *Config) GoFixturePath() string {
	return c.FixturesPath + "/go-project"
}

// TSFixturePath returns the path to the TypeScript fixture project.
func (c *Config) TSFixturePath() string {
	return c.FixturesPath + "/ts-project"
}

// PythonFixturePath returns the path to the Python fixture project.
func (c *Config) PythonFixturePath() string {
	return c.FixturesPath + "/python-project"
}

// JavaFixturePath returns the path to the Java fixture project.
func (c *Config) JavaFixturePath() string {
	return c.FixturesPath + "/java-project"
}

// JSFixturePath returns the path to the JavaScript fixture project.
func (c *Config) JSFixturePath() string {
	return c.FixturesPath + "/js-project"
}
