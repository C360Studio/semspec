package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// OpenSensorHub upstream is mounted read-only at /sources by the WITH_EPIC
// overlay. A generated OSH driver build declares its platform dependency the
// normal way (e.g. `org.sensorhub:sensorhub-core`) and expects to resolve it
// from a package registry — but the configured registry (GitHub Packages)
// returns 401 without credentials, so QA fails on a deliverable whose code is
// otherwise sound (proven 2026-06-15: with osh-core built from source the
// assembled driver builds and its tests pass).
//
// The WITH_EPIC design *intends* osh-core to be available as source, so the QA
// (and dev) environment should make it resolvable from /sources — like a real
// OSH developer who has the platform locally. The blocker is purely mechanical:
// a Gradle composite build (`includeBuild`) must write its own build outputs,
// and /sources is read-only, so a direct includeBuild against it silently falls
// back to the registry → 401.
//
// This stages a WRITABLE copy of each OSH gradle build and drops an init script
// into $GRADLE_USER_HOME/init.d/ that includeBuild()s each with explicit
// dependency substitution (org.sensorhub:<sub> → the source :<sub> project).
// Gradle auto-applies init.d/*.gradle, so any build run under the isolated QA
// GRADLE_USER_HOME (see prepareQAIsolation) resolves OSH from source.
//
// No-op when /sources is absent or holds no OSH gradle build, so non-OSH and
// non-Java projects are unaffected.
//
// NOTE (validation): the staging + script generation is unit-tested here, but
// the end-to-end resolution (Gradle actually substituting from the composite)
// must be validated in a live OSH sandbox before this is relied on. The explicit
// substitution rules mirror the manually-verified diagnostic from 2026-06-15.

const (
	defaultOSHSourcesRoot = "/sources"
	// Stable writable stage dir, reused across QA runs (copying osh-core is
	// expensive). It lives OUTSIDE the per-run tmpRoot so it is not deleted by
	// the per-run cleanup; each run only references it via its own init.d script.
	defaultOSHStageRoot = "/tmp/semspec-osh-src"
	oshInitScriptName   = "10-osh-source-substitution.gradle"
	oshStageStampFile   = ".semspec-osh-src-of"
)

// oshSourceConfig is overridable for tests.
type oshSourceConfig struct {
	sourcesRoot string
	stageRoot   string
	// A directory under sourcesRoot is treated as an OSH gradle build to
	// includeBuild when its name contains any token AND it has a settings.gradle.
	matchTokens []string
}

func defaultOSHSourceConfig() oshSourceConfig {
	cfg := oshSourceConfig{
		sourcesRoot: oshEnvOr("SEMSPEC_OSH_SOURCES_ROOT", defaultOSHSourcesRoot),
		stageRoot:   oshEnvOr("SEMSPEC_OSH_STAGE_ROOT", defaultOSHStageRoot),
		matchTokens: []string{"osh-core", "osh-addons"},
	}
	if v := os.Getenv("SEMSPEC_OSH_SOURCE_DIRS"); v != "" {
		cfg.matchTokens = oshSplitNonEmpty(v, ",")
	}
	return cfg
}

// defaultGradleHome resolves the Gradle home that plain (dev/validator) sandbox
// exec uses: GRADLE_USER_HOME if exported, else $HOME/.gradle. This mirrors
// execCommandWithEnv, which sets HOME=/home/sandbox and does not set
// GRADLE_USER_HOME.
func defaultGradleHome() string {
	if v := os.Getenv("GRADLE_USER_HOME"); v != "" {
		return v
	}
	home := os.Getenv("HOME")
	if home == "" {
		home = "/home/sandbox"
	}
	return filepath.Join(home, ".gradle")
}

// installOSHSourceSubstitutionForDev installs the OSH source-substitution init
// script into the default Gradle home so dev/validator builds (plain exec, no
// isolated GRADLE_USER_HOME) resolve OSH from source too — not just QA.
//
// Fails closed: when OSH /sources are present but staging fails, it returns an
// error. The caller treats that as fatal, turning a broken environment contract
// into an immediate harness red instead of a noisy registry 401 in every dev
// loop. Absent /sources is a clean no-op (returns nil).
func installOSHSourceSubstitutionForDev(logger *slog.Logger) error {
	home := defaultGradleHome()
	summary, err := stageOSHSourceSubstitution(home, defaultOSHSourceConfig())
	if err != nil {
		return fmt.Errorf("install OSH source substitution into %s: %w", home, err)
	}
	if summary != "" {
		logger.Info("OSH source substitution installed for dev/validator builds",
			"gradle_home", home, "detail", summary)
	}
	return nil
}

// stageOSHSourceSubstitution stages writable OSH source builds and writes the
// init.d substitution script under gradleHome. It returns a human-readable
// summary (empty when nothing was staged). The caller treats a returned error
// as non-fatal — QA then falls back to the deliverable's declared resolution.
func stageOSHSourceSubstitution(gradleHome string, cfg oshSourceConfig) (summary string, err error) {
	srcDirs, err := discoverOSHSourceDirs(cfg)
	if err != nil || len(srcDirs) == 0 {
		return "", err
	}

	type staged struct {
		dir         string
		subprojects []string
	}
	var builds []staged
	for _, src := range srcDirs {
		dst := filepath.Join(cfg.stageRoot, filepath.Base(src))
		if err := stageWritableCopy(src, dst); err != nil {
			return "", fmt.Errorf("stage OSH source %s: %w", src, err)
		}
		builds = append(builds, staged{
			dir:         dst,
			subprojects: parseGradleIncludes(filepath.Join(dst, "settings.gradle")),
		})
	}

	var b strings.Builder
	b.WriteString("// Auto-generated by semspec sandbox QA. Resolve OpenSensorHub upstream\n")
	b.WriteString("// from source (WITH_EPIC /sources) instead of an authenticated registry.\n")
	b.WriteString("gradle.settingsEvaluated { settings ->\n")
	var staticParts []string
	for _, bd := range builds {
		staticParts = append(staticParts, filepath.Base(bd.dir))
		if len(bd.subprojects) == 0 {
			// Fall back to plain includeBuild (Gradle auto-substitution by the
			// included build's published coordinates).
			fmt.Fprintf(&b, "    settings.includeBuild(%q)\n", bd.dir)
			continue
		}
		fmt.Fprintf(&b, "    settings.includeBuild(%q) { included ->\n", bd.dir)
		b.WriteString("        included.dependencySubstitution {\n")
		for _, sp := range bd.subprojects {
			// Explicit, version-agnostic substitution of the published module
			// onto the source project (mirrors the verified diagnostic).
			fmt.Fprintf(&b,
				"            substitute module('org.sensorhub:%s') using project(':%s')\n",
				sp, sp)
		}
		b.WriteString("        }\n")
		b.WriteString("    }\n")
	}
	b.WriteString("}\n")

	if err := writeInitScript(gradleHome, b.String()); err != nil {
		return "", err
	}
	return "OSH source substitution enabled (includeBuild from source): " +
		strings.Join(staticParts, ", "), nil
}

// discoverOSHSourceDirs returns absolute paths of OSH gradle builds under
// sourcesRoot (dir name contains a match token AND contains settings.gradle).
func discoverOSHSourceDirs(cfg oshSourceConfig) ([]string, error) {
	entries, err := os.ReadDir(cfg.sourcesRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // /sources not mounted → no-op
		}
		return nil, err
	}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() || !oshMatchesAnyToken(e.Name(), cfg.matchTokens) {
			continue
		}
		abs := filepath.Join(cfg.sourcesRoot, e.Name())
		if _, statErr := os.Stat(filepath.Join(abs, "settings.gradle")); statErr != nil {
			continue // not a gradle build root
		}
		dirs = append(dirs, abs)
	}
	sort.Strings(dirs)
	return dirs, nil
}

// stageWritableCopy copies src→dst once (skips when already staged from the same
// source, refreshes when the source path changed), since /sources is read-only
// and copying osh-core is expensive.
func stageWritableCopy(src, dst string) error {
	stamp := filepath.Join(dst, oshStageStampFile)
	if existing, err := os.ReadFile(stamp); err == nil && strings.TrimSpace(string(existing)) == src {
		return nil // already staged from this source
	}
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	// `cp -a` preserves the gradle wrapper's executable bit and is robust for
	// large trees. The trailing /. copies contents into dst.
	if out, err := exec.Command("cp", "-a", src+"/.", dst).CombinedOutput(); err != nil {
		return fmt.Errorf("cp -a %s -> %s: %v: %s", src, dst, err, strings.TrimSpace(string(out)))
	}
	return os.WriteFile(stamp, []byte(src), 0o644)
}

var gradleIncludeRe = regexp.MustCompile(`(?m)^\s*include\s+['"]([^'"]+)['"]`)

// parseGradleIncludes returns the project names declared by `include '<name>'`
// lines in a settings.gradle. Returns nil when the file is unreadable or has no
// includes (the caller then falls back to plain includeBuild).
func parseGradleIncludes(settingsPath string) []string {
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var names []string
	for _, m := range gradleIncludeRe.FindAllStringSubmatch(string(data), -1) {
		name := strings.TrimSpace(m[1])
		name = strings.TrimPrefix(name, ":")
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

func writeInitScript(gradleHome, content string) error {
	initDir := filepath.Join(gradleHome, "init.d")
	if err := os.MkdirAll(initDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(initDir, oshInitScriptName), []byte(content), 0o644)
}

func oshMatchesAnyToken(name string, tokens []string) bool {
	for _, t := range tokens {
		if t != "" && strings.Contains(name, t) {
			return true
		}
	}
	return false
}

func oshSplitNonEmpty(s, sep string) []string {
	var out []string
	for _, p := range strings.Split(s, sep) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func oshEnvOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
