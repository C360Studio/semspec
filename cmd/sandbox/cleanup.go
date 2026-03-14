package main

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// CleanupLoop runs periodic cleanup of stale worktrees that have not been
// modified within maxAge. It stops when ctx is cancelled.
func (s *Server) CleanupLoop(ctx context.Context, interval, maxAge time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanupStaleWorktrees(ctx, maxAge)
		}
	}
}

// cleanupStaleWorktrees removes worktrees whose directory has not been
// modified (mtime) within maxAge. This handles the case where a task crashes
// before calling DELETE /worktree.
func (s *Server) cleanupStaleWorktrees(ctx context.Context, maxAge time.Duration) {
	entries, err := os.ReadDir(s.worktreeRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		s.logger.Error("cleanup: failed to read worktree root", "path", s.worktreeRoot, "error", err)
		return
	}

	cutoff := time.Now().Add(-maxAge)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			s.logger.Warn("cleanup: failed to stat entry", "name", entry.Name(), "error", err)
			continue
		}

		if info.ModTime().After(cutoff) {
			// Recently modified — leave it alone.
			continue
		}

		taskID := entry.Name()
		worktreePath := filepath.Join(s.worktreeRoot, taskID)

		s.logger.Info("cleanup: removing stale worktree", "task_id", taskID, "age", time.Since(info.ModTime()).Round(time.Minute))

		if err := s.removeWorktree(ctx, worktreePath); err != nil {
			s.logger.Error("cleanup: failed to remove stale worktree", "task_id", taskID, "error", err)
			continue
		}

		// Delete the agent branch — best-effort.
		if err := runGit(ctx, s.repoPath, "branch", "-D", "agent/"+taskID); err != nil {
			s.logger.Warn("cleanup: failed to delete stale branch", "task_id", taskID, "error", err)
		}
	}
}
