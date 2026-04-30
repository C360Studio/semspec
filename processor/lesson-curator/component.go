package lessoncurator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semspec/workflow/lessons"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
)

// Component is the lesson-curator processor. It runs a periodic sweep
// over the lessons graph and retires entries that meet the retirement
// criteria (Phase 5a: idle past threshold).
//
// No JetStream consumer, no NATS subscriptions. The only external
// dependency is the lessons.Writer + TripleWriter for graph reads/writes.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	lessonWriter *lessons.Writer

	// repoPath is the resolved workspace root used to verify
	// EvidenceFiles[].Path. Empty when neither the config field nor
	// SEMSPEC_REPO_PATH nor the working directory resolved to a real
	// directory — in that case the filesystem-existence retirement
	// criterion is skipped per Phase 5b's "skip silently" contract.
	repoPath string

	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	sweeps         atomic.Int64
	lessonsRetired atomic.Int64
	sweepFailures  atomic.Int64

	lastActivityMu sync.RWMutex
	lastActivity   time.Time
}

// NewComponent constructs a lesson-curator from raw JSON config and
// semstreams dependencies.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var cfg Config
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	defaults := DefaultConfig()
	if cfg.SweepInterval == "" {
		cfg.SweepInterval = defaults.SweepInterval
	}
	if cfg.IdleThreshold == "" {
		cfg.IdleThreshold = defaults.IdleThreshold
	}
	if cfg.MinAgeBeforeRetire == "" {
		cfg.MinAgeBeforeRetire = defaults.MinAgeBeforeRetire
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	logger := deps.GetLogger()

	tw := &graphutil.TripleWriter{
		NATSClient:    deps.NATSClient,
		Logger:        logger,
		ComponentName: "lesson-curator",
	}

	return &Component{
		name:         "lesson-curator",
		config:       cfg,
		natsClient:   deps.NATSClient,
		logger:       logger,
		lessonWriter: &lessons.Writer{TW: tw, Logger: logger},
		repoPath:     resolveRepoPath(cfg.RepoPath),
	}, nil
}

// resolveRepoPath picks the workspace root: explicit config > env >
// CWD. Returns "" when none of those resolve to a real directory; the
// curator skips the filesystem-existence retirement criterion in that
// case rather than producing false positives against a wrong root.
func resolveRepoPath(configured string) string {
	candidates := []string{configured, os.Getenv("SEMSPEC_REPO_PATH")}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, cwd)
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			continue
		}
		return abs
	}
	return ""
}

// rewriteCheckInRepo returns a rewriteCheck predicate scoped to repoPath.
// Empty repoPath disables the predicate (returns nil) so the criterion is
// skipped entirely — same shape as fileExistsInRepo.
//
// Implementation: `git blame -L<start>,<end> --porcelain` against the
// file. Each blamed line emits a header `<sha> <orig> <final>` — if any
// such SHA matches the cited CommitSHA prefix, the region still has at
// least one anchored line and we report "not rewritten". Zero matches
// → fully rewritten.
//
// We deliberately use a strict "any survival keeps it" threshold here.
// A softer ratio could be tuned later; for Phase 5c the cheap signal is
// good enough and false-negatives (kept lessons that should retire) are
// safer than false-positives (retiring still-relevant lessons).
func rewriteCheckInRepo(repoPath string) func(path string, lineStart, lineEnd int, commitSHA string) (bool, error) {
	if repoPath == "" {
		return nil
	}
	return func(path string, lineStart, lineEnd int, commitSHA string) (bool, error) {
		if path == "" || commitSHA == "" || lineStart <= 0 || lineEnd < lineStart {
			return false, fmt.Errorf("rewrite check: invalid args")
		}
		abs := path
		if !filepath.IsAbs(path) {
			abs = filepath.Join(repoPath, path)
		}
		// Bound the blame call so a runaway git invocation can't wedge
		// the sweep loop.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "git",
			"-C", repoPath,
			"blame",
			"--porcelain",
			fmt.Sprintf("-L%d,%d", lineStart, lineEnd),
			"--",
			abs,
		)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return false, fmt.Errorf("git blame: %w (%s)", err, strings.TrimSpace(stderr.String()))
		}

		prefix := commitSHA
		if len(prefix) > 12 {
			prefix = prefix[:12]
		}
		// Each non-continuation header line in --porcelain output starts
		// with a 40-char SHA followed by a space. Look for any blame SHA
		// whose prefix matches our cited commit; presence of even one
		// counts as "still anchored".
		for _, line := range strings.Split(stdout.String(), "\n") {
			if len(line) < 41 {
				continue
			}
			if line[40] != ' ' {
				continue
			}
			sha := line[:40]
			if strings.HasPrefix(sha, prefix) || strings.HasPrefix(commitSHA, sha[:min(len(sha), len(commitSHA))]) {
				return false, nil
			}
		}
		return true, nil
	}
}

// fileExistsInRepo returns a fileExists predicate scoped to repoPath.
// Workspace-relative paths are resolved against repoPath; absolute paths
// are checked as-is. Empty repoPath disables the predicate so the
// criterion is skipped (returns false → "evidence missing", which would
// retire every cited path — wrong outcome). Caller passes nil into the
// criteria struct in that case.
func fileExistsInRepo(repoPath string) func(string) bool {
	if repoPath == "" {
		return nil
	}
	return func(p string) bool {
		if p == "" {
			return false
		}
		abs := p
		if !filepath.IsAbs(p) {
			abs = filepath.Join(repoPath, p)
		}
		info, err := os.Stat(abs)
		return err == nil && (info.Mode().IsRegular() || info.IsDir())
	}
}

// Initialize prepares the component for startup.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized lesson-curator",
		"sweep_interval", c.config.SweepInterval,
		"idle_threshold", c.config.IdleThreshold,
		"min_age_before_retire", c.config.MinAgeBeforeRetire,
		"enabled", c.config.Enabled,
	)
	return nil
}

// Start runs the periodic sweep loop until the context is cancelled.
func (c *Component) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("component already running")
	}
	c.running = true
	c.startTime = time.Now()

	subCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.mu.Unlock()

	go c.sweepLoop(subCtx)

	c.logger.Info("lesson-curator started",
		"sweep_interval", c.config.GetSweepInterval(),
		"idle_threshold", c.config.GetIdleThreshold(),
		"enabled", c.config.Enabled,
	)
	return nil
}

// Stop gracefully stops the component.
func (c *Component) Stop(_ time.Duration) error {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return nil
	}
	cancel := c.cancel
	c.running = false
	c.cancel = nil
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	c.logger.Info("lesson-curator stopped",
		"sweeps", c.sweeps.Load(),
		"lessons_retired", c.lessonsRetired.Load(),
		"sweep_failures", c.sweepFailures.Load(),
	)
	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "lesson-curator",
		Type:        "processor",
		Description: "ADR-033 Phase 5: periodic retirement sweep over the lessons graph",
		Version:     "0.1.0",
	}
}

// InputPorts returns the input port definitions. The curator has no
// NATS inputs — it ticks on a timer and reads/writes the lessons graph
// directly via TripleWriter.
func (c *Component) InputPorts() []component.Port {
	return []component.Port{}
}

// OutputPorts returns the output port definitions. The curator's only
// output is graph writes via TripleWriter, which it shares with every
// lesson producer; no dedicated NATS subject.
func (c *Component) OutputPorts() []component.Port {
	return []component.Port{}
}

// ConfigSchema returns the configuration schema.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return curatorSchema
}

// Health reports the component's runtime state.
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	running := c.running
	startTime := c.startTime
	c.mu.RUnlock()

	status := "stopped"
	if running {
		status = "running"
	}

	return component.HealthStatus{
		Healthy:    running,
		LastCheck:  time.Now(),
		ErrorCount: int(c.sweepFailures.Load()),
		Uptime:     time.Since(startTime),
		Status:     status,
	}
}

// DataFlow reports how active the curator is.
func (c *Component) DataFlow() component.FlowMetrics {
	return component.FlowMetrics{
		MessagesPerSecond: 0,
		BytesPerSecond:    0,
		ErrorRate:         0,
		LastActivity:      c.getLastActivity(),
	}
}

// sweepLoop runs the retirement sweep on the configured cadence. The
// first sweep fires after one full interval — there's no benefit to a
// startup-time sweep, and it gives any pending writes time to land.
func (c *Component) sweepLoop(ctx context.Context) {
	interval := c.config.GetSweepInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !c.config.Enabled {
				c.logger.Debug("Sweep skipped — curator disabled")
				continue
			}
			c.runSweep(ctx)
		}
	}
}

// runSweep executes one full retirement pass: read every lesson, decide
// which ones to retire, write the RetiredAt triples. Failures are
// counted but do not abort the loop — the next tick re-attempts.
func (c *Component) runSweep(ctx context.Context) {
	c.sweeps.Add(1)
	c.updateLastActivity()

	if c.lessonWriter == nil {
		c.logger.Warn("Sweep skipped — lesson writer unavailable")
		c.sweepFailures.Add(1)
		return
	}

	all, err := c.lessonWriter.ListLessonsForRole(ctx, "", 0)
	if err != nil {
		c.logger.Warn("Sweep failed to list lessons", "error", err)
		c.sweepFailures.Add(1)
		return
	}

	criteria := retirementCriteria{
		now:                time.Now(),
		idleThreshold:      c.config.GetIdleThreshold(),
		minAgeBeforeRetire: c.config.GetMinAgeBeforeRetire(),
		fileExists:         fileExistsInRepo(c.repoPath),
		rewriteCheck:       rewriteCheckInRepo(c.repoPath),
	}

	var retired int
	for _, lesson := range all {
		ok, reason := criteria.shouldRetire(lesson)
		if !ok {
			continue
		}
		if err := c.lessonWriter.RetireLesson(ctx, lesson.ID, reason); err != nil {
			c.logger.Warn("Failed to retire lesson",
				"lesson_id", lesson.ID, "reason", reason, "error", err)
			c.sweepFailures.Add(1)
			continue
		}
		c.lessonsRetired.Add(1)
		retired++
	}

	c.logger.Info("Lesson retirement sweep complete",
		"scanned", len(all), "retired", retired)
}

func (c *Component) updateLastActivity() {
	c.lastActivityMu.Lock()
	c.lastActivity = time.Now()
	c.lastActivityMu.Unlock()
}

func (c *Component) getLastActivity() time.Time {
	c.lastActivityMu.RLock()
	defer c.lastActivityMu.RUnlock()
	return c.lastActivity
}
