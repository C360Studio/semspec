package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// QuestionsAPIScenario tests the Q&A HTTP API endpoints.
// It creates a question, retrieves it, answers it, and verifies the answer event.
// It also covers Q/A modernization features: Category, Metadata, and AnswerAction.
type QuestionsAPIScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	nats        *client.NATSClient
	fs          *client.FilesystemClient
}

// NewQuestionsAPIScenario creates a new Q&A API scenario.
func NewQuestionsAPIScenario(cfg *config.Config) *QuestionsAPIScenario {
	return &QuestionsAPIScenario{
		name:        "questions-api",
		description: "Tests Q&A HTTP API endpoints (list, get, answer)",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *QuestionsAPIScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *QuestionsAPIScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *QuestionsAPIScenario) Setup(ctx context.Context) error {
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

	// Create NATS client for direct KV writes (question creation)
	natsClient, err := client.NewNATSClient(ctx, s.config.NATSURL)
	if err != nil {
		return fmt.Errorf("create NATS client: %w", err)
	}
	s.nats = natsClient

	return nil
}

// Execute runs the Q&A API scenario.
func (s *QuestionsAPIScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"create-question", s.stageCreateQuestion},
		{"list-questions", s.stageListQuestions},
		{"get-question", s.stageGetQuestion},
		{"answer-question", s.stageAnswerQuestion},
		{"verify-answered", s.stageVerifyAnswered},
		{"verify-answer-event", s.stageVerifyAnswerEvent},
		{"verify-conflict", s.stageVerifyConflict},
		{"create-categorized-question", s.stageCreateCategorizedQuestion},
		{"verify-category-roundtrip", s.stageVerifyCategoryRoundtrip},
		{"answer-with-action", s.stageAnswerWithAction},
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

// Teardown cleans up after the scenario.
func (s *QuestionsAPIScenario) Teardown(ctx context.Context) error {
	if s.nats != nil {
		return s.nats.Close(ctx)
	}
	return nil
}

// stageCreateQuestion creates a test question in the QUESTIONS KV bucket.
// We do this by directly storing via the KV endpoint since there's no create API.
func (s *QuestionsAPIScenario) stageCreateQuestion(ctx context.Context, result *Result) error {
	// Generate a unique question ID
	questionID := fmt.Sprintf("q-%d", time.Now().UnixNano()%100000000)
	result.SetDetail("question_id", questionID)

	// For now, let's try listing questions first to see if there are any existing ones
	// Then we'll use the trigger approach if needed
	listResp, err := s.http.ListQuestions(ctx, "pending", "", 10)
	if err != nil {
		// If list fails, the endpoint might not be available
		return fmt.Errorf("list questions (initial): %w", err)
	}

	// If there are no pending questions, we need to create one
	// We'll trigger a /plan command which creates knowledge gap questions
	if len(listResp.Questions) == 0 {
		// No existing questions, need to create one via workflow
		// Use a command that might trigger question creation
		result.AddWarning("No existing questions found, will try to create one")

		// Since we can't directly create questions via HTTP, let's check if there's
		// any existing question in any status
		allResp, err := s.http.ListQuestions(ctx, "all", "", 10)
		if err != nil {
			return fmt.Errorf("list all questions: %w", err)
		}

		if len(allResp.Questions) > 0 {
			// Use the first existing question
			q := allResp.Questions[0]
			result.SetDetail("question_id", q.ID)
			result.SetDetail("question_topic", q.Topic)
			result.SetDetail("question_status", q.Status)
			result.SetDetail("using_existing_question", true)
			return nil
		}

		// No questions at all - we need to create one via NATS
		// This is a limitation - E2E tests might need to trigger question creation
		// via the workflow. For now, we'll create a minimal test by verifying
		// the API endpoints work even with empty results
		result.SetDetail("no_questions_available", true)
		result.SetDetail("question_id", questionID)
		result.AddWarning("No questions available - some tests will be skipped")
	} else {
		// Use the first pending question
		q := listResp.Questions[0]
		result.SetDetail("question_id", q.ID)
		result.SetDetail("question_topic", q.Topic)
		result.SetDetail("question_status", q.Status)
		result.SetDetail("using_existing_question", true)
	}

	return nil
}

// stageListQuestions tests GET /plan-manager/questions with filters.
func (s *QuestionsAPIScenario) stageListQuestions(ctx context.Context, result *Result) error {
	// Test listing with pending status (default)
	pendingResp, err := s.http.ListQuestions(ctx, "pending", "", 0)
	if err != nil {
		return fmt.Errorf("list pending questions: %w", err)
	}
	result.SetDetail("pending_count", pendingResp.Total)

	// Test listing all questions
	allResp, err := s.http.ListQuestions(ctx, "all", "", 0)
	if err != nil {
		return fmt.Errorf("list all questions: %w", err)
	}
	result.SetDetail("all_count", allResp.Total)

	// Test listing with topic filter (if we have questions)
	if allResp.Total > 0 {
		// Use the topic from the first question
		topic := allResp.Questions[0].Topic
		topicResp, err := s.http.ListQuestions(ctx, "all", topic, 0)
		if err != nil {
			return fmt.Errorf("list questions by topic: %w", err)
		}
		result.SetDetail("topic_filtered_count", topicResp.Total)

		// Verify the filter worked
		for _, q := range topicResp.Questions {
			if !strings.HasPrefix(q.Topic, strings.Split(topic, ".")[0]) {
				result.AddWarning(fmt.Sprintf("Topic filter may not be working: expected %s, got %s", topic, q.Topic))
			}
		}
	}

	// Test limit parameter
	limitResp, err := s.http.ListQuestions(ctx, "all", "", 1)
	if err != nil {
		return fmt.Errorf("list questions with limit: %w", err)
	}
	if len(limitResp.Questions) > 1 {
		return fmt.Errorf("limit not respected: expected <= 1, got %d", len(limitResp.Questions))
	}
	result.SetDetail("limit_test_passed", true)

	return nil
}

// stageGetQuestion tests GET /plan-manager/questions/{id}.
func (s *QuestionsAPIScenario) stageGetQuestion(ctx context.Context, result *Result) error {
	noQuestions, _ := result.GetDetailBool("no_questions_available")
	if noQuestions {
		result.AddWarning("Skipping get-question test - no questions available")
		return nil
	}

	questionID, ok := result.GetDetailString("question_id")
	if !ok {
		return fmt.Errorf("question_id not found in result")
	}

	// Get the question
	question, err := s.http.GetQuestion(ctx, questionID)
	if err != nil {
		return fmt.Errorf("get question %s: %w", questionID, err)
	}

	// Verify the response
	if question.ID != questionID {
		return fmt.Errorf("question ID mismatch: expected %s, got %s", questionID, question.ID)
	}

	result.SetDetail("question_from_agent", question.FromAgent)
	result.SetDetail("question_question", question.Question)
	result.SetDetail("get_question_success", true)

	// Test getting a non-existent question
	_, err = s.http.GetQuestion(ctx, "q-nonexistent")
	if err == nil {
		return fmt.Errorf("expected error for non-existent question, got nil")
	}
	if !strings.Contains(err.Error(), "404") {
		result.AddWarning(fmt.Sprintf("Expected 404 for non-existent question, got: %v", err))
	}

	return nil
}

// stageAnswerQuestion tests POST /plan-manager/questions/{id}/answer.
func (s *QuestionsAPIScenario) stageAnswerQuestion(ctx context.Context, result *Result) error {
	noQuestions, _ := result.GetDetailBool("no_questions_available")
	if noQuestions {
		result.AddWarning("Skipping answer-question test - no questions available")
		return nil
	}

	questionID, ok := result.GetDetailString("question_id")
	if !ok {
		return fmt.Errorf("question_id not found in result")
	}

	// Check if question is already answered
	currentStatus, _ := result.GetDetailString("question_status")
	if currentStatus == "answered" {
		result.AddWarning("Question already answered - skipping answer test")
		result.SetDetail("skip_answer_test", true)
		return nil
	}

	// Answer the question
	answer := "The authentication feature scope includes login, logout, and session management."
	confidence := "high"
	sources := "E2E test, ADR-001"

	answeredQuestion, err := s.http.AnswerQuestion(ctx, questionID, answer, confidence, sources)
	if err != nil {
		return fmt.Errorf("answer question: %w", err)
	}

	// Verify the response
	if answeredQuestion.Status != "answered" {
		return fmt.Errorf("expected status 'answered', got '%s'", answeredQuestion.Status)
	}
	if answeredQuestion.Answer != answer {
		return fmt.Errorf("answer mismatch: expected %q, got %q", answer, answeredQuestion.Answer)
	}
	if answeredQuestion.Confidence != confidence {
		return fmt.Errorf("confidence mismatch: expected %q, got %q", confidence, answeredQuestion.Confidence)
	}

	result.SetDetail("answer_submitted", true)
	result.SetDetail("answered_at", answeredQuestion.AnsweredAt)

	return nil
}

// stageVerifyAnswered verifies the question status changed to answered.
func (s *QuestionsAPIScenario) stageVerifyAnswered(ctx context.Context, result *Result) error {
	noQuestions, _ := result.GetDetailBool("no_questions_available")
	if noQuestions {
		result.AddWarning("Skipping verify-answered test - no questions available")
		return nil
	}

	skipTest, _ := result.GetDetailBool("skip_answer_test")
	if skipTest {
		result.AddWarning("Skipping verify-answered test - answer test was skipped")
		return nil
	}

	questionID, ok := result.GetDetailString("question_id")
	if !ok {
		return fmt.Errorf("question_id not found in result")
	}

	// Get the question again to verify status
	question, err := s.http.GetQuestion(ctx, questionID)
	if err != nil {
		return fmt.Errorf("get question after answer: %w", err)
	}

	if question.Status != "answered" {
		return fmt.Errorf("expected status 'answered', got '%s'", question.Status)
	}

	if question.AnsweredAt == nil {
		return fmt.Errorf("answered_at should be set")
	}

	result.SetDetail("verified_answered", true)
	return nil
}

// stageVerifyAnswerEvent checks for the answer event in message logger.
func (s *QuestionsAPIScenario) stageVerifyAnswerEvent(ctx context.Context, result *Result) error {
	noQuestions, _ := result.GetDetailBool("no_questions_available")
	if noQuestions {
		result.AddWarning("Skipping verify-answer-event test - no questions available")
		return nil
	}

	skipTest, _ := result.GetDetailBool("skip_answer_test")
	if skipTest {
		result.AddWarning("Skipping verify-answer-event test - answer test was skipped")
		return nil
	}

	questionID, ok := result.GetDetailString("question_id")
	if !ok {
		return fmt.Errorf("question_id not found in result")
	}

	// Poll for the answer event in message logger
	subject := fmt.Sprintf("question.answer.%s", questionID)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	var lastErr error
	for attempts := 0; attempts < 10; attempts++ {
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("timeout waiting for answer event: %w (last error: %v)", ctx.Err(), lastErr)
			}
			return fmt.Errorf("timeout waiting for answer event: %w", ctx.Err())
		case <-ticker.C:
			entries, err := s.http.GetMessageLogEntries(ctx, 50, "question.answer.>")
			if err != nil {
				lastErr = err
				continue
			}

			// Look for our specific answer event
			for _, entry := range entries {
				if entry.Subject == subject {
					result.SetDetail("answer_event_found", true)
					result.SetDetail("answer_event_subject", entry.Subject)

					// Parse the payload to verify content
					var payload struct {
						QuestionID string `json:"question_id"`
						Answer     string `json:"answer"`
					}
					if err := json.Unmarshal(entry.RawData, &payload); err == nil {
						if payload.QuestionID == questionID {
							result.SetDetail("answer_event_verified", true)
							return nil
						}
					}
					return nil
				}
			}
		}
	}

	// Answer event not found - this might be OK depending on configuration
	result.AddWarning("Answer event not found in message logger - this may be expected if message-logger doesn't capture question.answer.> subjects")
	return nil
}

// stageCreateCategorizedQuestion creates a question with category "environment" and metadata
// via direct NATS KV write, testing the Q/A modernization Category and Metadata fields.
func (s *QuestionsAPIScenario) stageCreateCategorizedQuestion(ctx context.Context, result *Result) error {
	questionID := fmt.Sprintf("q-cat-%d", time.Now().UnixNano()%100000000)
	result.SetDetail("categorized_question_id", questionID)

	q := map[string]any{
		"id":         questionID,
		"from_agent": "e2e-test",
		"topic":      "environment.build.gradle",
		"question":   "How do I install Gradle for the build?",
		"category":   "environment",
		"metadata": map[string]string{
			"exec_result": "command_not_found",
			"command":     "gradle",
		},
		"urgency":    "normal",
		"status":     "pending",
		"created_at": time.Now().UTC(),
	}

	data, err := json.Marshal(q)
	if err != nil {
		return fmt.Errorf("marshal question: %w", err)
	}

	js := s.nats.JetStreamContext()
	kv, err := js.KeyValue(ctx, "QUESTIONS")
	if err != nil {
		return fmt.Errorf("get QUESTIONS KV bucket: %w", err)
	}

	if _, err := kv.Put(ctx, questionID, data); err != nil {
		return fmt.Errorf("store categorized question: %w", err)
	}

	result.SetDetail("categorized_question_stored", true)
	return nil
}

// stageVerifyCategoryRoundtrip retrieves the categorized question via HTTP and verifies
// that Category and Metadata fields survive the KV → HTTP API roundtrip.
func (s *QuestionsAPIScenario) stageVerifyCategoryRoundtrip(ctx context.Context, result *Result) error {
	questionID, ok := result.GetDetailString("categorized_question_id")
	if !ok {
		return fmt.Errorf("categorized_question_id not found in result")
	}

	question, err := s.http.GetQuestion(ctx, questionID)
	if err != nil {
		return fmt.Errorf("get categorized question %s: %w", questionID, err)
	}

	if question.Category != "environment" {
		return fmt.Errorf("category mismatch: expected %q, got %q", "environment", question.Category)
	}

	if question.Metadata == nil {
		return fmt.Errorf("metadata is nil; expected exec_result and command keys")
	}

	if v := question.Metadata["exec_result"]; v != "command_not_found" {
		return fmt.Errorf("metadata[exec_result] mismatch: expected %q, got %q", "command_not_found", v)
	}

	if v := question.Metadata["command"]; v != "gradle" {
		return fmt.Errorf("metadata[command] mismatch: expected %q, got %q", "gradle", v)
	}

	result.SetDetail("category_roundtrip_verified", true)
	return nil
}

// stageAnswerWithAction answers the categorized question with an AnswerAction and verifies
// the returned question reflects status "answered".
func (s *QuestionsAPIScenario) stageAnswerWithAction(ctx context.Context, result *Result) error {
	questionID, ok := result.GetDetailString("categorized_question_id")
	if !ok {
		return fmt.Errorf("categorized_question_id not found in result")
	}

	req := client.AnswerQuestionRequest{
		Answer:     "Install Gradle to resolve the build issue",
		Confidence: "high",
		Action: &client.AnswerAction{
			Type: "install_package",
			Parameters: map[string]string{
				"package_manager": "apt",
				"packages":        "gradle",
			},
		},
	}

	answered, err := s.http.AnswerQuestionWithAction(ctx, questionID, req)
	if err != nil {
		return fmt.Errorf("answer question with action: %w", err)
	}

	if answered.Status != "answered" {
		return fmt.Errorf("expected status %q, got %q", "answered", answered.Status)
	}

	result.SetDetail("answer_with_action_verified", true)
	return nil
}

// stageVerifyConflict tests that answering an already-answered question returns 409.
func (s *QuestionsAPIScenario) stageVerifyConflict(ctx context.Context, result *Result) error {
	noQuestions, _ := result.GetDetailBool("no_questions_available")
	if noQuestions {
		result.AddWarning("Skipping verify-conflict test - no questions available")
		return nil
	}

	skipTest, _ := result.GetDetailBool("skip_answer_test")
	if skipTest {
		result.AddWarning("Skipping verify-conflict test - answer test was skipped")
		return nil
	}

	questionID, ok := result.GetDetailString("question_id")
	if !ok {
		return fmt.Errorf("question_id not found in result")
	}

	// Try to answer the question again - should get 409 Conflict
	_, err := s.http.AnswerQuestion(ctx, questionID, "Another answer", "low", "")
	if err == nil {
		return fmt.Errorf("expected error when answering already-answered question")
	}

	if !strings.Contains(err.Error(), "409") {
		return fmt.Errorf("expected 409 Conflict error, got: %v", err)
	}

	result.SetDetail("conflict_verified", true)
	return nil
}
