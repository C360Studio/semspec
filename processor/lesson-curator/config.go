// Package lessoncurator implements ADR-033 Phase 5: a periodic processor
// that retires lessons whose evidence has gone stale or whose
// LastInjectedAt is older than the configured threshold.
//
// Phase 5a (this commit) ships the idle-since-last-injection criterion.
// Phase 5b will add filesystem existence checks for EvidenceFiles.Path,
// and Phase 5c will add git-rewrite checks against EvidenceFiles.CommitSHA.
package lessoncurator

import (
	"fmt"
	"reflect"
	"time"

	"github.com/c360studio/semstreams/component"
)

// curatorSchema is the JSON schema for Config.
var curatorSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the lesson-curator component.
//
// The defaults pick a daily sweep with a 30-day idle threshold —
// generous enough that working lessons never get expired by accident,
// but aggressive enough that lessons that fall out of the rotation
// (Phase 4b) won't accumulate in the graph forever.
type Config struct {
	// Enabled gates the entire sweep loop. False = component starts but
	// the ticker no-ops; useful when a deployment hasn't yet validated
	// the retirement criteria against its own lesson distribution.
	Enabled bool `json:"enabled" schema:"type:boolean,description:Whether the sweep loop runs; false means the component starts but every tick is a no-op,category:basic,default:true"`

	// SweepInterval is how often the curator runs the retirement check.
	SweepInterval string `json:"sweep_interval" schema:"type:string,description:How often the curator scans the lessons graph,category:basic,default:24h"`

	// IdleThreshold is how long a lesson can go without being injected
	// before it's retired. Lessons that haven't been selected for
	// prompt injection in this much time are stale by definition.
	IdleThreshold string `json:"idle_threshold" schema:"type:string,description:Lessons not injected within this duration are retired,category:advanced,default:720h"`

	// MinAgeBeforeRetire prevents brand-new lessons from being retired
	// just because they haven't been injected yet. A lesson must be
	// older than this before any retirement criterion applies.
	MinAgeBeforeRetire string `json:"min_age_before_retire" schema:"type:string,description:Lessons younger than this are never retired regardless of idle status,category:advanced,default:168h"`

	// RepoPath is the repository root used to verify that an
	// EvidenceFiles[].Path still exists on disk (Phase 5b). Empty falls
	// back to SEMSPEC_REPO_PATH env var, then the process working
	// directory. When neither resolves to a real directory the
	// filesystem-existence retirement criterion is skipped.
	RepoPath string `json:"repo_path" schema:"type:string,description:Workspace root for resolving EvidenceFiles paths; falls back to SEMSPEC_REPO_PATH and CWD,category:advanced,default:"`
}

// DefaultConfig returns sensible defaults: daily sweep, 30-day idle
// threshold, 7-day grace period for new lessons.
func DefaultConfig() Config {
	return Config{
		Enabled:            true,
		SweepInterval:      "24h",
		IdleThreshold:      "720h", // 30 days
		MinAgeBeforeRetire: "168h", // 7 days
	}
}

// Validate checks the configuration.
func (c *Config) Validate() error {
	if _, err := time.ParseDuration(c.SweepInterval); err != nil {
		return fmt.Errorf("invalid sweep_interval: %w", err)
	}
	if _, err := time.ParseDuration(c.IdleThreshold); err != nil {
		return fmt.Errorf("invalid idle_threshold: %w", err)
	}
	if _, err := time.ParseDuration(c.MinAgeBeforeRetire); err != nil {
		return fmt.Errorf("invalid min_age_before_retire: %w", err)
	}
	return nil
}

// GetSweepInterval returns the parsed sweep interval, falling back to 24h.
func (c *Config) GetSweepInterval() time.Duration {
	if d, err := time.ParseDuration(c.SweepInterval); err == nil && d > 0 {
		return d
	}
	return 24 * time.Hour
}

// GetIdleThreshold returns the parsed idle threshold, falling back to 720h.
func (c *Config) GetIdleThreshold() time.Duration {
	if d, err := time.ParseDuration(c.IdleThreshold); err == nil && d > 0 {
		return d
	}
	return 720 * time.Hour
}

// GetMinAgeBeforeRetire returns the parsed min-age, falling back to 168h.
func (c *Config) GetMinAgeBeforeRetire() time.Duration {
	if d, err := time.ParseDuration(c.MinAgeBeforeRetire); err == nil && d > 0 {
		return d
	}
	return 168 * time.Hour
}
