package main

import "testing"

func TestSelectQAWorkDir(t *testing.T) {
	const repoPath = "/repo"

	// resolve mirrors Server.worktreeFor: returns the worktree path when it
	// exists, "" otherwise.
	resolvePresent := func(string) string { return "/repo/.semspec/worktrees/qa-auth" }
	resolveMissing := func(string) string { return "" }

	tests := []struct {
		name         string
		workspace    string
		resolve      func(string) string
		wantDir      string
		wantFellBack bool
	}{
		{
			name:         "no_workspace_uses_repo_root",
			workspace:    "",
			resolve:      resolveMissing, // must not even be consulted
			wantDir:      repoPath,
			wantFellBack: false,
		},
		{
			name:         "workspace_present_uses_worktree",
			workspace:    "qa-auth",
			resolve:      resolvePresent,
			wantDir:      "/repo/.semspec/worktrees/qa-auth",
			wantFellBack: false,
		},
		{
			name:         "workspace_missing_falls_back_and_flags",
			workspace:    "qa-auth",
			resolve:      resolveMissing,
			wantDir:      repoPath,
			wantFellBack: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, fellBack := selectQAWorkDir(repoPath, tt.workspace, tt.resolve)
			if dir != tt.wantDir {
				t.Errorf("dir = %q, want %q", dir, tt.wantDir)
			}
			if fellBack != tt.wantFellBack {
				t.Errorf("fellBack = %v, want %v", fellBack, tt.wantFellBack)
			}
		})
	}
}
