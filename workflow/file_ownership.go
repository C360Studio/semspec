package workflow

import (
	"path"
	"sort"
	"strings"
)

// wellKnownExtensionlessDeliverables are root-level deliverable files that carry
// no extension but are real owned artifacts a parallel story can collide on the
// same way README did. Without this set IsConcreteScopedFile would exempt them
// (path.Ext == "") and the ownership gates would miss a Dockerfile/Makefile
// co-write.
var wellKnownExtensionlessDeliverables = map[string]bool{
	"Dockerfile":  true,
	"Makefile":    true,
	"Jenkinsfile": true,
	"Vagrantfile": true,
	"Procfile":    true,
}

// IsConcreteScopedFile reports whether a normalized scoped path is a single
// literal file (not a directory entry or glob). It must either carry a file
// extension or be a well-known extensionless deliverable, and contain no glob
// metacharacters. Directory entries ("src") and patterns ("src/**/*.java")
// return false — a component owns concrete files, not dirs or patterns, so
// requiring those to be "owned" would false-positive.
//
// Single source of truth for "concrete scoped deliverable", shared by the
// plan-reviewer SOP rules (scopedFileOwnershipFindings, story rules) and the
// architecture-generator's early ownership gate (ADR-051). Keep every caller
// citing ONE definition so the well-known-deliverable set cannot drift.
func IsConcreteScopedFile(p string) bool {
	if strings.ContainsAny(p, "*?[") {
		return false
	}
	if path.Ext(p) != "" {
		return true
	}
	return wellKnownExtensionlessDeliverables[path.Base(p)]
}

// ownedFileUniverse builds the set of files owned by any component, expanding
// each component's implementation_files with their companion test paths (a
// component that owns Foo.java owns FooTest.java). Shared ownership is allowed —
// a file listed on several components appears once in the set.
func ownedFileUniverse(components []ComponentDef) map[string]struct{} {
	owned := make(map[string]struct{})
	for _, c := range components {
		for _, f := range ExpandFileScopeWithCompanionTests(c.ImplementationFiles) {
			owned[f] = struct{}{}
		}
	}
	return owned
}

// UnownedScopedIncludeFiles returns the concrete scope.include deliverable files
// (excluding scope.do_not_touch read-only references) that no component owns.
// Returned paths are normalized, de-duplicated, and sorted for deterministic
// output; nil when every concrete include is owned.
//
// ADR-051 / issue #175: scope.include names existing files the plan will MODIFY
// (e.g. build.gradle, README.md). Unlike scope.create — which is reconciled from
// Story.FilesOwned at stories-save (ensureScopeCreateCoversStories) and is owned
// by construction — scope.include is planner-authored and must be explicitly
// attached to an owning component by the architect. An unowned include
// deliverable is written by every parallel story and produces an unmergeable
// conflict at assembly (the 2026-06-13 README wedge).
//
// This is the include-only subset of the plan-reviewer's scopedFileOwnershipFindings,
// callable at architecture-generation time — where scope.include is stable but
// scope.create is still draft-partial (checking create here would false-positive).
// Create-file ownership stays a later check, after the stories phase reconciles
// scope.create.
//
// Ownership uses companion-test expansion; directories/globs are exempt
// (IsConcreteScopedFile); do_not_touch is exempt.
func UnownedScopedIncludeFiles(scope Scope, components []ComponentDef) []string {
	owned := ownedFileUniverse(components)

	doNotTouch := make(map[string]struct{})
	for _, f := range NormalizeFilePaths(scope.DoNotTouch) {
		doNotTouch[f] = struct{}{}
	}

	seen := make(map[string]struct{})
	var orphans []string
	for _, raw := range scope.Include {
		f := NormalizeFilePath(raw)
		if f == "" || !IsConcreteScopedFile(f) {
			continue
		}
		if _, dup := seen[f]; dup {
			continue
		}
		seen[f] = struct{}{}
		if _, protected := doNotTouch[f]; protected {
			continue
		}
		if _, ok := owned[f]; ok {
			continue
		}
		orphans = append(orphans, f)
	}
	sort.Strings(orphans)
	return orphans
}
