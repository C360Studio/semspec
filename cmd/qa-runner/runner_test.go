package main

import (
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
)

// newTestHandler returns a qaHandler wired with defaults suitable for pure-function tests.
func newTestHandler(projectHostPath string, defaultTimeout time.Duration) *qaHandler {
	return &qaHandler{
		projectHostPath: projectHostPath,
		defaultTimeout:  defaultTimeout,
		logger:          slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}
}

func TestResolveWorkspace(t *testing.T) {
	tests := []struct {
		name            string
		projectHostPath string
		evtHostPath     string
		wantPath        string
		wantErrContains string
	}{
		{
			name:        "event path wins when set",
			evtHostPath: "/workspaces/from-event",
			wantPath:    "/workspaces/from-event",
		},
		{
			name:            "handler fallback when event is empty",
			projectHostPath: "/workspaces/fallback",
			evtHostPath:     "",
			wantPath:        "/workspaces/fallback",
		},
		{
			name:            "event path wins over handler fallback",
			projectHostPath: "/workspaces/fallback",
			evtHostPath:     "/workspaces/event",
			wantPath:        "/workspaces/event",
		},
		{
			name:            "no path available surfaces runner error",
			projectHostPath: "",
			evtHostPath:     "",
			wantErrContains: "workspace_host_path not resolvable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(tt.projectHostPath, 0)
			gotPath, gotErr := h.resolveWorkspace(workflow.QARequestedEvent{WorkspaceHostPath: tt.evtHostPath})
			if gotPath != tt.wantPath {
				t.Errorf("path = %q, want %q", gotPath, tt.wantPath)
			}
			if tt.wantErrContains == "" && gotErr != "" {
				t.Errorf("unexpected error: %q", gotErr)
			}
			if tt.wantErrContains != "" && !strings.Contains(gotErr, tt.wantErrContains) {
				t.Errorf("error %q missing %q", gotErr, tt.wantErrContains)
			}
		})
	}
}

func TestResolveTimeout(t *testing.T) {
	tests := []struct {
		name           string
		defaultTimeout time.Duration
		evtSeconds     int
		want           time.Duration
	}{
		{
			name:       "event override wins over default",
			evtSeconds: 30,
			want:       30 * time.Second,
		},
		{
			name:           "event override wins even when larger than default",
			defaultTimeout: 10 * time.Minute,
			evtSeconds:     3600,
			want:           time.Hour,
		},
		{
			name:           "handler default when event unset",
			defaultTimeout: 5 * time.Minute,
			evtSeconds:     0,
			want:           5 * time.Minute,
		},
		{
			name:           "package default when nothing set",
			defaultTimeout: 0,
			evtSeconds:     0,
			want:           actDefaultTimeout,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler("", tt.defaultTimeout)
			got := h.resolveTimeout(workflow.QARequestedEvent{TimeoutSeconds: tt.evtSeconds})
			if got != tt.want {
				t.Errorf("timeout = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestBuildFailures(t *testing.T) {
	tests := []struct {
		name       string
		passed     bool
		output     string
		exitCode   int
		timedOut   bool
		timeout    time.Duration
		wantCount  int
		wantMsgSub string
		wantLogLen int // -1 means don't check
	}{
		{
			name:       "passing run has no failures",
			passed:     true,
			output:     "PASS",
			wantCount:  0,
			wantLogLen: -1,
		},
		{
			name:       "non-zero exit produces one failure with excerpt",
			passed:     false,
			output:     "FAIL\nstack trace here",
			exitCode:   1,
			wantCount:  1,
			wantMsgSub: "code 1",
			wantLogLen: len("FAIL\nstack trace here"),
		},
		{
			name:       "timeout produces timeout failure",
			passed:     false,
			output:     "partial output before timeout",
			timedOut:   true,
			timeout:    30 * time.Second,
			wantCount:  1,
			wantMsgSub: "timed out after 30s",
			wantLogLen: -1, // don't check — excerpt mirrors full short output
		},
		{
			name:       "long output is capped to tail",
			passed:     false,
			output:     strings.Repeat("X", actLogExcerptBytes+500),
			exitCode:   1,
			wantCount:  1,
			wantLogLen: actLogExcerptBytes,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildFailures(tt.passed, tt.output, tt.exitCode, tt.timedOut, tt.timeout)
			if len(got) != tt.wantCount {
				t.Fatalf("got %d failures, want %d", len(got), tt.wantCount)
			}
			if tt.wantCount == 0 {
				return
			}
			f := got[0]
			if f.JobName != "act" {
				t.Errorf("JobName = %q, want %q", f.JobName, "act")
			}
			if tt.wantMsgSub != "" && !strings.Contains(f.Message, tt.wantMsgSub) {
				t.Errorf("Message %q missing %q", f.Message, tt.wantMsgSub)
			}
			if tt.wantLogLen >= 0 && len(f.LogExcerpt) != tt.wantLogLen {
				t.Errorf("LogExcerpt length = %d, want %d", len(f.LogExcerpt), tt.wantLogLen)
			}
		})
	}
}

func TestInferArtifactType(t *testing.T) {
	tests := []struct {
		ext  string
		want string
	}{
		{".png", "screenshot"},
		{".PNG", "screenshot"},
		{".jpg", "screenshot"},
		{".jpeg", "screenshot"},
		{".gif", "screenshot"},
		{".webp", "screenshot"},
		{".zip", "trace"},
		{".tar", "trace"},
		{".gz", "trace"},
		{".tgz", "trace"},
		{".out", "coverage"},
		{".cov", "coverage"},
		{".log", "log"},
		{".txt", "log"},
		{"", "log"},
		{".exe", "log"},
	}
	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			got := inferArtifactType(tt.ext)
			if got != tt.want {
				t.Errorf("inferArtifactType(%q) = %q, want %q", tt.ext, got, tt.want)
			}
		})
	}
}

func TestActCappedWriter_UnderLimit(t *testing.T) {
	w := &actCappedWriter{limit: 100}
	_, _ = w.Write([]byte("hello"))
	_, _ = w.Write([]byte(" world"))
	got := w.String()
	if got != "hello world" {
		t.Errorf("String() = %q, want %q", got, "hello world")
	}
	if w.capped {
		t.Error("capped should be false when under limit")
	}
}

func TestActCappedWriter_AtLimit(t *testing.T) {
	w := &actCappedWriter{limit: 5}
	_, _ = w.Write([]byte("hello"))
	got := w.String()
	if got != "hello" {
		t.Errorf("String() = %q, want %q", got, "hello")
	}
}

func TestActCappedWriter_OverLimit(t *testing.T) {
	w := &actCappedWriter{limit: 5}
	n, err := w.Write([]byte("helloworld"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 10 {
		t.Errorf("Write returned n=%d, want 10 (full byte count even when truncated)", n)
	}
	got := w.String()
	if !strings.HasPrefix(got, "hello") {
		t.Errorf("buffer should start with %q, got %q", "hello", got)
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("expected truncation marker in output, got %q", got)
	}
	if !w.capped {
		t.Error("capped should be true after exceeding limit")
	}
}

func TestActCappedWriter_SubsequentWritesAfterCap(t *testing.T) {
	w := &actCappedWriter{limit: 5}
	_, _ = w.Write([]byte("helloworld"))
	before := w.String()
	_, _ = w.Write([]byte("more data"))
	after := w.String()
	if before != after {
		t.Errorf("buffer changed after cap: %q vs %q", before, after)
	}
}
