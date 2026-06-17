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
// story's owned file set. The owned set is expanded with deterministic
// companion test paths before classification, so a Story that owns
// src/main/java/.../Foo.java may create src/test/java/.../FooTest.java without
// being treated as out-of-territory.
//
// The hard-fail vs advisory line is drawn at MERGEABLE vs UNMERGEABLE, not
// new-vs-modified. Two unmergeable shapes hard-fail:
//   - a shared DOC co-written by parallel stories (the 2026-06-13 README wedge);
//   - a NEW source/test file created OUTSIDE the story's declared territory
//     (the 2026-06-14 wedge — four parallel stories each created the same
//     fabricated-path driver class and collided at terminal assembly). See
//     ADR-049: this is a planning/ownership gap, routed to recovery, not a
//     developer error.
//
// A non-owner bumping build.gradle / go.mod, editing a pre-existing test, or
// splitting a class into a new file WITHIN its declared package usually merges
// cleanly and is a routine TDD move, so it is advisory (surfaced to the
// reviewer; #176 fails honestly if it does conflict).
type ownershipVerdict struct {
	// JunkViolations are committed scratch/merge artefacts (hard fail). Each
	// element is "path (pattern)".
	JunkViolations []string
	// ModifiedDocUnowned are pre-existing DOCUMENTATION files a non-owner
	// modified (hard fail — the unmergeable co-write that wedged the
	// 2026-06-13 README assembly).
	ModifiedDocUnowned []string
	// NewUnownedOutOfTerritory are newly-created source/test files OUTSIDE the
	// story's declared territory — the directory prefixes of its owned files
	// (hard fail, ADR-049 move 3). A file the implementation requires that lies
	// outside every owned package is the shape parallel stories independently
	// converge on and collide over at assembly; it signals the architect's
	// partition or the story's FilesOwned is wrong.
	NewUnownedOutOfTerritory []string
	// NewTopologyControlled are newly-created build/workspace/package manifests
	// or standalone-project files outside the story's declared ownership. These
	// change repository topology and are a planning gap, not an in-loop class
	// split or harmless advisory artifact.
	NewTopologyControlled []string
	// ModifiedUnowned are pre-existing non-doc files a non-owner modified
	// (advisory — source/build edits usually merge; surfaced, not blocked).
	ModifiedUnowned []string
	// NewUnowned are newly-created files INSIDE the owned territory (advisory —
	// a dev legitimately splitting a class into a new file in its own package
	// lands here) plus new docs outside ownership (hygiene-only); too noisy to
	// hard-fail.
	NewUnowned []string
	// RootScratch are newly-created SOURCE files at the worktree ROOT (no package
	// directory) — e.g. a dev's throwaway FindClass.java probe. A real source/
	// test deliverable always lives in a package dir, so a root-level source file
	// is never a deliverable; but it is NOT a planning gap (it doesn't signal a
	// wrong partition) and must NOT be left advisory either, because the commit
	// path stages everything (`git add -A`) and it would silently pollute the
	// branch. Hard fail routed to DEV-RETRY: the developer must remove it.
	RootScratch []string
}

// clean reports whether there are no hard-fail violations (scratch artefacts,
// a shared-doc co-write, an out-of-territory new source/test file, or
// root-level source scratch the dev must clean up).
func (v ownershipVerdict) clean() bool {
	return len(v.JunkViolations) == 0 &&
		len(v.ModifiedDocUnowned) == 0 &&
		len(v.NewUnownedOutOfTerritory) == 0 &&
		len(v.NewTopologyControlled) == 0 &&
		len(v.RootScratch) == 0
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

// isIgnorableBuildArtifact reports whether a path is a transient byproduct the
// TOOLCHAIN (not the developer) emits, which must never be treated as a
// deliverable, a planning gap, or even committable scratch. The canonical case:
// javac writes a timestamped @argfile (javac.<ts>.args) when the compile
// classpath is long — which the osh-core source composite (ADR build-self-
// containment) makes very long. These appear as untracked `??` files and would
// otherwise be misclassified as an out-of-territory new source file (ADR-049
// planning gap), wedging the node — a developer cannot "stop creating" a file
// the build regenerates every compile, so it must be IGNORED, not hard-failed.
// (They are also .gitignored in the fixtures so `git add -A` never commits them;
// this is the portable fallback for repos lacking that ignore.)
func isIgnorableBuildArtifact(p string) bool {
	norm := strings.ReplaceAll(p, "\\", "/")
	base := path.Base(norm)
	if !strings.HasSuffix(base, ".args") {
		return false
	}
	// Narrow to TOOL-generated @argfiles only — never a deliverable. A legitimate
	// tracked/owned `*.args` (e.g. src/test/resources/cli.args) must NOT match, or
	// it would silently vanish from both validation and the commit.
	//   - JDK tools write "<tool>.<timestamp>.args": javac./jar./javadoc.
	//   - Gradle/Maven write @argfiles under a build/ output directory.
	switch {
	case strings.HasPrefix(base, "javac."),
		strings.HasPrefix(base, "jar."),
		strings.HasPrefix(base, "javadoc."):
		return true
	case strings.HasPrefix(norm, "build/"), strings.Contains(norm, "/build/"):
		return true
	default:
		return false
	}
}

// sourceFileExts are extensions for compiled/interpreted source that must live
// in a package/module directory — a file with one of these at the worktree root
// is never a deliverable, only dev scratch (e.g. a FindClass.java probe).
var sourceFileExts = map[string]struct{}{
	".java": {}, ".kt": {}, ".kts": {}, ".scala": {}, ".groovy": {},
	".go": {}, ".py": {}, ".rb": {}, ".rs": {}, ".ts": {}, ".js": {},
	".c": {}, ".cc": {}, ".cpp": {}, ".cxx": {}, ".h": {}, ".hpp": {}, ".cs": {},
}

// isSourceFile reports whether the basename has a source-code extension.
func isSourceFile(p string) bool {
	_, ok := sourceFileExts[strings.ToLower(path.Ext(path.Base(p)))]
	return ok
}

// firstSegment returns the top-level path component, or "" for a root-level file
// with no directory (e.g. "FindClass.java" → "", "src/main/java/X.java" → "src").
func firstSegment(p string) string {
	p = strings.TrimPrefix(strings.ReplaceAll(p, "\\", "/"), "./")
	if i := strings.Index(p, "/"); i >= 0 {
		return p[:i]
	}
	return ""
}

// ownedTerritory returns the set of directory prefixes the story owns, derived
// from the directories of its owned files. A new file under one of these
// directories is a legitimate in-package split; a new file outside all of them
// is an ownership gap (ADR-049): the 2026-06-14 wedge declared
// .../driver/mavsdk/ but the developer (correctly) wrote .../impl/sensor/mavsdk/.
func ownedTerritory(owned map[string]struct{}) map[string]struct{} {
	territory := make(map[string]struct{}, len(owned))
	for f := range owned {
		territory[path.Dir(f)] = struct{}{}
	}
	return territory
}

// withinTerritory reports whether a normalized file path sits in or below one of
// the owned directories. A root-owned file ("." territory) matches only other
// root-level files, never a nested path.
func withinTerritory(norm string, territory map[string]struct{}) bool {
	d := path.Dir(norm)
	for t := range territory {
		if d == t || strings.HasPrefix(d, t+"/") {
			return true
		}
	}
	return false
}

// decideOwnership classifies a worktree change set against the owned file set.
// Pure: no git, no IO. owned is the normalized set of Story.FilesOwned.
func decideOwnership(changes []porcelainEntry, owned map[string]struct{}) ownershipVerdict {
	var v ownershipVerdict
	owned = expandOwnedMapWithCompanionTests(owned)
	territory := ownedTerritory(owned)
	for _, c := range changes {
		norm := workflow.NormalizeFilePath(c.Path)
		if norm == "" {
			continue
		}
		// Transient toolchain byproducts (javac.<ts>.args et al.) are neither
		// deliverables, junk-to-clean, nor planning gaps — a dev can't stop the
		// build regenerating them. Ignore before any classification.
		if isIgnorableBuildArtifact(norm) {
			continue
		}
		if pattern, ok := isJunkArtifact(norm); ok {
			v.JunkViolations = append(v.JunkViolations, fmt.Sprintf("%s (%s)", norm, pattern))
			continue
		}
		if _, isOwned := owned[norm]; isOwned {
			continue
		}
		// A `git mv` rename/copy reports only the DESTINATION path here and is
		// classified by the modified arms below (advisory for non-doc) — the
		// out-of-territory hard-fail targets NEW creation (`??`/`A`), which is
		// the observed 2026-06-14 wedge (devs WRITE the canonical entry class,
		// they do not rename into it). Renaming into a fabricated canonical path
		// is a theoretical gap left to the assembly-merge backstop (#176).
		switch {
		case c.isNew():
			switch {
			case workflow.IsTopologyControlledPath(norm):
				// Creating a new build/workspace/package root outside declared
				// ownership is a topology planning gap. Do this before the root
				// non-source advisory branch so settings.gradle/package.json/
				// gradlew cannot sneak through as "just a config file".
				v.NewTopologyControlled = append(v.NewTopologyControlled, norm)
			case workflow.IsDocumentationPath(norm):
				// A NEW unowned doc is hygiene-only (an undeclared coverage
				// matrix / scratch note); advisory, surfaced to the reviewer.
				v.NewUnowned = append(v.NewUnowned, norm)
			case len(territory) == 0 || withinTerritory(norm, territory):
				// No declared territory (manual / E2E dispatch with no story
				// context — the production caller skips the gate entirely in this
				// case, so this only guards direct callers), or an in-package
				// class split: advisory, matching pre-ADR-049 behavior.
				v.NewUnowned = append(v.NewUnowned, norm)
			case firstSegment(norm) == "" && isSourceFile(norm):
				// A new SOURCE file at the worktree ROOT (no package directory) — a
				// dev's throwaway probe (FindClass.java). Never a deliverable (source
				// must live in a package dir) and never a planning gap (it doesn't
				// signal a wrong partition), but it must NOT be left advisory: the
				// commit path stages everything (`git add -A`), so an advisory file
				// silently pollutes the branch. Hard fail routed to dev-retry so the
				// developer removes it.
				v.RootScratch = append(v.RootScratch, norm)
			case firstSegment(norm) == "":
				// A new NON-source file at the root (a stray note, an undeclared
				// root config) — not a deliverable parallel stories converge on, and
				// not unambiguously scratch. Advisory, surfaced to the reviewer.
				v.NewUnowned = append(v.NewUnowned, norm)
			default:
				// A new source/test file in a package directory but OUTSIDE the
				// declared territory is the 2026-06-14 wedge: parallel stories
				// converge on the same fabricated DELIVERABLE path and collide at
				// assembly. Hard fail so the planning/ownership gap surfaces in
				// seconds (ADR-049 move 3).
				v.NewUnownedOutOfTerritory = append(v.NewUnownedOutOfTerritory, norm)
			}
		case workflow.IsDocumentationPath(norm):
			// A non-owner co-writing a shared doc is the unmergeable shape.
			v.ModifiedDocUnowned = append(v.ModifiedDocUnowned, norm)
		default:
			v.ModifiedUnowned = append(v.ModifiedUnowned, norm)
		}
	}
	return v
}

func expandOwnedMapWithCompanionTests(owned map[string]struct{}) map[string]struct{} {
	if len(owned) == 0 {
		return owned
	}
	paths := make([]string, 0, len(owned))
	for f := range owned {
		paths = append(paths, f)
	}
	expanded := workflow.ExpandFileScopeWithCompanionTests(paths)
	out := make(map[string]struct{}, len(expanded))
	for _, f := range expanded {
		out[f] = struct{}{}
	}
	return out
}

// runFileOwnershipContainment computes the worktree's actual change set via
// `git status --porcelain` (run in workDir via runner) and returns up to two
// CheckResults: a Required containment verdict (junk + modified-unowned) and an
// advisory new-files list. When owned is empty the gate is skipped (manual
// validation / E2E / any dispatch without story context). A git failure is
// surfaced as a non-blocking advisory rather than a hard fail — the gate must
// not wedge a run on a plumbing hiccup.
func (e *Executor) runFileOwnershipContainment(ctx context.Context, owned []string, workDir string, runner CommandRunner) []payloads.CheckResult {
	const checkName = payloads.CheckFileOwnershipContainment

	// No story ownership context (manual validation / E2E / any dispatch
	// without FilesOwned) — the gate does not apply. Return no check row so it
	// is invisible to ChecksRun rather than a spurious always-pass result.
	if len(owned) == 0 {
		return nil
	}

	owned = workflow.ExpandFileScopeWithCompanionTests(owned)
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

	// Required gate 1 (#175/#177): scratch artefacts, a non-owner co-writing a
	// shared doc, or root-level source scratch — the shapes a developer CAN fix
	// by retrying (remove the scratch / stop editing the shared doc). Routed to
	// dev-retry, NOT recovery, and never left advisory (the commit path stages
	// everything, so advisory scratch silently pollutes the branch).
	containmentClean := len(verdict.JunkViolations) == 0 &&
		len(verdict.ModifiedDocUnowned) == 0 &&
		len(verdict.RootScratch) == 0
	containment := payloads.CheckResult{
		Name:     checkName,
		Passed:   containmentClean,
		Required: true,
		Command:  cmd,
		Duration: duration,
	}
	if containmentClean {
		containment.Stdout = fmt.Sprintf("no scratch artefacts committed and no shared docs co-written outside the story's %d owned path(s)", len(owned))
	} else {
		var b strings.Builder
		if len(verdict.JunkViolations) > 0 {
			fmt.Fprintf(&b, "Scratch/merge artefacts must not be committed (write scratch to /tmp, not the worktree): %s. ", strings.Join(verdict.JunkViolations, ", "))
		}
		if len(verdict.RootScratch) > 0 {
			fmt.Fprintf(&b, "Root-level source file(s) that are not deliverables must be removed before submit (a source file belongs in a package directory, not the worktree root — write throwaway probes to /tmp): %s. ", strings.Join(verdict.RootScratch, ", "))
		}
		if len(verdict.ModifiedDocUnowned) > 0 {
			fmt.Fprintf(&b, "Modified shared documentation this story does not own: %s. A doc owned by no story (or by another story) is co-written by parallel stories and cannot be merged at assembly — only edit docs in your file scope; if a deliverable doc isn't yours, surface it as a planning gap. ", strings.Join(verdict.ModifiedDocUnowned, ", "))
		}
		fmt.Fprintf(&b, "Owned paths: %s.", strings.Join(owned, ", "))
		containment.ExitCode = 1
		containment.Stderr = b.String()
	}
	results = append(results, containment)

	// Required gate 2 (ADR-049 move 3): a new source/test file created OUTSIDE
	// the story's declared territory, or a new topology-controlled project file
	// created outside declared ownership, is a planning/ownership gap — the
	// developer cannot fix it by retrying. Emitted as its own check so the
	// execution-manager router fast-fails it to recovery (architecture_revise /
	// story_reprepare) rather than burning the TDD budget. Only present when
	// the gap exists, so a clean run carries no extra Required row.
	if len(verdict.NewUnownedOutOfTerritory) > 0 || len(verdict.NewTopologyControlled) > 0 {
		var b strings.Builder
		if len(verdict.NewUnownedOutOfTerritory) > 0 {
			fmt.Fprintf(&b, "New source/test file(s) created outside this story's declared file scope: %s. "+
				"The story owns paths under: %s. A file the implementation requires but no story owns is created "+
				"identically by every parallel story and collides at assembly — this is a planning/ownership gap "+
				"(the component boundary or the story's files_owned is wrong), not a developer error. ",
				strings.Join(verdict.NewUnownedOutOfTerritory, ", "), strings.Join(owned, ", "))
		}
		if len(verdict.NewTopologyControlled) > 0 {
			fmt.Fprintf(&b, "New topology-controlled project/build file(s) created outside this story's declared file scope: %s. "+
				"Build/workspace/package manifests and standalone wrapper files change repository topology; creating them from a dev loop "+
				"usually means the architecture/story contract introduced a clean-room project root instead of integrating with the brownfield baseline. "+
				"Owned paths: %s.",
				strings.Join(verdict.NewTopologyControlled, ", "), strings.Join(owned, ", "))
		}
		results = append(results, payloads.CheckResult{
			Name:     payloads.CheckFileOwnershipPlanningGap,
			Passed:   false,
			Required: true,
			Command:  cmd,
			ExitCode: 1,
			Stderr:   strings.TrimSpace(b.String()),
			Duration: duration,
		})
	}

	// Advisory: non-doc files modified outside ownership (usually mergeable
	// build/source edits) + new files inside the owned territory (legitimate
	// class splits) / new docs. Surfaced to the reviewer, never hard-fails.
	advisory := append(append([]string(nil), verdict.ModifiedUnowned...), verdict.NewUnowned...)
	if len(advisory) > 0 {
		results = append(results, payloads.CheckResult{
			Name:     payloads.CheckFileOwnershipAdvisory,
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
