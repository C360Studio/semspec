package workflow

import (
	"path"
	"strings"
)

// CompanionTestPaths returns conventional test-file companions for an owned
// source file. These paths are deterministic ownership expansions: a Story that
// owns src/main/java/.../Foo.java also owns the canonical unit test path
// src/test/java/.../FooTest.java, even when the architect omitted that test
// file from component_boundaries[].implementation_files.
//
// The expansion is intentionally conservative for MVP: Maven-style
// src/{main,test}/java only, and only FooTest.java. Multi-module layouts and
// alternate suffixes such as *IT.java or *Tests.java require explicit scope.
func CompanionTestPaths(sourcePath string) []string {
	p := NormalizeFilePath(sourcePath)
	if p == "" {
		return nil
	}

	if strings.HasPrefix(p, "src/main/java/") && strings.HasSuffix(p, ".java") {
		rel := strings.TrimPrefix(p, "src/main/java/")
		base := strings.TrimSuffix(path.Base(rel), ".java")
		if base == "" || strings.HasSuffix(base, "Test") || strings.HasSuffix(base, "Tests") {
			return nil
		}
		dir := path.Dir(rel)
		testRel := base + "Test.java"
		if dir != "." {
			testRel = dir + "/" + testRel
		}
		return []string{"src/test/java/" + testRel}
	}

	return nil
}

// ExpandFileScopeWithCompanionTests returns a normalized, deduplicated file
// scope plus deterministic companion test files. Original paths keep their
// relative order; companions are appended immediately after the source that
// implies them so prompt output stays readable.
func ExpandFileScopeWithCompanionTests(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths)*2)
	add := func(p string) {
		p = NormalizeFilePath(p)
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}

	for _, p := range paths {
		np := NormalizeFilePath(p)
		add(np)
		for _, companion := range CompanionTestPaths(np) {
			add(companion)
		}
	}
	return out
}
