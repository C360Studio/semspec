// Package governance provides enforcement middleware for tool executors.
//
// The governance filter intercepts tool calls and enforces declared constraints
// before delegating to an inner executor. This creates a clear separation
// between policy enforcement (what a task is allowed to do) and implementation
// (how the tool performs the operation).
//
// # FileScopeFilter
//
// FileScopeFilter enforces file-scope constraints derived from TaskNode.FileScope.
// When a DAG task is dispatched, it carries a list of glob patterns declaring
// which files the task is allowed to modify. The filter wraps the inner executor
// and blocks any write operation targeting a path outside those patterns.
//
// Intercepted tools:
//   - file_write — blocked if the "path" argument is outside scope
//   - file_create — blocked if the "path" argument is outside scope
//   - file_delete — blocked if the "path" argument is outside scope
//
// Allowed through unconditionally:
//   - file_read — reads are not destructive; scope does not apply
//   - file_list — reads are not destructive; scope does not apply
//   - git_commit — pass-through; write-scope is already enforced upstream
//   - All other tools — pass-through to inner executor
//
// Empty file_scope blocks all write operations, acting as a deny-all policy.
// This is intentional: a task that declares no scope should not be allowed to
// write to any file.
//
// Glob matching uses doublestar semantics (** matches across path separators).
// Paths are matched as relative paths from the repository root. Absolute paths
// provided by the agent are converted to relative paths before matching.
package governance
