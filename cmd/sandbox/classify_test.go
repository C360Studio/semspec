package main

import (
	"testing"
)

func TestClassifyExec(t *testing.T) {
	tests := []struct {
		name           string
		stderr         string
		exitCode       int
		timedOut       bool
		wantClass      ExecClassification
		wantMissing    string
	}{
		{
			name:      "success",
			exitCode:  0,
			wantClass: ClassSuccess,
		},
		{
			name:      "timeout",
			exitCode:  -1,
			timedOut:  true,
			wantClass: ClassTimeout,
		},
		{
			name:        "exit 127 bash command not found",
			stderr:      "bash: cargo: command not found",
			exitCode:    127,
			wantClass:   ClassCommandNotFound,
			wantMissing: "cargo",
		},
		{
			name:        "exit 127 sh not found",
			stderr:      "sh: 1: protoc: not found",
			exitCode:    127,
			wantClass:   ClassCommandNotFound,
			wantMissing: "protoc",
		},
		{
			name:        "exit 127 zsh command not found",
			stderr:      "zsh: command not found: flutter",
			exitCode:    127,
			wantClass:   ClassCommandNotFound,
			wantMissing: "flutter",
		},
		{
			name:        "exit 127 bin sh not found",
			stderr:      "/bin/sh: rustup: not found",
			exitCode:    127,
			wantClass:   ClassCommandNotFound,
			wantMissing: "rustup",
		},
		{
			name:        "exit 127 no recognizable pattern",
			stderr:      "some other error",
			exitCode:    127,
			wantClass:   ClassCommandNotFound,
			wantMissing: "", // can't extract command name
		},
		{
			name:      "exit 126 permission denied",
			stderr:    "bash: ./script.sh: Permission denied",
			exitCode:  126,
			wantClass: ClassPermissionDenied,
		},
		{
			name:      "generic permission denied in stderr",
			stderr:    "error: EACCES: permission denied, open '/etc/secret'",
			exitCode:  1,
			wantClass: ClassPermissionDenied,
		},
		{
			name:      "permission denied case insensitive",
			stderr:    "Permission Denied accessing file",
			exitCode:  1,
			wantClass: ClassPermissionDenied,
		},
		{
			name:        "command not found pattern with non-127 exit code",
			stderr:      "bash: jq: command not found",
			exitCode:    1,
			wantClass:   ClassCommandNotFound,
			wantMissing: "jq",
		},
		{
			name:      "timeout takes priority over exit code 0",
			exitCode:  0,
			timedOut:  true,
			wantClass: ClassTimeout,
		},
		{
			name:      "permission denied takes priority over command not found in stderr",
			stderr:    "permission denied\nbash: npm: command not found",
			exitCode:  1,
			wantClass: ClassPermissionDenied,
		},
		{
			name:      "generic failure",
			stderr:    "error: compilation failed",
			exitCode:  1,
			wantClass: ClassFailure,
		},
		{
			name:      "generic failure exit 2",
			stderr:    "usage error",
			exitCode:  2,
			wantClass: ClassFailure,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			class, missing := classifyExec(tt.stderr, tt.exitCode, tt.timedOut)
			if class != tt.wantClass {
				t.Errorf("classification = %q, want %q", class, tt.wantClass)
			}
			if missing != tt.wantMissing {
				t.Errorf("missingCommand = %q, want %q", missing, tt.wantMissing)
			}
		})
	}
}

func TestExtractMissingCommand(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   string
	}{
		{"bash style", "bash: cargo: command not found", "cargo"},
		{"sh style", "sh: 1: foo: not found", "foo"},
		{"zsh style", "zsh: command not found: bar", "bar"},
		{"/bin/sh style", "/bin/sh: protoc: not found", "protoc"},
		{"no match", "error: something else", ""},
		{"empty", "", ""},
		{"multiline with match", "running tests...\nbash: golangci-lint: command not found\nexit 127", "golangci-lint"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractMissingCommand(tt.stderr)
			if got != tt.want {
				t.Errorf("extractMissingCommand() = %q, want %q", got, tt.want)
			}
		})
	}
}
