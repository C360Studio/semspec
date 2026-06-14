package structuralvalidator

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
)

// File-ownership containment gate (issues #175 / #177).
//
// A story may only MODIFY files it owns, and must not commit scratch artefacts.
// A file owned by NO story is written by every parallel story and produces an
// unmergeable conflict at assembly (the 2026-06-13 mavlink-hard README wedge);
// scratch (patch.diff, *.orig) rides `git add -A` onto the branch and collides
// the same way.
//
// The check computes the AUTHORITATIVE changed set from `git status` in the
// worktree rather than trusting the agent's self-reported FilesModified — the
// whole point of the gate is to catch the agent overstepping, so its own claim
// can't be the source of truth. It runs BEFORE submit_work commits, so the
// untracked scratch that `git add -A` is about to stage is still visible as `??`.
//
// The pure core (parsePorcelain / isJunkArtifact / decideOwnership) holds no IO
// and is the unit-test surface.

// porcelainEntry is one parsed line of `git status --porcelain=v1`.
type porcelainEntry struct {
	Path        string // current path (rename destination for R/C entries)
	IndexStatus byte   // X column
	WorkStatus  byte   // Y column
}

// isNew reports whether the entry is a file the dev CREATED (untracked or
// index-added) rather than a modification of a pre-existing tracked file.
func (e porcelainEntry) isNew() bool {
	return (e.IndexStatus == '?' && e.WorkStatus == '?') || e.IndexStatus == 'A'
}

// ownershipVerdict is the classification of a worktree change set against the
// story's owned file set.
//
// The hard-fail vs advisory line is drawn at MERGEABLE vs UNMERGEABLE, not
// new-vs-modified. The assembly wedge is caused by a shared DOC co-written by
// parallel stories (README + the coverage matrix — line-level conflicts that
// cannot auto-merge). A non-owner bumping build.gradle / go.mod or editing a
// pre-existing test usually merges cleanly and is a routine TDD move, so it is
// advisory (surfaced to the reviewer; #176 fails honestly if it does conflict).
type ownershipVerdict struct {
	// JunkViolations are committed scratch/merge artefacts (hard fail). Each
	// element is "path (pattern)".
	JunkViolations []string
	// ModifiedDocUnowned are pre-existing DOCUMENTATION files a non-owner
	// modified (hard fail — the unmergeable co-write that wedged the
	// 2026-06-13 README assembly).
	ModifiedDocUnowned []string
	// ModifiedUnowned are pre-existing non-doc files a non-owner modified
	// (advisory — source/build edits usually merge; surfaced, not blocked).
	ModifiedUnowned []string
	// NewUnowned are newly-created files outside the owned set (advisory — a dev
	// legitimately splitting a class into a new file lands here; too noisy to
	// hard-fail).
	NewUnowned []string
}

// clean reports whether there are no hard-fail violations.
func (v ownershipVerdict) clean() bool {
	return len(v.JunkViolations) == 0 && len(v.ModifiedDocUnowned) == 0
}

// parsePorcelain parses `git status --porcelain=v1` output into entries. It is
// tolerant of trailing newlines and blank lines. Renames/copies report the
// destination path (the part after " -> "). Double-quoted paths (git quotes
// paths with special chars) are unquoted best-effort.
func parsePorcelain(stdout string) []porcelainEntry {
	var out []porcelainEntry
	for _, line := range strings.Split(stdout, "\n") {
		// A porcelain v1 line is "XY PATH"; minimum length 4 ("XY x").
		if len(line) < 4 {
			continue
		}
		x, y := line[0], line[1]
		rest := line[3:] // skip the single separating space at index 2
		if i := strings.Index(rest, " -> "); i >= 0 {
			rest = rest[i+len(" -> "):]
		}
		rest = strings.TrimSpace(rest)
		if strings.HasPrefix(rest, `"`) && strings.HasSuffix(rest, `"`) && len(rest) >= 2 {
			rest = rest[1 : len(rest)-1]
		}
		if rest == "" {
			continue
		}
		out = append(out, porcelainEntry{Path: rest, IndexStatus: x, WorkStatus: y})
	}
	return out
}

// isJunkArtifact reports whether a path is a scratch/merge artefact that must
// never be committed (issue #177). Matched on the basename. Deliberately narrow
// — only non-source artefact shapes — so it never collides with a legitimate
// source or test file (those are handled by the modified/new-unowned rules).
func isJunkArtifact(p string) (pattern string, ok bool) {
	base := path.Base(strings.ReplaceAll(p, "\\", "/"))
	switch {
	case strings.HasSuffix(base, ".orig"):
		return "*.orig", true
	case strings.HasSuffix(base, ".rej"):
		return "*.rej", true
	case strings.HasSuffix(base, ".patch"):
		return "*.patch", true
	case strings.HasSuffix(base, ".bak"):
		return "*.bak", true
	case strings.HasSuffix(base, ".tmp"):
		return "*.tmp", true
	case strings.HasSuffix(base, ".swp"):
		return "*.swp", true
	case strings.HasSuffix(base, "~"):
		return "*~", true
	case strings.HasPrefix(base, "patch") && strings.HasSuffix(base, ".diff"):
		// patch.diff, patch2.diff — saved `git diff` output committed at root.
		return "patch*.diff", true
	default:
		return "", false
	}
}

// decideOwnership classifies a worktree change set against the owned file set.
// Pure: no git, no IO. owned is the normalized set of Story.FilesOwned.
func decideOwnership(changes []porcelainEntry, owned map[string]struct{}) ownershipVerdict {
	var v ownershipVerdict
	for _, c := range changes {
		norm := workflow.NormalizeFilePath(c.Path)
		if norm == "" {
			continue
		}
		if pattern, ok := isJunkArtifact(norm); ok {
			v.JunkViolations = append(v.JunkViolations, fmt.Sprintf("%s (%s)", norm, pattern))
			continue
		}
		if _, isOwned := owned[norm]; isOwned {
			continue
		}
		switch {
		case c.isNew():
			v.NewUnowned = append(v.NewUnowned, norm)
		case workflow.IsDocumentationPath(norm):
			// A non-owner co-writing a shared doc is the unmergeable shape.
			v.ModifiedDocUnowned = append(v.ModifiedDocUnowned, norm)
		default:
			v.ModifiedUnowned = append(v.ModifiedUnowned, norm)
		}
	}
	return v
}

// runFileOwnershipContainment computes the worktree's actual change set via
// `git status --porcelain` (run in workDir via runner) and returns up to two
// CheckResults: a Required containment verdict (junk + modified-unowned) and an
// advisory new-files list. When owned is empty the gate is skipped (manual
// validation / E2E / any dispatch without story context). A git failure is
// surfaced as a non-blocking advisory rather than a hard fail — the gate must
// not wedge a run on a plumbing hiccup.
func (e *Executor) runFileOwnershipContainment(ctx context.Context, owned []string, workDir string, runner CommandRunner) []payloads.CheckResult {
	const checkName = "file-ownership-containment"

	// No story ownership context (manual validation / E2E / any dispatch
	// without FilesOwned) — the gate does not apply. Return no check row so it
	// is invisible to ChecksRun rather than a spurious always-pass result.
	if len(owned) == 0 {
		return nil
	}

	ownedSet := make(map[string]struct{}, len(owned))
	for _, f := range owned {
		ownedSet[f] = struct{}{}
	}

	start := time.Now()
	// --untracked-files=all reports untracked-but-not-ignored files. Scratch
	// that a repo's .gitignore already covers (the fixtures' *.orig/patch.diff
	// patterns) is therefore invisible here — which is correct: git add -A also
	// honours .gitignore, so that scratch never reaches the commit. The junk arm
	// below is thus the PORTABLE FALLBACK for repos that lack such a .gitignore
	// (e.g. a fresh user project), not a duplicate of the fixture gitignore.
	cmd := "git status --porcelain=v1 --untracked-files=all"
	stdout, stderr, exitCode, err := runner.Run(ctx, cmd, workDir, 15*time.Second)
	duration := time.Since(start).String()

	if err != nil || exitCode != 0 {
		// Don't block on a git plumbing failure — advisory only.
		detail := strings.TrimSpace(stderr)
		if err != nil {
			detail = err.Error()
		}
		return []payloads.CheckResult{{
			Name:     checkName,
			Passed:   true,
			Required: false,
			Command:  cmd,
			ExitCode: exitCode,
			Stdout:   fmt.Sprintf("could not compute change set (git status failed): %s — containment gate skipped", detail),
			Duration: duration,
		}}
	}

	verdict := decideOwnership(parsePorcelain(stdout), ownedSet)

	var results []payloads.CheckResult

	// Required gate: scratch artefacts + a non-owner co-writing a shared doc
	// (the unmergeable shapes that wedge assembly).
	containment := payloads.CheckResult{
		Name:     checkName,
		Passed:   verdict.clean(),
		Required: true,
		Command:  cmd,
		Duration: duration,
	}
	if verdict.clean() {
		containment.Stdout = fmt.Sprintf("no scratch artefacts committed and no shared docs co-written outside the story's %d owned path(s)", len(owned))
	} else {
		var b strings.Builder
		if len(verdict.JunkViolations) > 0 {
			fmt.Fprintf(&b, "Scratch/merge artefacts must not be committed (write scratch to /tmp, not the worktree): %s. ", strings.Join(verdict.JunkViolations, ", "))
		}
		if len(verdict.ModifiedDocUnowned) > 0 {
			fmt.Fprintf(&b, "Modified shared documentation this story does not own: %s. A doc owned by no story (or by another story) is co-written by parallel stories and cannot be merged at assembly — only edit docs in your file scope; if a deliverable doc isn't yours, surface it as a planning gap. ", strings.Join(verdict.ModifiedDocUnowned, ", "))
		}
		fmt.Fprintf(&b, "Owned paths: %s.", strings.Join(owned, ", "))
		containment.ExitCode = 1
		containment.Stderr = b.String()
	}
	results = append(results, containment)

	// Advisory: non-doc files modified outside ownership (usually mergeable
	// build/source edits) + new files outside the owned set (legitimate class
	// splits). Surfaced to the reviewer, never hard-fails.
	advisory := append(append([]string(nil), verdict.ModifiedUnowned...), verdict.NewUnowned...)
	if len(advisory) > 0 {
		results = append(results, payloads.CheckResult{
			Name:     "file-ownership-advisory",
			Passed:   false,
			Required: false,
			Command:  cmd,
			Stdout: fmt.Sprintf("touched %d file(s) outside the declared file scope (advisory — confirm these belong to this story; a conflict at assembly will fail the plan honestly): %s",
				len(advisory), strings.Join(advisory, ", ")),
			Duration: duration,
		})
	}

	return results
}
