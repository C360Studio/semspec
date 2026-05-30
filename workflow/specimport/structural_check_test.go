package specimport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeChangeFile creates a file at <root>/<changeName>/<rel> for tests.
func writeChangeFile(t *testing.T, root, changeName, rel, content string) {
	t.Helper()
	full := filepath.Join(root, changeName, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func sampleProposalMarkdown() string {
	return `# Proposal: Sample Change

## Why
Sample reasoning.

## What Changes

### New Capabilities

- ` + "`user-auth`" + ` — Authenticate users via password.
- ` + "`session-store`" + ` — Persist sessions.

### Modified Capabilities

- ` + "`legacy-shim`" + ` — Extend legacy shim.
`
}

func sampleSpecMarkdown(cap string) string {
	return `# Spec: ` + cap + `

## Overview
Sample spec for ` + cap + `.

## Requirements

### Requirement: behaviour
The system SHALL behave correctly.
`
}

func TestStructuralCheck_HappyPath(t *testing.T) {
	root := t.TempDir()
	change := "sample-change"
	writeChangeFile(t, root, change, "proposal.md", sampleProposalMarkdown())
	for _, cap := range []string{"user-auth", "session-store", "legacy-shim"} {
		writeChangeFile(t, root, change, filepath.Join("specs", cap, "spec.md"), sampleSpecMarkdown(cap))
	}
	writeChangeFile(t, root, change, ".openspec.yaml", "schema: spec-driven\n")

	res, err := StructuralCheck(filepath.Join(root, change))
	if err != nil {
		t.Fatalf("StructuralCheck: %v", err)
	}
	if !res.OK {
		t.Errorf("expected OK=true, got findings %+v", res.Findings)
	}
	if got := len(res.Proposal.CapabilityNames); got != 3 {
		t.Errorf("expected 3 capabilities, got %d (%v)", got, res.Proposal.CapabilityNames)
	}
	wantOrder := []string{"user-auth", "session-store", "legacy-shim"}
	for i, want := range wantOrder {
		if res.Proposal.CapabilityNames[i] != want {
			t.Errorf("capability[%d]: want %q, got %q", i, want, res.Proposal.CapabilityNames[i])
		}
	}
	if res.Schema != "spec-driven" {
		t.Errorf("expected schema=spec-driven, got %q", res.Schema)
	}
}

func TestStructuralCheck_MissingProposal(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := StructuralCheck(filepath.Join(root, "empty"))
	if err != nil {
		t.Fatalf("StructuralCheck: %v", err)
	}
	if res.OK {
		t.Error("expected OK=false when proposal.md missing")
	}
	hasMissingProposal := false
	for _, f := range res.Findings {
		if f.Code == "missing_proposal" {
			hasMissingProposal = true
		}
	}
	if !hasMissingProposal {
		t.Errorf("expected missing_proposal finding, got %+v", res.Findings)
	}
}

func TestStructuralCheck_MissingSpec(t *testing.T) {
	root := t.TempDir()
	writeChangeFile(t, root, "sample", "proposal.md", sampleProposalMarkdown())
	// Only emit specs/ for two of three capabilities.
	writeChangeFile(t, root, "sample", "specs/user-auth/spec.md", sampleSpecMarkdown("user-auth"))
	writeChangeFile(t, root, "sample", "specs/session-store/spec.md", sampleSpecMarkdown("session-store"))

	res, _ := StructuralCheck(filepath.Join(root, "sample"))
	if res.OK {
		t.Error("expected OK=false with missing spec")
	}
	hasMissingSpec := false
	for _, f := range res.Findings {
		if f.Code == "missing_spec" && strings.Contains(f.Message, "legacy-shim") {
			hasMissingSpec = true
		}
	}
	if !hasMissingSpec {
		t.Errorf("expected missing_spec finding for legacy-shim, got %+v", res.Findings)
	}
}

func TestStructuralCheck_UnsupportedSchema(t *testing.T) {
	root := t.TempDir()
	writeChangeFile(t, root, "sample", "proposal.md", sampleProposalMarkdown())
	for _, cap := range []string{"user-auth", "session-store", "legacy-shim"} {
		writeChangeFile(t, root, "sample", filepath.Join("specs", cap, "spec.md"), sampleSpecMarkdown(cap))
	}
	writeChangeFile(t, root, "sample", ".openspec.yaml", "schema: bmad-prose\n")

	res, _ := StructuralCheck(filepath.Join(root, "sample"))
	if res.OK {
		t.Error("expected OK=false with unsupported schema")
	}
	hasUnsupported := false
	for _, f := range res.Findings {
		if f.Code == "unsupported_schema" {
			hasUnsupported = true
		}
	}
	if !hasUnsupported {
		t.Errorf("expected unsupported_schema finding, got %+v", res.Findings)
	}
}

func TestStructuralCheck_MissingOpenSpecYAMLIsWarning(t *testing.T) {
	root := t.TempDir()
	writeChangeFile(t, root, "sample", "proposal.md", sampleProposalMarkdown())
	for _, cap := range []string{"user-auth", "session-store", "legacy-shim"} {
		writeChangeFile(t, root, "sample", filepath.Join("specs", cap, "spec.md"), sampleSpecMarkdown(cap))
	}
	// NO .openspec.yaml

	res, _ := StructuralCheck(filepath.Join(root, "sample"))
	if !res.OK {
		t.Errorf("expected OK=true even without .openspec.yaml (warning only), got findings %+v", res.Findings)
	}
	hasWarning := false
	for _, f := range res.Findings {
		if f.Code == "missing_openspec_yaml" && f.Severity == "warning" {
			hasWarning = true
		}
	}
	if !hasWarning {
		t.Error("expected missing_openspec_yaml warning")
	}
	if res.Schema != "spec-driven" {
		t.Errorf("expected default schema=spec-driven, got %q", res.Schema)
	}
}

func TestStructuralCheck_NonDirectoryRejects(t *testing.T) {
	root := t.TempDir()
	notDir := filepath.Join(root, "file.txt")
	if err := os.WriteFile(notDir, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := StructuralCheck(notDir)
	if err == nil {
		t.Error("expected error when path is not a directory")
	}
}

func TestExtractCapabilityNames_HeaderForm(t *testing.T) {
	body := "### `header-cap`\nfoo\n- `bullet-cap` — desc"
	names := extractCapabilityNamesFromProposal(body)
	want := []string{"header-cap", "bullet-cap"}
	if len(names) != 2 || names[0] != want[0] || names[1] != want[1] {
		t.Errorf("want %v, got %v", want, names)
	}
}

func TestExtractCapabilityNames_DedupsRepeats(t *testing.T) {
	body := "- `user-auth` — one\n- `user-auth` — two\n- `session-store` — three"
	names := extractCapabilityNamesFromProposal(body)
	if len(names) != 2 {
		t.Errorf("expected dedup to 2, got %d (%v)", len(names), names)
	}
}

func TestLooksKebabCase(t *testing.T) {
	tests := map[string]bool{
		"user-auth":  true,
		"a":          true,
		"v2-api":     true,
		"":           false,
		"-leading":   false,
		"trailing-":  false,
		"UPPER":      false,
		"with_under": false,
		"with.dot":   false,
	}
	for s, want := range tests {
		t.Run(s, func(t *testing.T) {
			if got := looksKebabCase(s); got != want {
				t.Errorf("looksKebabCase(%q) = %v, want %v", s, got, want)
			}
		})
	}
}
