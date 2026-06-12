package payloads

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestQARequestedPayloadValidate(t *testing.T) {
	base := func() *QARequestedPayload {
		return &QARequestedPayload{QARequestedEvent: workflow.QARequestedEvent{
			Slug:        "auth",
			PlanID:      "plan-1",
			Mode:        workflow.QALevelUnit,
			TestCommand: "go test ./...",
		}}
	}

	tests := []struct {
		name      string
		mutate    func(*QARequestedPayload)
		wantErr   bool
		errSubstr string
	}{
		{"valid_no_workspace", func(*QARequestedPayload) {}, false, ""},
		{"valid_with_workspace", func(p *QARequestedPayload) { p.Workspace = workflow.QAWorktreeID("auth") }, false, ""},
		{"empty_slug", func(p *QARequestedPayload) { p.Slug = "" }, true, "slug"},
		{"non_unit_mode", func(p *QARequestedPayload) { p.Mode = workflow.QALevelSynthesis }, true, "mode must be unit"},
		{"workspace_path_traversal_dots", func(p *QARequestedPayload) { p.Workspace = "../escape" }, true, "path separators"},
		{"workspace_slash", func(p *QARequestedPayload) { p.Workspace = "qa/auth" }, true, "path separators"},
		{"workspace_backslash", func(p *QARequestedPayload) { p.Workspace = "qa\\auth" }, true, "path separators"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := base()
			tt.mutate(p)
			err := p.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Validate() = nil, want error containing %q", tt.errSubstr)
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("Validate() error = %q, want substring %q", err.Error(), tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
		})
	}
}
