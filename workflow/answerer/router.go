package answerer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/google/uuid"
)

// Router handles question routing based on topic patterns.
type Router struct {
	registry *Registry
	nc       *natsclient.Client
	logger   *slog.Logger
}

// NewRouter creates a new question router.
func NewRouter(registry *Registry, nc *natsclient.Client, logger *slog.Logger) *Router {
	if logger == nil {
		logger = slog.Default()
	}
	return &Router{
		registry: registry,
		nc:       nc,
		logger:   logger,
	}
}

// RouteResult contains the result of routing a question.
type RouteResult struct {
	// Route is the matched route configuration.
	Route *Route

	// Routed indicates whether the question was successfully routed.
	Routed bool

	// Message describes the routing result.
	Message string
}

// RouteQuestion routes a question to the appropriate answerer.
func (r *Router) RouteQuestion(ctx context.Context, q *workflow.Question) (*RouteResult, error) {
	route := r.registry.Match(q.Topic)

	r.logger.Info("Routing question",
		"question_id", q.ID,
		"topic", q.Topic,
		"answerer", route.Answerer,
		"type", route.Type)

	result := &RouteResult{
		Route:  route,
		Routed: false,
	}

	var err error
	switch route.Type {
	case AnswererAgent:
		err = r.routeToAgent(ctx, q, route)
		result.Message = fmt.Sprintf("Routed to agent %s for auto-answer", GetAnswererName(route.Answerer))
	case AnswererTeam:
		err = r.routeToTeam(ctx, q, route)
		result.Message = fmt.Sprintf("Routed to team %s", GetAnswererName(route.Answerer))
	case AnswererHuman:
		err = r.routeToHuman(ctx, q, route)
		result.Message = fmt.Sprintf("Assigned to %s", GetAnswererName(route.Answerer))
	case AnswererTool:
		err = r.routeToTool(ctx, q, route)
		result.Message = fmt.Sprintf("Sent to tool %s for auto-answer", GetAnswererName(route.Answerer))
	default:
		err = fmt.Errorf("unknown answerer type: %s", route.Type)
	}

	if err != nil {
		return result, fmt.Errorf("route question: %w", err)
	}

	result.Routed = true
	return result, nil
}

// routeToAgent publishes a task for an agent to answer the question.
func (r *Router) routeToAgent(ctx context.Context, q *workflow.Question, route *Route) error {
	// Create a task for the question-answerer processor
	task := QuestionAnswerTask{
		TaskID:     uuid.New().String(),
		QuestionID: q.ID,
		Topic:      q.Topic,
		Question:   q.Question,
		Context:    q.Context,
		Capability: route.Capability,
		AgentName:  GetAnswererName(route.Answerer),
		SLA:        route.SLA.Duration(),
		CreatedAt:  time.Now(),
		// Propagate trace context for observability
		TraceID: q.TraceID,
		LoopID:  q.BlockedLoopID,
	}

	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("marshal task: %w", err)
	}

	subject := "agent.task.question-answerer"
	if err := r.nc.PublishToStream(ctx, subject, data); err != nil {
		return fmt.Errorf("publish task: %w", err)
	}

	r.logger.Info("Routed question to agent",
		"question_id", q.ID,
		"agent", route.Answerer,
		"capability", route.Capability,
		"task_id", task.TaskID)

	return nil
}

// routeToTeam notifies a team about a pending question.
func (r *Router) routeToTeam(ctx context.Context, q *workflow.Question, route *Route) error {
	// Update question with assigned team
	q.AssignedTo = route.Answerer
	q.AssignedAt = time.Now()

	// Send notification if configured
	if route.Notify != "" {
		if err := r.sendNotification(ctx, route.Notify, q, "new_question"); err != nil {
			r.logger.Warn("Failed to send team notification",
				"question_id", q.ID,
				"notify", route.Notify,
				"error", err)
			// Don't fail routing if notification fails
		}
	}

	r.logger.Info("Routed question to team",
		"question_id", q.ID,
		"team", route.Answerer,
		"notify", route.Notify)

	return nil
}

// routeToHuman assigns a question to a specific human.
func (r *Router) routeToHuman(ctx context.Context, q *workflow.Question, route *Route) error {
	humanName := GetAnswererName(route.Answerer)

	// Handle special "requester" case
	if humanName == "requester" {
		// Use the original requester from the question context
		// This would be populated from the workflow metadata
		q.AssignedTo = "human/requester"
	} else {
		q.AssignedTo = route.Answerer
	}
	q.AssignedAt = time.Now()

	r.logger.Info("Assigned question to human",
		"question_id", q.ID,
		"human", q.AssignedTo)

	return nil
}

// routeToTool publishes a task for a tool to answer the question.
func (r *Router) routeToTool(ctx context.Context, q *workflow.Question, route *Route) error {
	toolName := GetAnswererName(route.Answerer)

	// Create a tool task
	task := ToolAnswerTask{
		TaskID:     uuid.New().String(),
		QuestionID: q.ID,
		Topic:      q.Topic,
		Question:   q.Question,
		Context:    q.Context,
		ToolName:   toolName,
		CreatedAt:  time.Now(),
		// Propagate trace context for observability
		TraceID: q.TraceID,
		LoopID:  q.BlockedLoopID,
	}

	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("marshal task: %w", err)
	}

	subject := fmt.Sprintf("tool.task.%s", toolName)
	if err := r.nc.PublishToStream(ctx, subject, data); err != nil {
		return fmt.Errorf("publish task: %w", err)
	}

	r.logger.Info("Routed question to tool",
		"question_id", q.ID,
		"tool", toolName,
		"task_id", task.TaskID)

	return nil
}

// sendNotification sends a notification about a question.
func (r *Router) sendNotification(ctx context.Context, channel string, q *workflow.Question, event string) error {
	notification := QuestionNotification{
		QuestionID: q.ID,
		Topic:      q.Topic,
		Question:   q.Question,
		Event:      event,
		Channel:    channel,
		Timestamp:  time.Now(),
	}

	data, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	// Parse channel to determine subject
	// e.g., "slack://channel-name" → "notification.slack.channel-name"
	subject := parseNotificationSubject(channel)

	if err := r.nc.PublishToStream(ctx, subject, data); err != nil {
		return fmt.Errorf("publish notification: %w", err)
	}

	return nil
}

// parseNotificationSubject converts a channel URL to a NATS subject.
func parseNotificationSubject(channel string) string {
	// Default subject
	subject := "notification.generic"

	// Parse "protocol://destination" format
	if len(channel) > 0 {
		for _, prefix := range []string{"slack://", "email://", "webhook://"} {
			if len(channel) > len(prefix) && channel[:len(prefix)] == prefix {
				protocol := prefix[:len(prefix)-3] // Remove "://"
				destination := channel[len(prefix):]
				subject = fmt.Sprintf("notification.%s.%s", protocol, destination)
				break
			}
		}
	}

	return subject
}

// QuestionAnswerTask is the payload for agent question-answering tasks.
type QuestionAnswerTask struct {
	TaskID     string        `json:"task_id"`
	QuestionID string        `json:"question_id"`
	Topic      string        `json:"topic"`
	Question   string        `json:"question"`
	Context    string        `json:"context,omitempty"`
	Capability string        `json:"capability,omitempty"`
	AgentName  string        `json:"agent_name"`
	SLA        time.Duration `json:"sla,omitempty"`
	CreatedAt  time.Time     `json:"created_at"`
	// TraceID correlates this task with other messages in the same request flow.
	TraceID string `json:"trace_id,omitempty"`
	// LoopID is the agent loop that initiated this task (if any).
	LoopID string `json:"loop_id,omitempty"`
}

// ToolAnswerTask is the payload for tool question-answering tasks.
type ToolAnswerTask struct {
	TaskID     string    `json:"task_id"`
	QuestionID string    `json:"question_id"`
	Topic      string    `json:"topic"`
	Question   string    `json:"question"`
	Context    string    `json:"context,omitempty"`
	ToolName   string    `json:"tool_name"`
	CreatedAt  time.Time `json:"created_at"`
	// TraceID correlates this task with other messages in the same request flow.
	TraceID string `json:"trace_id,omitempty"`
	// LoopID is the agent loop that initiated this task (if any).
	LoopID string `json:"loop_id,omitempty"`
}

// QuestionNotification is the payload for question notifications.
type QuestionNotification struct {
	QuestionID string    `json:"question_id"`
	Topic      string    `json:"topic"`
	Question   string    `json:"question"`
	Event      string    `json:"event"` // new_question, escalated, answered, timeout
	Channel    string    `json:"channel"`
	Timestamp  time.Time `json:"timestamp"`
}
