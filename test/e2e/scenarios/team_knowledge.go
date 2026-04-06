package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// LessonsLearnedScenario tests the lessons learned infrastructure.
// It verifies that error categories are seeded and the lessons HTTP
// endpoint responds. This is a Tier 1 scenario (no LLM required).
type LessonsLearnedScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
}

// NewTeamKnowledgeScenario creates a lessons learned infrastructure scenario.
// Name kept for backwards compatibility with E2E runner registration.
func NewTeamKnowledgeScenario(cfg *config.Config) *LessonsLearnedScenario {
	return &LessonsLearnedScenario{
		name:        "lessons-learned",
		description: "Lessons learned infrastructure: error categories seeded, HTTP endpoints operational",
		config:      cfg,
	}
}

// Name implements Scenario.
func (s *LessonsLearnedScenario) Name() string { return s.name }

// Description implements Scenario.
func (s *LessonsLearnedScenario) Description() string { return s.description }

// Setup implements Scenario.
func (s *LessonsLearnedScenario) Setup(ctx context.Context) error {
	s.http = client.NewHTTPClient(s.config.HTTPBaseURL)
	if err := s.http.WaitForHealthy(ctx); err != nil {
		return fmt.Errorf("service not healthy: %w", err)
	}
	return nil
}

// Execute implements Scenario.
func (s *LessonsLearnedScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"verify-error-categories-seeded", s.stageVerifyErrorCategories},
		{"verify-lessons-endpoint", s.stageVerifyLessonsEndpoint},
		{"verify-lessons-counts-endpoint", s.stageVerifyLessonCountsEndpoint},
	}

	for _, stage := range stages {
		stageStart := time.Now()
		stageCtx, cancel := context.WithTimeout(ctx, s.config.StageTimeout)
		err := stage.fn(stageCtx, result)
		cancel()
		stageDuration := time.Since(stageStart)
		result.SetMetric(fmt.Sprintf("%s_duration_us", stage.name), stageDuration.Microseconds())
		if err != nil {
			result.AddStage(stage.name, false, stageDuration, err.Error())
			result.AddError(fmt.Sprintf("%s: %v", stage.name, err))
			result.Error = fmt.Sprintf("%s failed: %v", stage.name, err)
			return result, nil
		}
		result.AddStage(stage.name, true, stageDuration, "")
	}

	result.Success = true
	return result, nil
}

// Teardown implements Scenario.
func (s *LessonsLearnedScenario) Teardown(_ context.Context) error {
	return nil
}

// ---------------------------------------------------------------------------
// Stages
// ---------------------------------------------------------------------------

// stageVerifyErrorCategories checks that error categories are seeded in
// ENTITY_STATES (they're the foundation for signal matching and trend injection).
func (s *LessonsLearnedScenario) stageVerifyErrorCategories(ctx context.Context, result *Result) error {
	url := s.config.HTTPBaseURL + "/message-logger/kv/ENTITY_STATES"
	resp, err := httpGetJSON(ctx, url)
	if err != nil {
		return fmt.Errorf("query ENTITY_STATES: %w", err)
	}

	// The KV endpoint returns {bucket, count, entries, pattern} — extract entries.
	var entries []any
	switch v := resp.(type) {
	case []any:
		entries = v
	case map[string]any:
		arr, ok := v["entries"].([]any)
		if !ok {
			return fmt.Errorf("kv response missing 'entries' array, got keys: %v", v)
		}
		entries = arr
	default:
		return fmt.Errorf("unexpected kv response type: %T", resp)
	}

	const errcatPrefix = "semspec.local.agent.roster.errcat."
	errcatCount := 0
	for _, entry := range entries {
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		key, _ := m["key"].(string)
		if len(key) > len(errcatPrefix) && key[:len(errcatPrefix)] == errcatPrefix {
			errcatCount++
		}
	}

	result.SetDetail("error_category_count", errcatCount)

	if errcatCount < 7 {
		return fmt.Errorf("expected at least 7 error categories, found %d", errcatCount)
	}

	return nil
}

// stageVerifyLessonsEndpoint tests GET /execution-manager/lessons returns
// a JSON array (may be empty on fresh infra).
func (s *LessonsLearnedScenario) stageVerifyLessonsEndpoint(ctx context.Context, result *Result) error {
	url := s.config.HTTPBaseURL + "/execution-manager/lessons"
	resp, err := httpGetJSON(ctx, url)
	if err != nil {
		return fmt.Errorf("GET lessons: %w", err)
	}

	lessons, ok := resp.([]any)
	if !ok {
		return fmt.Errorf("expected JSON array from /lessons, got %T", resp)
	}

	result.SetDetail("lessons_count", len(lessons))
	return nil
}

// stageVerifyLessonCountsEndpoint tests GET /execution-manager/lessons/counts
// returns a JSON object.
func (s *LessonsLearnedScenario) stageVerifyLessonCountsEndpoint(ctx context.Context, _ *Result) error {
	url := s.config.HTTPBaseURL + "/execution-manager/lessons/counts?role=developer"
	resp, err := httpGetJSON(ctx, url)
	if err != nil {
		return fmt.Errorf("GET lessons/counts: %w", err)
	}

	_, ok := resp.(map[string]any)
	if !ok {
		return fmt.Errorf("expected JSON object from /lessons/counts, got %T", resp)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// httpGetJSON performs a GET request and decodes the JSON response.
func httpGetJSON(ctx context.Context, url string) (any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	var result any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}
	return result, nil
}
