package answerer

import (
	"testing"
	"time"
)

func TestParseNotificationSubject(t *testing.T) {
	tests := []struct {
		channel string
		want    string
	}{
		{"slack://general", "notification.slack.general"},
		{"slack://security-alerts", "notification.slack.security-alerts"},
		{"email://team@example.com", "notification.email.team@example.com"},
		{"webhook://https://hooks.example.com/123", "notification.webhook.https://hooks.example.com/123"},
		{"unknown", "notification.generic"},
		{"", "notification.generic"},
	}

	for _, tt := range tests {
		t.Run(tt.channel, func(t *testing.T) {
			got := parseNotificationSubject(tt.channel)
			if got != tt.want {
				t.Errorf("parseNotificationSubject(%q) = %q, want %q", tt.channel, got, tt.want)
			}
		})
	}
}

func TestQuestionAnswerTaskSerialization(t *testing.T) {
	task := QuestionAnswerTask{
		TaskID:     "task-123",
		QuestionID: "q-abc",
		Topic:      "architecture.database",
		Question:   "Should we use PostgreSQL?",
		Context:    "Building a new service",
		Capability: "planning",
		AgentName:  "architect",
		SLA:        time.Hour,
		CreatedAt:  time.Now(),
	}

	if task.TaskID != "task-123" {
		t.Errorf("TaskID = %v, want task-123", task.TaskID)
	}
	if task.Capability != "planning" {
		t.Errorf("Capability = %v, want planning", task.Capability)
	}
}

func TestToolAnswerTaskSerialization(t *testing.T) {
	task := ToolAnswerTask{
		TaskID:     "task-456",
		QuestionID: "q-def",
		Topic:      "knowledge.nats",
		Question:   "What is the NATS subject pattern?",
		ToolName:   "web-search",
		CreatedAt:  time.Now(),
	}

	if task.ToolName != "web-search" {
		t.Errorf("ToolName = %v, want web-search", task.ToolName)
	}
}

func TestQuestionNotificationSerialization(t *testing.T) {
	notification := QuestionNotification{
		QuestionID: "q-789",
		Topic:      "api.semstreams",
		Question:   "Does LoopInfo include workflow_slug?",
		Event:      "new_question",
		Channel:    "slack://semstreams-team",
		Timestamp:  time.Now(),
	}

	if notification.Event != "new_question" {
		t.Errorf("Event = %v, want new_question", notification.Event)
	}
}

func TestRouteResultMessage(t *testing.T) {
	tests := []struct {
		answererType AnswererType
		answerer     string
		wantContains string
	}{
		{AnswererAgent, "agent/architect", "architect"},
		{AnswererTeam, "team/semstreams", "semstreams"},
		{AnswererHuman, "human/requester", "requester"},
		{AnswererTool, "tool/web-search", "web-search"},
	}

	for _, tt := range tests {
		t.Run(string(tt.answererType), func(t *testing.T) {
			name := GetAnswererName(tt.answerer)
			if name != tt.wantContains {
				t.Errorf("GetAnswererName(%q) = %q, want %q", tt.answerer, name, tt.wantContains)
			}
		})
	}
}
