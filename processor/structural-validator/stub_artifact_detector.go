package structuralvalidator

import (
	"archive/zip"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/c360studio/semspec/workflow/payloads"
)

// stubJarSizeThreshold is the cap below which a JAR is treated as a stub
// outright. A real published JAR (even tiny annotation-only libs) is
// typically 2KB+. The take-19 stubs were 55 bytes — MANIFEST.MF and
// nothing else. The threshold is intentionally generous to avoid false
// positives on legitimate tiny JARs while still catching the obvious
// fabrication shape.
const stubJarSizeThreshold = 2048

// CheckStubArtifacts scans modified .jar files for fabrication-shape
// stubs. Returns a CheckResult that fails (Required: true) on any
// detected stub.
//
// Detection rules:
//
//  1. File size < 2 KiB — almost always a MANIFEST-only stub. Real
//     published artifacts are larger even when the artifact is tiny.
//
//  2. Zero `.class` entries inside the JAR — a JVM library JAR without
//     a single class file is structurally meaningless. The take-19
//     pattern: stub JAR contained only META-INF/MANIFEST.MF, the dev
//     wrote tests that depended on classes that didn't exist in the
//     JAR (resolved via a separate fabricated .class file).
//
//  3. Invalid / unreadable ZIP — caught here so the agent can't slip
//     a non-archive past the gate by naming a file foo.jar.
//
// Why Required: true (not advisory): fabrication is a hard ship-stopper.
// Take-19 / take-29 both shipped green builds with 55-byte stubs because
// reviewer/QA had no anchor to reject them. This is the deterministic
// backstop that doesn't depend on persona compliance.
//
// Closes deferred item (c) from take-19 forensics. Item d-equivalent
// (test-body mismatch / faked implementations) is now owned by the
// LLM reviewer + executable QA runtime after issue #113 retired the
// literal-substring harness-discipline checks; see ADR-041 Move 5
// amendment.
func CheckStubArtifacts(workDir string, filesModified []string) payloads.CheckResult {
	jars := filterJarFiles(filesModified)
	if len(jars) == 0 {
		return payloads.CheckResult{
			Name:     "stub-artifact-detector",
			Passed:   true,
			Required: true,
			Command:  "stub-artifact-detector (internal)",
			Stdout:   "no .jar files in modified set — nothing to inspect",
		}
	}

	var violations []string
	for _, rel := range jars {
		abs := rel
		if !filepath.IsAbs(rel) {
			abs = filepath.Join(workDir, rel)
		}
		if v := inspectJar(rel, abs); v != "" {
			violations = append(violations, v)
		}
	}

	if len(violations) == 0 {
		return payloads.CheckResult{
			Name:     "stub-artifact-detector",
			Passed:   true,
			Required: true,
			Command:  "stub-artifact-detector (internal)",
			Stdout:   fmt.Sprintf("%d jar(s) inspected, no stubs detected", len(jars)),
		}
	}

	return payloads.CheckResult{
		Name:     "stub-artifact-detector",
		Passed:   false,
		Required: true,
		Command:  "stub-artifact-detector (internal)",
		Stdout:   strings.Join(violations, "\n"),
	}
}

// filterJarFiles returns only the .jar entries from filesModified.
func filterJarFiles(files []string) []string {
	var out []string
	for _, f := range files {
		if strings.HasSuffix(strings.ToLower(f), ".jar") {
			out = append(out, f)
		}
	}
	return out
}

// inspectJar returns "" when the JAR passes all checks, or a violation
// message describing what's wrong. rel is the path used in messages
// (workspace-relative); abs is the on-disk path used for I/O.
func inspectJar(rel, abs string) string {
	zr, err := zip.OpenReader(abs)
	if err != nil {
		return fmt.Sprintf("stub-artifact %s: not a valid JAR (zip open failed: %v) — fabricated or corrupt", rel, err)
	}
	defer zr.Close()

	// Sum compressed and uncompressed sizes from the zip entries; this
	// is cheaper than os.Stat for the same signal and survives a tar
	// wrapping a zip (no false positives).
	var totalUncompressed uint64
	classCount := 0
	for _, f := range zr.File {
		totalUncompressed += f.UncompressedSize64
		if strings.HasSuffix(f.Name, ".class") {
			classCount++
		}
	}

	if totalUncompressed < stubJarSizeThreshold {
		return fmt.Sprintf("stub-artifact %s: uncompressed size %d bytes < %d threshold — fabrication shape (take-19 stubs were 55 bytes, MANIFEST-only)",
			rel, totalUncompressed, stubJarSizeThreshold)
	}
	if classCount == 0 {
		return fmt.Sprintf("stub-artifact %s: zero .class entries — JAR contains no JVM bytecode (likely META-INF-only stub, the take-19 fabrication pattern)", rel)
	}
	return ""
}
