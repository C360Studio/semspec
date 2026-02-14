package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// ExploreInteractiveScenario tests the /explore command with full multi-turn
// question-answer interaction. This validates the async nature of explore:
// 1. /explore triggers agentic loop
// 2. LLM may ask clarifying questions via <gap> blocks
// 3. Questions appear in QUESTIONS KV bucket
// 4. User answers via /answer command
// 5. Loop continues until exploration complete
type ExploreInteractiveScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	fs          *client.FilesystemClient
	llmClient   *http.Client
}

// NewExploreInteractiveScenario creates a new interactive explore scenario.
func NewExploreInteractiveScenario(cfg *config.Config) *ExploreInteractiveScenario {
	return &ExploreInteractiveScenario{
		name:        "explore-interactive",
		description: "Tests /explore with multi-turn Q&A: LLM asks questions, test answers them",
		config:      cfg,
		llmClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// Name returns the scenario name.
func (s *ExploreInteractiveScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *ExploreInteractiveScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *ExploreInteractiveScenario) Setup(ctx context.Context) error {
	// Create filesystem client and setup workspace
	s.fs = client.NewFilesystemClient(s.config.WorkspacePath)
	if err := s.fs.SetupWorkspace(); err != nil {
		return fmt.Errorf("setup workspace: %w", err)
	}

	// Create HTTP client
	s.http = client.NewHTTPClient(s.config.HTTPBaseURL)

	// Wait for service to be healthy
	if err := s.http.WaitForHealthy(ctx); err != nil {
		return fmt.Errorf("service not healthy: %w", err)
	}

	// Check if LLM (Ollama) is available
	if !s.isLLMAvailable() {
		return fmt.Errorf("LLM not available at localhost:11434 - start Ollama to run this test")
	}

	return nil
}

// isLLMAvailable checks if the LLM endpoint is reachable.
func (s *ExploreInteractiveScenario) isLLMAvailable() bool {
	resp, err := s.llmClient.Get("http://localhost:11434/api/tags")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Execute runs the interactive explore scenario.
func (s *ExploreInteractiveScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"send-explore-command", s.stageSendExploreCommand},
		{"wait-for-exploration-created", s.stageWaitForExplorationCreated},
		{"poll-and-answer-questions", s.stagePollAndAnswerQuestions},
		{"verify-exploration-enriched", s.stageVerifyExplorationEnriched},
	}

	for _, stage := range stages {
		stageStart := time.Now()
		// Use longer timeout for LLM stages
		stageTimeout := s.config.StageTimeout
		if stage.name == "poll-and-answer-questions" {
			stageTimeout = 180 * time.Second // Multi-turn can take a while
		}
		stageCtx, cancel := context.WithTimeout(ctx, stageTimeout)

		err := stage.fn(stageCtx, result)
		cancel()

		stageDuration := time.Since(stageStart)
		result.SetMetric(fmt.Sprintf("%s_duration_ms", stage.name), stageDuration.Milliseconds())

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

// Teardown cleans up after the scenario.
func (s *ExploreInteractiveScenario) Teardown(ctx context.Context) error {
	return nil
}

// stageSendExploreCommand sends /explore command to start an exploration.
func (s *ExploreInteractiveScenario) stageSendExploreCommand(ctx context.Context, result *Result) error {
	// Use a topic that's likely to generate questions
	explorationTopic := "database migration strategy"
	expectedSlug := "database-migration-strategy"
	result.SetDetail("exploration_topic", explorationTopic)
	result.SetDetail("expected_slug", expectedSlug)

	// Send /explore (LLM is default now)
	resp, err := s.http.SendMessage(ctx, "/explore "+explorationTopic)
	if err != nil {
		return fmt.Errorf("send /explore command: %w", err)
	}

	result.SetDetail("explore_response_type", resp.Type)
	result.SetDetail("explore_response_content", resp.Content)

	if resp.Type == "error" {
		return fmt.Errorf("explore returned error: %s", resp.Content)
	}

	result.SetDetail("explore_sent", true)
	return nil
}

// stageWaitForExplorationCreated waits for the exploration plan.json to be created.
func (s *ExploreInteractiveScenario) stageWaitForExplorationCreated(ctx context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	// Wait for change directory to exist
	if err := s.fs.WaitForChange(ctx, expectedSlug); err != nil {
		return fmt.Errorf("exploration directory not created: %w", err)
	}

	// Verify plan.json exists
	if err := s.fs.WaitForChangeFile(ctx, expectedSlug, "plan.json"); err != nil {
		return fmt.Errorf("plan.json not created: %w", err)
	}

	// Load and verify plan.json is an exploration (uncommitted)
	planPath := s.fs.ChangePath(expectedSlug) + "/plan.json"
	var plan map[string]any
	if err := s.fs.ReadJSON(planPath, &plan); err != nil {
		return fmt.Errorf("read plan.json: %w", err)
	}

	// Exploration plans start as uncommitted
	committed, ok := plan["committed"].(bool)
	if !ok {
		return fmt.Errorf("plan.json missing 'committed' field")
	}
	if committed {
		// It's already committed - that's also valid, LLM might have completed quickly
		result.SetDetail("quickly_committed", true)
	}

	result.SetDetail("exploration_created", true)
	return nil
}

// Question represents a pending question from the QUESTIONS KV bucket.
type Question struct {
	ID            string `json:"id"`
	FromAgent     string `json:"from_agent"`
	Topic         string `json:"topic"`
	Question      string `json:"question"`
	Context       string `json:"context,omitempty"`
	BlockedLoopID string `json:"blocked_loop_id,omitempty"`
	Status        string `json:"status"`
}

// stagePollAndAnswerQuestions polls for questions and answers them.
func (s *ExploreInteractiveScenario) stagePollAndAnswerQuestions(ctx context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	// Check if exploration was quickly committed (no questions needed)
	if quicklyCommitted, ok := result.GetDetailBool("quickly_committed"); ok && quicklyCommitted {
		result.SetDetail("questions_answered", 0)
		result.SetDetail("no_questions_needed", true)
		return nil
	}

	questionsAnswered := 0
	maxQuestions := 5 // Safety limit

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for questionsAnswered < maxQuestions {
		select {
		case <-ctx.Done():
			// Timeout - check if exploration completed without questions
			if s.isExplorationComplete(ctx, expectedSlug) {
				result.SetDetail("questions_answered", questionsAnswered)
				result.SetDetail("completed_during_polling", true)
				return nil
			}
			return fmt.Errorf("timeout while polling for questions (answered %d)", questionsAnswered)

		case <-ticker.C:
			// Check if exploration is complete
			if s.isExplorationComplete(ctx, expectedSlug) {
				result.SetDetail("questions_answered", questionsAnswered)
				return nil
			}

			// Poll for pending questions
			questions, err := s.getPendingQuestions(ctx)
			if err != nil {
				// Continue polling even if there's an error
				continue
			}

			if len(questions) == 0 {
				continue
			}

			// Answer the first pending question
			q := questions[0]
			answer := s.generateAnswer(q)

			resp, err := s.http.SendMessage(ctx, fmt.Sprintf("/answer %s %s", q.ID, answer))
			if err != nil {
				result.AddWarning(fmt.Sprintf("failed to answer question %s: %v", q.ID, err))
				continue
			}

			if resp.Type == "error" {
				result.AddWarning(fmt.Sprintf("answer command returned error for %s: %s", q.ID, resp.Content))
				continue
			}

			questionsAnswered++
			result.SetDetail(fmt.Sprintf("question_%d_id", questionsAnswered), q.ID)
			result.SetDetail(fmt.Sprintf("question_%d_text", questionsAnswered), q.Question)
			result.SetDetail(fmt.Sprintf("answer_%d", questionsAnswered), answer)
		}
	}

	result.SetDetail("questions_answered", questionsAnswered)
	return nil
}

// getPendingQuestions retrieves pending questions from the QUESTIONS KV bucket.
func (s *ExploreInteractiveScenario) getPendingQuestions(ctx context.Context) ([]Question, error) {
	kvResp, err := s.http.GetKVEntries(ctx, "QUESTIONS")
	if err != nil {
		return nil, err
	}

	var questions []Question
	for _, entry := range kvResp.Entries {
		var q Question
		if err := json.Unmarshal([]byte(entry.Value), &q); err != nil {
			continue
		}
		if q.Status == "pending" {
			questions = append(questions, q)
		}
	}

	return questions, nil
}

// generateAnswer produces a reasonable answer based on the question.
func (s *ExploreInteractiveScenario) generateAnswer(q Question) string {
	questionLower := strings.ToLower(q.Question)

	// Provide contextually appropriate answers
	if strings.Contains(questionLower, "database") || strings.Contains(questionLower, "migration") {
		return "We are using PostgreSQL and need zero-downtime migrations. We have about 50 tables with moderate complexity."
	}
	if strings.Contains(questionLower, "framework") || strings.Contains(questionLower, "tool") {
		return "We prefer using golang-migrate for schema migrations and have CI/CD integration requirements."
	}
	if strings.Contains(questionLower, "timeline") || strings.Contains(questionLower, "deadline") {
		return "We need to complete this within the next sprint, approximately 2 weeks."
	}
	if strings.Contains(questionLower, "team") || strings.Contains(questionLower, "experience") {
		return "The team has experience with schema migrations but wants to establish best practices."
	}
	if strings.Contains(questionLower, "risk") || strings.Contains(questionLower, "concern") {
		return "Main concerns are data integrity and rollback capabilities in production."
	}

	// Default answer
	return "Yes, please proceed with the standard approach for this aspect."
}

// isExplorationComplete checks if the exploration has been enriched by the LLM.
func (s *ExploreInteractiveScenario) isExplorationComplete(ctx context.Context, slug string) bool {
	planPath := s.fs.ChangePath(slug) + "/plan.json"
	var plan map[string]any
	if err := s.fs.ReadJSON(planPath, &plan); err != nil {
		return false
	}

	// Check if exploration has been enriched with Goal (sign of LLM processing)
	goal, hasGoal := plan["goal"].(string)
	if hasGoal && goal != "" {
		return true
	}

	// Also check for execution steps
	execution, hasExecution := plan["execution"].(string)
	if hasExecution && execution != "" {
		return true
	}

	return false
}

// stageVerifyExplorationEnriched verifies the exploration has LLM-generated content.
func (s *ExploreInteractiveScenario) stageVerifyExplorationEnriched(ctx context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")
	planPath := s.fs.ChangePath(expectedSlug) + "/plan.json"

	// Poll for enrichment to complete
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for exploration to be enriched")
		case <-ticker.C:
			var plan map[string]any
			if err := s.fs.ReadJSON(planPath, &plan); err != nil {
				continue
			}

			// Check for Goal (mandatory for enriched plans)
			goal, hasGoal := plan["goal"].(string)
			if !hasGoal || goal == "" {
				continue
			}

			result.SetDetail("goal", goal)

			// Capture other enriched fields
			if planContext, ok := plan["context"].(string); ok && planContext != "" {
				result.SetDetail("context", planContext)
			}
			if execution, ok := plan["execution"].(string); ok && execution != "" {
				result.SetDetail("execution", execution)
			}

			// Check scope structure
			if scope, ok := plan["scope"].(map[string]any); ok {
				result.SetDetail("has_scope", true)
				if include, ok := scope["include"].([]any); ok {
					result.SetDetail("scope_include_count", len(include))
				}
			}

			result.SetDetail("exploration_enriched", true)
			return nil
		}
	}
}
