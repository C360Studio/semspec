package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeOSHSource writes a minimal osh-core-shaped gradle build under root/name.
func fakeOSHSource(t *testing.T, root, name string, includes []string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("rootProject.name = 'osh-core'\n\n")
	for _, inc := range includes {
		b.WriteString("include '" + inc + "'\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.gradle"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	// a file to copy so we can assert the stage actually copied content
	if err := os.WriteFile(filepath.Join(dir, "build.gradle"), []byte("// osh-core\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDiscoverOSHSourceDirs(t *testing.T) {
	srcRoot := t.TempDir()
	fakeOSHSource(t, srcRoot, "github-com-opensensorhub-osh-core", []string{"sensorhub-core"})
	fakeOSHSource(t, srcRoot, "github-com-opensensorhub-osh-addons", []string{"sensorhub-driver-foo"})
	// a non-OSH dir (matches no token) and an OSH-named dir without settings.gradle
	if err := os.MkdirAll(filepath.Join(srcRoot, "github-com-other-thing"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(srcRoot, "osh-core-but-no-settings"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := oshSourceConfig{sourcesRoot: srcRoot, matchTokens: []string{"osh-core", "osh-addons"}}
	dirs, err := discoverOSHSourceDirs(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 2 {
		t.Fatalf("expected 2 OSH dirs, got %d: %v", len(dirs), dirs)
	}
	for _, d := range dirs {
		if !strings.Contains(d, "osh-core") && !strings.Contains(d, "osh-addons") {
			t.Errorf("unexpected dir discovered: %s", d)
		}
	}
}

func TestDiscoverOSHSourceDirs_NoSourcesIsNoOp(t *testing.T) {
	cfg := oshSourceConfig{sourcesRoot: filepath.Join(t.TempDir(), "does-not-exist"), matchTokens: []string{"osh-core"}}
	dirs, err := discoverOSHSourceDirs(cfg)
	if err != nil {
		t.Fatalf("absent /sources must be a no-op, got error: %v", err)
	}
	if len(dirs) != 0 {
		t.Fatalf("expected no dirs, got %v", dirs)
	}
}

func TestParseGradleIncludes(t *testing.T) {
	dir := t.TempDir()
	content := `rootProject.name = 'osh-core'
include 'swe-common-core'
include 'sensorhub-core'
  include "quoted-double"
include ':leading-colon'
include 'swe-common-core'
project(':swe-common-core').projectDir = "$rootDir/lib-ogc/swe-common-core" as File
`
	p := filepath.Join(dir, "settings.gradle")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := parseGradleIncludes(p)
	want := []string{"swe-common-core", "sensorhub-core", "quoted-double", "leading-colon"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("parseGradleIncludes = %v, want %v", got, want)
	}
	if parseGradleIncludes(filepath.Join(dir, "nope.gradle")) != nil {
		t.Fatalf("missing file should return nil")
	}
}

func TestStageWritableCopy_CopiesAndSkips(t *testing.T) {
	src := fakeOSHSource(t, t.TempDir(), "github-com-opensensorhub-osh-core", []string{"sensorhub-core"})
	dst := filepath.Join(t.TempDir(), "osh-core")

	if err := stageWritableCopy(src, dst); err != nil {
		t.Fatalf("first copy: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "settings.gradle")); err != nil {
		t.Fatalf("expected settings.gradle copied: %v", err)
	}
	// mark dst so we can detect whether a second call re-copies (it shouldn't)
	marker := filepath.Join(dst, "MARKER")
	if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := stageWritableCopy(src, dst); err != nil {
		t.Fatalf("second copy: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("second call must skip (marker should survive): %v", err)
	}
	// changing the source path should refresh (wipe) the stage
	src2 := fakeOSHSource(t, t.TempDir(), "github-com-opensensorhub-osh-core", []string{"sensorhub-core"})
	if err := stageWritableCopy(src2, dst); err != nil {
		t.Fatalf("refresh copy: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("source change must refresh the stage (marker should be gone)")
	}
}

func TestStageOSHSourceSubstitution_EndToEnd(t *testing.T) {
	srcRoot := t.TempDir()
	fakeOSHSource(t, srcRoot, "github-com-opensensorhub-osh-core", []string{"sensorhub-core", "swe-common-core"})
	cfg := oshSourceConfig{
		sourcesRoot: srcRoot,
		stageRoot:   filepath.Join(t.TempDir(), "stage"),
		matchTokens: []string{"osh-core"},
	}
	gradleHome := filepath.Join(t.TempDir(), "gradle")

	summary, err := stageOSHSourceSubstitution(gradleHome, cfg)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if !strings.Contains(summary, "OSH source substitution enabled") {
		t.Fatalf("unexpected summary: %q", summary)
	}

	script := filepath.Join(gradleHome, "init.d", oshInitScriptName)
	data, err := os.ReadFile(script)
	if err != nil {
		t.Fatalf("init script not written: %v", err)
	}
	s := string(data)
	for _, want := range []string{
		"gradle.settingsEvaluated",
		"settings.includeBuild(",
		"dependencySubstitution",
		"substitute module('org.sensorhub:sensorhub-core') using project(':sensorhub-core')",
		"substitute module('org.sensorhub:swe-common-core') using project(':swe-common-core')",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("init script missing %q\n---\n%s", want, s)
		}
	}
}

func TestStageOSHSourceSubstitution_NoOpWithoutOSH(t *testing.T) {
	cfg := oshSourceConfig{
		sourcesRoot: filepath.Join(t.TempDir(), "absent"),
		stageRoot:   t.TempDir(),
		matchTokens: []string{"osh-core"},
	}
	gradleHome := filepath.Join(t.TempDir(), "gradle")
	summary, err := stageOSHSourceSubstitution(gradleHome, cfg)
	if err != nil {
		t.Fatalf("no-op must not error: %v", err)
	}
	if summary != "" {
		t.Fatalf("expected empty summary, got %q", summary)
	}
	if _, err := os.Stat(filepath.Join(gradleHome, "init.d", oshInitScriptName)); !os.IsNotExist(err) {
		t.Fatalf("no init script should be written when no OSH source present")
	}
}

func TestStageOSHSourceSubstitution_FailsClosedWhenStagingFails(t *testing.T) {
	srcRoot := t.TempDir()
	fakeOSHSource(t, srcRoot, "github-com-opensensorhub-osh-core", []string{"sensorhub-core"})
	// stageRoot is a regular FILE, so MkdirAll beneath it fails → staging fails
	// even though OSH source WAS discovered. This must surface a hard error
	// (fail closed), not a silent no-op.
	badStage := filepath.Join(t.TempDir(), "stage-is-a-file")
	if err := os.WriteFile(badStage, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := oshSourceConfig{sourcesRoot: srcRoot, stageRoot: badStage, matchTokens: []string{"osh-core"}}
	gradleHome := filepath.Join(t.TempDir(), "gradle")

	if _, err := stageOSHSourceSubstitution(gradleHome, cfg); err == nil {
		t.Fatal("expected fail-closed error when OSH discovered but staging fails")
	}
	if _, err := os.Stat(filepath.Join(gradleHome, "init.d", oshInitScriptName)); !os.IsNotExist(err) {
		t.Fatal("init script must not be written when staging fails")
	}
}

func TestDefaultGradleHome(t *testing.T) {
	t.Setenv("GRADLE_USER_HOME", "/custom/gradle-home")
	if got := defaultGradleHome(); got != "/custom/gradle-home" {
		t.Fatalf("with GRADLE_USER_HOME set, got %q", got)
	}
	t.Setenv("GRADLE_USER_HOME", "")
	t.Setenv("HOME", "/home/somebody")
	if got := defaultGradleHome(); got != "/home/somebody/.gradle" {
		t.Fatalf("falling back to HOME, got %q", got)
	}
}
