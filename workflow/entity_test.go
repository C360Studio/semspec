package workflow

import "testing"

func TestExtractSlugFromTaskID(t *testing.T) {
	tests := []struct {
		name     string
		taskID   string
		wantSlug string
	}{
		{
			name:     "valid single-word slug",
			taskID:   "c360.semspec.workflow.task.task.my-plan-1",
			wantSlug: "my-plan",
		},
		{
			name:     "valid multi-word slug",
			taskID:   "c360.semspec.workflow.task.task.add-auth-refresh-3",
			wantSlug: "add-auth-refresh",
		},
		{
			name:     "valid long slug with sequence 10",
			taskID:   "c360.semspec.workflow.task.task.add-a-goodbye-endpoint-that-returns-a-goodbye-mess-10",
			wantSlug: "add-a-goodbye-endpoint-that-returns-a-goodbye-mess",
		},
		{
			name:     "valid sequence 1",
			taskID:   "c360.semspec.workflow.task.task.simple-1",
			wantSlug: "simple",
		},
		{
			name:     "empty string",
			taskID:   "",
			wantSlug: "",
		},
		{
			name:     "wrong prefix",
			taskID:   "c360.semspec.workflow.plan.plan.my-plan",
			wantSlug: "",
		},
		{
			name:     "random string",
			taskID:   "random-string",
			wantSlug: "",
		},
		{
			name:     "prefix only",
			taskID:   "c360.semspec.workflow.task.task.",
			wantSlug: "",
		},
		{
			name:     "no sequence number",
			taskID:   "c360.semspec.workflow.task.task.my-plan",
			wantSlug: "",
		},
		{
			name:     "trailing hyphen no digits",
			taskID:   "c360.semspec.workflow.task.task.my-plan-",
			wantSlug: "",
		},
		{
			name:     "non-digit sequence",
			taskID:   "c360.semspec.workflow.task.task.my-plan-abc",
			wantSlug: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractSlugFromTaskID(tt.taskID)
			if got != tt.wantSlug {
				t.Errorf("ExtractSlugFromTaskID(%q) = %q, want %q", tt.taskID, got, tt.wantSlug)
			}
		})
	}
}
