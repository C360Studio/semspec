package answerer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
)

func init() {
	// Register Notification type for message deserialization
	_ = component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "notification",
		Category:    "question",
		Version:     "v1",
		Description: "Question notification payload",
		Factory:     func() any { return &Notification{} },
	})
}

// Notifier sends notifications about questions.
type Notifier struct {
	nc     *natsclient.Client
	logger *slog.Logger
}

// NewNotifier creates a new notifier.
func NewNotifier(nc *natsclient.Client, logger *slog.Logger) *Notifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &Notifier{
		nc:     nc,
		logger: logger,
	}
}

// NotificationEvent is the type of notification event.
type NotificationEvent string

const (
	NotificationEventNewQuestion NotificationEvent = "new_question"
	NotificationEventAnswered    NotificationEvent = "answered"
	NotificationEventTimeout     NotificationEvent = "timeout"
	NotificationEventEscalated   NotificationEvent = "escalated"
)

// Notification represents a notification about a question.
type Notification struct {
	// QuestionID is the question this notification is about.
	QuestionID string `json:"question_id"`

	// Topic is the question's topic.
	Topic string `json:"topic"`

	// Question is the question text.
	Question string `json:"question"`

	// Event is the type of notification.
	Event NotificationEvent `json:"event"`

	// Channel is the target channel (e.g., "slack://general").
	Channel string `json:"channel"`

	// AssignedTo is who the question is assigned to (if applicable).
	AssignedTo string `json:"assigned_to,omitempty"`

	// Answer is the answer text (for answered notifications).
	Answer string `json:"answer,omitempty"`

	// AnsweredBy is who answered (for answered notifications).
	AnsweredBy string `json:"answered_by,omitempty"`

	// Timestamp is when this notification was created.
	Timestamp time.Time `json:"timestamp"`
}

// Schema returns the message type for this payload.
func (n *Notification) Schema() message.Type {
	return NotificationType
}

// Validate validates the notification.
func (n *Notification) Validate() error {
	if n.QuestionID == "" {
		return fmt.Errorf("question_id is required")
	}
	if n.Event == "" {
		return fmt.Errorf("event is required")
	}
	return nil
}

// MarshalJSON marshals the notification to JSON.
func (n *Notification) MarshalJSON() ([]byte, error) {
	type Alias Notification
	return json.Marshal((*Alias)(n))
}

// UnmarshalJSON unmarshals the notification from JSON.
func (n *Notification) UnmarshalJSON(data []byte) error {
	type Alias Notification
	return json.Unmarshal(data, (*Alias)(n))
}

// NotificationType is the message type for notifications.
var NotificationType = message.Type{
	Domain:   "notification",
	Category: "question",
	Version:  "v1",
}

// Notify sends a notification about a question.
func (n *Notifier) Notify(ctx context.Context, channel string, q *workflow.Question, event NotificationEvent) error {
	notification := &Notification{
		QuestionID: q.ID,
		Topic:      q.Topic,
		Question:   q.Question,
		Event:      event,
		Channel:    channel,
		AssignedTo: q.AssignedTo,
		Timestamp:  time.Now(),
	}

	// Add answer info if this is an answered notification
	if event == NotificationEventAnswered {
		notification.Answer = q.Answer
		notification.AnsweredBy = q.AnsweredBy
	}

	return n.send(ctx, notification)
}

// NotifyNewQuestion sends a notification about a new question.
func (n *Notifier) NotifyNewQuestion(ctx context.Context, channel string, q *workflow.Question) error {
	return n.Notify(ctx, channel, q, NotificationEventNewQuestion)
}

// NotifyAnswered sends a notification about an answered question.
func (n *Notifier) NotifyAnswered(ctx context.Context, channel string, q *workflow.Question) error {
	return n.Notify(ctx, channel, q, NotificationEventAnswered)
}

// NotifyTimeout sends a notification about a timed-out question.
func (n *Notifier) NotifyTimeout(ctx context.Context, channel string, q *workflow.Question) error {
	return n.Notify(ctx, channel, q, NotificationEventTimeout)
}

// NotifyEscalated sends a notification about an escalated question.
func (n *Notifier) NotifyEscalated(ctx context.Context, channel string, q *workflow.Question) error {
	return n.Notify(ctx, channel, q, NotificationEventEscalated)
}

// send publishes the notification to the appropriate subject.
func (n *Notifier) send(ctx context.Context, notification *Notification) error {
	baseMsg := message.NewBaseMessage(NotificationType, notification, "question-notifier")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	// Parse channel to determine subject
	subject := parseChannelSubject(notification.Channel)

	if err := n.nc.PublishToStream(ctx, subject, data); err != nil {
		return fmt.Errorf("publish to %s: %w", subject, err)
	}

	n.logger.Debug("Notification sent",
		"question_id", notification.QuestionID,
		"event", notification.Event,
		"channel", notification.Channel,
		"subject", subject)

	return nil
}

// parseChannelSubject converts a channel URL to a NATS subject.
// Examples:
//   - "slack://general" → "notification.slack.general"
//   - "email://team@example.com" → "notification.email.team@example.com"
//   - "webhook://https://hooks.example.com" → "notification.webhook.https://hooks.example.com"
func parseChannelSubject(channel string) string {
	// Default subject
	subject := "notification.generic"

	// Parse "protocol://destination" format
	if len(channel) > 0 {
		for _, prefix := range []string{"slack://", "email://", "webhook://"} {
			if strings.HasPrefix(channel, prefix) {
				protocol := strings.TrimSuffix(prefix, "://")
				destination := strings.TrimPrefix(channel, prefix)
				subject = fmt.Sprintf("notification.%s.%s", protocol, destination)
				break
			}
		}
	}

	return subject
}
