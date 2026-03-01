// Package contextbuilder provides context gathering for workflow tasks.
package contextbuilder

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/c360studio/semspec/processor/context-builder/strategies"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/answerer"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// QAIntegrationConfig holds configuration for the Q&A integration.
type QAIntegrationConfig struct {
	// BlockingTimeout is the maximum time to wait for answers.
	// Default: 5 minutes
	BlockingTimeout time.Duration

	// AllowBlocking enables blocking behavior when context is insufficient.
	// Default: true
	AllowBlocking bool

	// SourceName identifies the component for question attribution.
	SourceName string
}

// DefaultQAIntegrationConfig returns default Q&A configuration.
func DefaultQAIntegrationConfig() QAIntegrationConfig {
	return QAIntegrationConfig{
		BlockingTimeout: 5 * time.Minute,
		AllowBlocking:   true,
		SourceName:      "context-builder",
	}
}

// QAIntegration handles question creation, routing, and answer incorporation.
type QAIntegration struct {
	questionStore *workflow.QuestionStore
	router        *answerer.Router
	natsClient    *natsclient.Client
	config        QAIntegrationConfig
	logger        *slog.Logger
}

// NewQAIntegration creates a new Q&A integration handler.
func NewQAIntegration(
	natsClient *natsclient.Client,
	questionStore *workflow.QuestionStore,
	router *answerer.Router,
	config QAIntegrationConfig,
	logger *slog.Logger,
) *QAIntegration {
	if logger == nil {
		logger = slog.Default()
	}
	return &QAIntegration{
		questionStore: questionStore,
		router:        router,
		natsClient:    natsClient,
		config:        config,
		logger:        logger,
	}
}

// AnsweredQuestion represents a question with its answer.
type AnsweredQuestion struct {
	Question strategies.Question
	Answer   string
	Answered bool
	Source   string // Who answered: agent/human/tool
}

// HandleInsufficientContext creates questions, routes them, and waits for answers.
// Returns answered questions or partial results on timeout.
func (qa *QAIntegration) HandleInsufficientContext(
	ctx context.Context,
	questions []strategies.Question,
	loopID string,
	planSlug string,
) ([]AnsweredQuestion, error) {
	if len(questions) == 0 {
		return nil, nil
	}

	if !qa.config.AllowBlocking {
		qa.logger.Info("Blocking disabled, skipping Q&A",
			"question_count", len(questions))
		return qa.asUnanswered(questions), nil
	}

	// Create timeout context
	ctx, cancel := context.WithTimeout(ctx, qa.config.BlockingTimeout)
	defer cancel()

	// Create and route questions
	createdQuestions := make([]*workflow.Question, 0, len(questions))
	for _, q := range questions {
		wq := workflow.NewQuestion(qa.config.SourceName, q.Topic, q.Question, q.Context)
		wq.BlockedLoopID = loopID
		wq.Urgency = qa.mapUrgency(q.Urgency)
		wq.PlanSlug = planSlug

		if err := qa.questionStore.Store(ctx, wq); err != nil {
			qa.logger.Warn("Failed to store question",
				"topic", q.Topic,
				"error", err)
			continue
		}

		// Route the question
		result, err := qa.router.RouteQuestion(ctx, wq)
		if err != nil {
			qa.logger.Warn("Failed to route question",
				"question_id", wq.ID,
				"error", err)
			continue
		}

		qa.logger.Info("Question created and routed",
			"question_id", wq.ID,
			"topic", q.Topic,
			"answerer", result.Route.Answerer)

		createdQuestions = append(createdQuestions, wq)
	}

	if len(createdQuestions) == 0 {
		qa.logger.Warn("Failed to create any questions, returning unanswered")
		return qa.asUnanswered(questions), nil
	}

	// Wait for answers
	answers, err := qa.waitForAnswers(ctx, createdQuestions)
	if err != nil {
		qa.logger.Warn("Error waiting for answers, returning partial results",
			"error", err)
	}

	// Map answers back to original questions
	return qa.mapAnswers(questions, createdQuestions, answers), nil
}

// waitForAnswers waits for answers to the given questions concurrently.
func (qa *QAIntegration) waitForAnswers(ctx context.Context, questions []*workflow.Question) (map[string]*workflow.AnswerPayload, error) {
	answers := make(map[string]*workflow.AnswerPayload)
	var mu sync.Mutex
	var wg sync.WaitGroup

	js, err := qa.natsClient.JetStream()
	if err != nil {
		return answers, fmt.Errorf("get jetstream: %w", err)
	}

	// Try to get KV bucket for answers
	kv, err := js.KeyValue(ctx, "QUESTION_ANSWERS")
	if err != nil {
		// Fall back to subject subscription if KV doesn't exist
		qa.logger.Debug("KV bucket not available, using subject subscription")
		return qa.subscribeForAnswers(ctx, questions)
	}

	// Watch for answers concurrently
	for _, q := range questions {
		wg.Add(1)
		go func(question *workflow.Question) {
			defer wg.Done()

			watcher, err := kv.Watch(ctx, question.ID)
			if err != nil {
				qa.logger.Debug("Failed to create watcher",
					"question_id", question.ID,
					"error", err)
				return
			}
			defer watcher.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case entry, ok := <-watcher.Updates():
					if !ok {
						return
					}
					if entry == nil {
						// Initial nil signals watcher is ready, continue waiting
						continue
					}
					if entry.Operation() == jetstream.KeyValueDelete {
						continue
					}

					var payload workflow.AnswerPayload
					if err := json.Unmarshal(entry.Value(), &payload); err != nil {
						qa.logger.Warn("Failed to unmarshal answer",
							"question_id", question.ID,
							"error", err)
						continue
					}

					mu.Lock()
					answers[question.ID] = &payload
					mu.Unlock()

					qa.logger.Info("Received answer via KV",
						"question_id", question.ID,
						"answered_by", payload.AnsweredBy)
					return
				}
			}
		}(q)
	}

	// Wait for all watchers to complete or timeout
	wg.Wait()

	return answers, nil
}

// subscribeForAnswers subscribes to answer subjects and waits for responses.
func (qa *QAIntegration) subscribeForAnswers(ctx context.Context, questions []*workflow.Question) (map[string]*workflow.AnswerPayload, error) {
	answers := make(map[string]*workflow.AnswerPayload)
	remaining := len(questions)

	js, err := qa.natsClient.JetStream()
	if err != nil {
		return answers, fmt.Errorf("get jetstream: %w", err)
	}

	// Create ephemeral consumer for answer subjects
	stream, err := js.Stream(ctx, "AGENT")
	if err != nil {
		return answers, fmt.Errorf("get stream: %w", err)
	}

	// Build filter subjects for all questions
	filterSubjects := make([]string, 0, len(questions))
	questionIDs := make(map[string]bool)
	for _, q := range questions {
		filterSubjects = append(filterSubjects, fmt.Sprintf("question.answer.%s", q.ID))
		questionIDs[q.ID] = true
	}

	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		FilterSubjects: filterSubjects,
		AckPolicy:      jetstream.AckExplicitPolicy,
		DeliverPolicy:  jetstream.DeliverNewPolicy,
	})
	if err != nil {
		return answers, fmt.Errorf("create consumer: %w", err)
	}

	// Consume messages until all answered or timeout
	for remaining > 0 {
		msgs, err := consumer.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
		if err != nil {
			if ctx.Err() != nil {
				return answers, ctx.Err()
			}
			continue
		}

		for msg := range msgs.Messages() {
			var baseMsg message.BaseMessage
			if err := json.Unmarshal(msg.Data(), &baseMsg); err != nil {
				msg.Nak()
				continue
			}

			var payload workflow.AnswerPayload
			payloadBytes, _ := json.Marshal(baseMsg.Payload())
			if err := json.Unmarshal(payloadBytes, &payload); err != nil {
				msg.Nak()
				continue
			}

			if questionIDs[payload.QuestionID] {
				answers[payload.QuestionID] = &payload
				remaining--
				qa.logger.Info("Received answer",
					"question_id", payload.QuestionID,
					"answered_by", payload.AnsweredBy)
			}
			msg.Ack()
		}

		// Check for timeout
		select {
		case <-ctx.Done():
			return answers, ctx.Err()
		default:
		}
	}

	return answers, nil
}

// mapAnswers maps answers back to original questions.
func (qa *QAIntegration) mapAnswers(
	original []strategies.Question,
	created []*workflow.Question,
	answers map[string]*workflow.AnswerPayload,
) []AnsweredQuestion {
	result := make([]AnsweredQuestion, len(original))

	// Create a map of topic -> created question ID
	topicToID := make(map[string]string)
	for _, q := range created {
		topicToID[q.Topic] = q.ID
	}

	for i, q := range original {
		result[i] = AnsweredQuestion{
			Question: q,
			Answered: false,
		}

		// Find the created question ID for this topic
		qID, ok := topicToID[q.Topic]
		if !ok {
			continue
		}

		// Check if we have an answer
		if answer, ok := answers[qID]; ok {
			result[i].Answer = answer.Answer
			result[i].Answered = true
			result[i].Source = answer.AnswererType
		}
	}

	return result
}

// asUnanswered converts questions to unanswered AnsweredQuestion slice.
func (qa *QAIntegration) asUnanswered(questions []strategies.Question) []AnsweredQuestion {
	result := make([]AnsweredQuestion, len(questions))
	for i, q := range questions {
		result[i] = AnsweredQuestion{
			Question: q,
			Answered: false,
		}
	}
	return result
}

// mapUrgency converts strategies.QuestionUrgency to workflow.QuestionUrgency.
func (qa *QAIntegration) mapUrgency(urgency strategies.QuestionUrgency) workflow.QuestionUrgency {
	switch urgency {
	case strategies.UrgencyLow:
		return workflow.QuestionUrgencyLow
	case strategies.UrgencyNormal:
		return workflow.QuestionUrgencyNormal
	case strategies.UrgencyHigh:
		return workflow.QuestionUrgencyHigh
	case strategies.UrgencyBlocking:
		return workflow.QuestionUrgencyBlocking
	default:
		return workflow.QuestionUrgencyNormal
	}
}

// EnrichWithAnswers incorporates answers into a strategy result.
func (qa *QAIntegration) EnrichWithAnswers(result *strategies.StrategyResult, answers []AnsweredQuestion) *strategies.StrategyResult {
	if result == nil {
		return result
	}

	// Add answered questions as context documents
	for i, aq := range answers {
		if !aq.Answered {
			continue
		}

		// Create a document key for the answer, using index to avoid collisions
		docKey := fmt.Sprintf("__qa_answer__%s_%d", aq.Question.Topic, i)
		content := fmt.Sprintf("## Question\n%s\n\n## Answer\n%s\n\n_Answered by: %s_",
			aq.Question.Question,
			aq.Answer,
			aq.Source,
		)

		if result.Documents == nil {
			result.Documents = make(map[string]string)
		}
		result.Documents[docKey] = content
	}

	// Clear questions that were answered, keep unanswered ones
	remainingQuestions := make([]strategies.Question, 0)
	for _, aq := range answers {
		if !aq.Answered {
			remainingQuestions = append(remainingQuestions, aq.Question)
		}
	}
	result.Questions = remainingQuestions

	// Update insufficiency flag if all questions were answered
	if len(remainingQuestions) == 0 {
		result.InsufficientContext = false
	}

	return result
}
