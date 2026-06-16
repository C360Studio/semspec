package structuralvalidator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
)

func sortedCopy(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

func ownedSetOf(paths ...string) map[string]struct{} {
	m := make(map[string]struct{})
	for _, p := range workflow.NormalizeFilePaths(paths) {
		m[p] = struct{}{}
	}
	return m
}

func TestParsePorcelain(t *testing.T) {
	in := " M src/A.java\n" +
		"?? patch.diff\n" +
		"A  src/New.java\n" +
		"R  old/Old.java -> src/Renamed.java\n" +
		"\n" + // blank line tolerated
		"M  build.gradle\n"
	got := parsePorcelain(in)

	want := []porcelainEntry{
		{Path: "src/A.java", IndexStatus: ' ', WorkStatus: 'M'},
		{Path: "patch.diff", IndexStatus: '?', WorkStatus: '?'},
		{Path: "src/New.java", IndexStatus: 'A', WorkStatus: ' '},
		{Path: "src/Renamed.java", IndexStatus: 'R', WorkStatus: ' '},
		{Path: "build.gradle", IndexStatus: 'M', WorkStatus: ' '},
	}
	if len(got) != len(want) {
		t.Fatalf("parsePorcelain returned %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestIsJunkArtifact(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"src/A.java.orig", true},
		{"A.rej", true},
		{"foo.patch", true},
		{"patch.diff", true},
		{"patch2.diff", true},
		{"deep/dir/patch3.diff", true},
		{"backup.bak", true},
		{"x.tmp", true},
		{".file.swp", true},
		{"foo~", true},
		{"src/Main.java", false},
		{"README.md", false},
		{"build.gradle", false},
		{"data.diff", false}, // .diff without patch prefix is not junk
		{"src/test/AppTest.java", false},
	}
	for _, tc := range tests {
		if _, ok := isJunkArtifact(tc.path); ok != tc.want {
			t.Errorf("isJunkArtifact(%q) = %v, want %v", tc.path, ok, tc.want)
		}
	}
}

func TestIsIgnorableBuildArtifact(t *testing.T) {
	cases := map[string]bool{
		"javac.20260616_030142.args":        true,
		"javac.args":                        true,
		"build/tmp/compileJava/source.args": true,
		"FindClass.java":                    false,
		"src/main/java/org/x/Foo.java":      false,
		"README.md":                         false,
		"build.gradle":                      false,
	}
	for p, want := range cases {
		if got := isIgnorableBuildArtifact(p); got != want {
			t.Errorf("isIgnorableBuildArtifact(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestFirstSegment(t *testing.T) {
	cases := map[string]string{
		"FindClass.java":     "", // root-level (no dir) → scratch, not a deliverable
		"javac.123.args":     "",
		"src/main/X.java":    "src",
		"other/pkg/New.java": "other",
		"./src/main/X.java":  "src",
	}
	for p, want := range cases {
		if got := firstSegment(p); got != want {
			t.Errorf("firstSegment(%q) = %q, want %q", p, got, want)
		}
	}
}

func TestDecideOwnership(t *testing.T) {
	tests := []struct {
		name                  string
		porcelain             string
		owned                 map[string]struct{}
		wantJunk              []string // matched by path prefix (pattern suffix ignored)
		wantModDocUnowned     []string // hard-fail: non-owner co-writing a shared doc
		wantModUnowned        []string // advisory: non-owner editing non-doc
		wantNewUnowned        []string // advisory: new file in-territory / new doc
		wantNewOutOfTerritory []string // hard-fail: new source/test outside territory
	}{
		{
			name:              "modified non-owned DOC (README wedge) hard-fails",
			porcelain:         " M README.md",
			owned:             ownedSetOf("src/A.java"),
			wantModDocUnowned: []string{"README.md"},
		},
		{
			name:           "modified non-owned BUILD file is advisory (usually mergeable)",
			porcelain:      " M build.gradle",
			owned:          ownedSetOf("src/A.java"),
			wantModUnowned: []string{"build.gradle"},
		},
		{
			name:           "modified non-owned source file is advisory",
			porcelain:      " M src/Other.java",
			owned:          ownedSetOf("src/A.java"),
			wantModUnowned: []string{"src/Other.java"},
		},
		{
			name:      "modified owned file is clean",
			porcelain: " M src/A.java",
			owned:     ownedSetOf("src/A.java"),
		},
		{
			name:      "new owned source file is clean",
			porcelain: "A  src/A.java",
			owned:     ownedSetOf("src/A.java"),
		},
		{
			name:           "new unowned source (class split) in same dir is advisory",
			porcelain:      "?? src/FooHelper.java",
			owned:          ownedSetOf("src/Foo.java"),
			wantNewUnowned: []string{"src/FooHelper.java"},
		},
		{
			name:           "new unowned source in an OWNED subdirectory is advisory (in territory)",
			porcelain:      "?? src/main/java/com/x/internal/Helper.java",
			owned:          ownedSetOf("src/main/java/com/x/Driver.java"),
			wantNewUnowned: []string{"src/main/java/com/x/internal/Helper.java"},
		},
		{
			name:                  "new unowned source OUTSIDE territory is an ownership gap (hard fail)",
			porcelain:             "?? src/main/java/org/sensorhub/impl/sensor/mavsdk/MavsdkDriver.java",
			owned:                 ownedSetOf("src/main/java/org/sensorhub/driver/mavsdk/MavSdkCSDriver.java"),
			wantNewOutOfTerritory: []string{"src/main/java/org/sensorhub/impl/sensor/mavsdk/MavsdkDriver.java"},
		},
		{
			name:                  "new unowned TEST outside territory is an ownership gap (hard fail)",
			porcelain:             "?? src/test/java/org/sensorhub/impl/sensor/mavsdk/MavsdkDriverTest.java",
			owned:                 ownedSetOf("src/main/java/org/sensorhub/driver/mavsdk/MavSdkCSDriver.java"),
			wantNewOutOfTerritory: []string{"src/test/java/org/sensorhub/impl/sensor/mavsdk/MavsdkDriverTest.java"},
		},
		{
			name:      "javac @argfile (build byproduct) is ignored — not a planning gap",
			porcelain: "?? javac.20260616_030142.args",
			owned:     ownedSetOf("src/main/java/org/sensorhub/driver/mavsdk/MavSdkCSDriver.java"),
			// no want* — fully ignored (the run #5 wedge: javac.<ts>.args tripped
			// the ADR-049 ownership gap).
		},
		{
			name:           "root-level dev scratch (FindClass.java) is advisory, NOT a planning gap",
			porcelain:      "?? FindClass.java",
			owned:          ownedSetOf("src/main/java/org/sensorhub/driver/mavsdk/MavSdkCSDriver.java"),
			wantNewUnowned: []string{"FindClass.java"},
		},
		{
			name:      "new Java companion test for owned main class is clean",
			porcelain: "?? src/test/java/org/sensorhub/impl/sensor/mavsdk/UnmannedSystemTest.java",
			owned:     ownedSetOf("src/main/java/org/sensorhub/impl/sensor/mavsdk/UnmannedSystem.java"),
		},
		{
			name:           "new unowned DOC outside territory stays advisory (hygiene only)",
			porcelain:      "?? docs/TRADEOFFS.md",
			owned:          ownedSetOf("src/Foo.java"),
			wantNewUnowned: []string{"docs/TRADEOFFS.md"},
		},
		{
			name:           "empty owned set leaves a new source advisory (no story context)",
			porcelain:      "?? src/New.java",
			owned:          ownedSetOf(), // no declared territory → cannot be 'outside' it
			wantNewUnowned: []string{"src/New.java"},
		},
		{
			name:      "declared shared entry file is owned (move-1 third option seam)",
			porcelain: "?? src/driver/mavsdk/MavsdkDriver.java",
			// Two components that explicitly share an entry file BOTH list it in
			// their implementation_files → it is in this story's FilesOwned → the
			// node gate treats it as owned (clean), and DeriveStoryScheduling
			// serializes the sharing stories. No ownership gap.
			owned: ownedSetOf("src/driver/mavsdk/MavsdkDriver.java", "src/driver/mavsdk/Telemetry.java"),
		},
		{
			name:      "patch.diff committed is junk",
			porcelain: "?? patch.diff",
			owned:     ownedSetOf("src/A.java"),
			wantJunk:  []string{"patch.diff"},
		},
		{
			name:      "patch2.diff is junk",
			porcelain: "?? patch2.diff",
			owned:     ownedSetOf("src/A.java"),
			wantJunk:  []string{"patch2.diff"},
		},
		{
			name:      "orig file is junk",
			porcelain: "?? src/A.java.orig",
			owned:     ownedSetOf("src/A.java"),
			wantJunk:  []string{"src/A.java.orig"},
		},
		{
			name:      "staged-modified owned is clean",
			porcelain: "M  src/A.java",
			owned:     ownedSetOf("src/A.java"),
		},
		{
			name:      "renamed to owned path is clean",
			porcelain: "R  old.java -> src/A.java",
			owned:     ownedSetOf("src/A.java"),
		},
		{
			name:           "renamed to unowned non-doc path is advisory",
			porcelain:      "R  old.java -> src/B.java",
			owned:          ownedSetOf("src/A.java"),
			wantModUnowned: []string{"src/B.java"},
		},
		{
			name:           "deleted unowned non-doc file is advisory",
			porcelain:      " D shared/Config.java",
			owned:          ownedSetOf("src/A.java"),
			wantModUnowned: []string{"shared/Config.java"},
		},
		{
			name:      "normalization matches owned with ./ prefix",
			porcelain: " M ./README.md",
			owned:     ownedSetOf("README.md"),
		},
		{
			name:              "normalization flags modified ./ unowned doc",
			porcelain:         " M ./README.md",
			owned:             ownedSetOf("src/A.java"),
			wantModDocUnowned: []string{"README.md"},
		},
		{
			name:      "junk takes precedence over ownership classification",
			porcelain: "?? README.md.orig",
			owned:     ownedSetOf("README.md"),
			wantJunk:  []string{"README.md.orig"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := decideOwnership(parsePorcelain(tc.porcelain), tc.owned)

			// Junk findings carry a "(pattern)" suffix; compare on the path prefix.
			gotJunkPaths := make([]string, len(v.JunkViolations))
			for i, j := range v.JunkViolations {
				gotJunkPaths[i] = strings.SplitN(j, " (", 2)[0]
			}
			if !equalSets(gotJunkPaths, tc.wantJunk) {
				t.Errorf("junk = %v, want %v", sortedCopy(gotJunkPaths), sortedCopy(tc.wantJunk))
			}
			if !equalSets(v.ModifiedDocUnowned, tc.wantModDocUnowned) {
				t.Errorf("modifiedDocUnowned = %v, want %v", sortedCopy(v.ModifiedDocUnowned), sortedCopy(tc.wantModDocUnowned))
			}
			if !equalSets(v.ModifiedUnowned, tc.wantModUnowned) {
				t.Errorf("modifiedUnowned = %v, want %v", sortedCopy(v.ModifiedUnowned), sortedCopy(tc.wantModUnowned))
			}
			if !equalSets(v.NewUnowned, tc.wantNewUnowned) {
				t.Errorf("newUnowned = %v, want %v", sortedCopy(v.NewUnowned), sortedCopy(tc.wantNewUnowned))
			}
			if !equalSets(v.NewUnownedOutOfTerritory, tc.wantNewOutOfTerritory) {
				t.Errorf("newUnownedOutOfTerritory = %v, want %v", sortedCopy(v.NewUnownedOutOfTerritory), sortedCopy(tc.wantNewOutOfTerritory))
			}
		})
	}
}

// TestDecideOwnership_NewUnownedSourceFile_FailsContainment is a RED test for the
// 2026-06-14 paid mavlink-hard assembly wedge (slug 8beacfaa5856, ~34.5M
// gemini-pro tokens before it failed).
//
// Four requirement stories each owned disjoint, architect-DECLARED paths under
// org/sensorhub/driver/mavsdk/ — paths the architect invented that match no OSH
// convention, against a bare-skeleton baseline with no existing driver. All four
// developers independently CREATED the real OSH-canonical driver entry class
// (org/sensorhub/impl/sensor/mavsdk/MavsdkDriver.java) and its shared test,
// because that is where an OSH sensor driver actually lives. decideOwnership saw
// each as a NEW file outside the owned set and filed it under the *advisory*
// NewUnowned bucket, so clean()==true and the per-node dev-review containment
// gate passed. The collision between the four identical new paths was therefore
// deferred to the terminal assembly merge — failing the plan only after all 16
// TDD nodes ran (the "a conflict at assembly will fail the plan honestly"
// advisory at ownership_check.go is exactly that multi-hour/paid deferral).
//
// A newly-created SOURCE/TEST file outside the story's owned set is NOT a
// mergeable "class split" when sibling parallel stories create the same path; it
// is the same unmergeable shape as the shared-doc co-write the gate already
// hard-fails. The dev review must fail it at the node so the ownership/partition
// gap (here: the architect's fabricated, non-canonical paths) surfaces in
// seconds, not after a multi-hour paid run.
//
// RED until new unowned source/test files are promoted from advisory NewUnowned
// to a hard-fail containment violation (at which point the "new unowned source
// (class split) is advisory" case in TestDecideOwnership must be reconciled).
func TestDecideOwnership_NewUnownedSourceFile_FailsContainment(t *testing.T) {
	// Architect-declared (fictional, non-canonical) owned path.
	owned := ownedSetOf("src/main/java/org/sensorhub/driver/mavsdk/MavSdkCSDriver.java")

	// What the developer actually created: the real OSH-canonical driver + test,
	// outside the declared ownership (untracked "??") — the de-facto path all
	// four parallel stories converged on.
	changes := parsePorcelain(
		"?? src/main/java/org/sensorhub/impl/sensor/mavsdk/MavsdkDriver.java\n" +
			"?? src/test/java/org/sensorhub/impl/sensor/mavsdk/MavsdkDriverTest.java\n",
	)

	v := decideOwnership(changes, owned)

	if v.clean() {
		t.Fatalf("decideOwnership treated newly-created unowned SOURCE/TEST files as advisory "+
			"(clean()==true) — this is the 2026-06-14 mavlink-hard wedge: four parallel stories each "+
			"created the same unowned driver path and the collision was deferred to terminal assembly. "+
			"Want clean()==false so the dev-review gate fails at the node. verdict=%+v", v)
	}
}

func equalSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as, bs := sortedCopy(a), sortedCopy(b)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

// fakeRunner returns canned output for the containment integration test.
type fakeRunner struct {
	stdout   string
	stderr   string
	exitCode int
	err      error
	gotCmd   string
}

func (f *fakeRunner) Run(_ context.Context, command, _ string, _ time.Duration) (string, string, int, error) {
	f.gotCmd = command
	return f.stdout, f.stderr, f.exitCode, f.err
}

func TestRunFileOwnershipContainment(t *testing.T) {
	e := &Executor{}

	t.Run("empty owned skips gate (no check row)", func(t *testing.T) {
		runner := &fakeRunner{stdout: " M README.md"}
		results := e.runFileOwnershipContainment(context.Background(), nil, "/wt", runner)
		if len(results) != 0 {
			t.Fatalf("expected no check rows when owned is empty, got %+v", results)
		}
		if runner.gotCmd != "" {
			t.Errorf("git should not run when owned is empty, ran %q", runner.gotCmd)
		}
	})

	t.Run("modified-unowned DOC hard fails the required gate", func(t *testing.T) {
		runner := &fakeRunner{stdout: " M README.md\n M src/A.java\n"}
		owned := workflow.NormalizeFilePaths([]string{"src/A.java"})
		results := e.runFileOwnershipContainment(context.Background(), owned, "/wt", runner)
		c := findResult(t, results, "file-ownership-containment")
		if c.Passed || !c.Required {
			t.Fatalf("expected required FAIL, got %+v", c)
		}
		if !strings.Contains(c.Stderr, "README.md") {
			t.Errorf("expected README.md named in failure, got %q", c.Stderr)
		}
	})

	t.Run("modified-unowned non-doc (build.gradle) is advisory, gate passes", func(t *testing.T) {
		runner := &fakeRunner{stdout: " M build.gradle\n M src/A.java\n"}
		owned := workflow.NormalizeFilePaths([]string{"src/A.java"})
		results := e.runFileOwnershipContainment(context.Background(), owned, "/wt", runner)
		c := findResult(t, results, "file-ownership-containment")
		if !c.Passed {
			t.Fatalf("build-file edit by a non-owner must NOT hard-fail (usually mergeable), got %+v", c)
		}
		adv := findResult(t, results, "file-ownership-advisory")
		if adv.Required || !strings.Contains(adv.Stdout, "build.gradle") {
			t.Errorf("expected build.gradle in advisory result, got %+v", adv)
		}
	})

	t.Run("junk hard fails the required gate", func(t *testing.T) {
		runner := &fakeRunner{stdout: "?? patch.diff\n M src/A.java\n"}
		owned := workflow.NormalizeFilePaths([]string{"src/A.java"})
		results := e.runFileOwnershipContainment(context.Background(), owned, "/wt", runner)
		c := findResult(t, results, "file-ownership-containment")
		if c.Passed {
			t.Fatalf("expected required FAIL on patch.diff, got %+v", c)
		}
		if !strings.Contains(c.Stderr, "patch.diff") {
			t.Errorf("expected patch.diff named, got %q", c.Stderr)
		}
	})

	t.Run("in-territory new-unowned is advisory, gate passes", func(t *testing.T) {
		runner := &fakeRunner{stdout: "?? src/Helper.java\n M src/A.java\n"}
		owned := workflow.NormalizeFilePaths([]string{"src/A.java"})
		results := e.runFileOwnershipContainment(context.Background(), owned, "/wt", runner)
		c := findResult(t, results, payloads.CheckFileOwnershipContainment)
		if !c.Passed {
			t.Fatalf("required gate should pass when only in-territory new-unowned present, got %+v", c)
		}
		adv := findResult(t, results, payloads.CheckFileOwnershipAdvisory)
		if adv.Required {
			t.Errorf("advisory result must be advisory (Required=false), got %+v", adv)
		}
		if !strings.Contains(adv.Stdout, "src/Helper.java") {
			t.Errorf("expected Helper.java named in advisory, got %q", adv.Stdout)
		}
	})

	t.Run("out-of-territory new source emits a planning-gap Required failure", func(t *testing.T) {
		runner := &fakeRunner{stdout: "?? other/pkg/New.java\n M src/A.java\n"}
		owned := workflow.NormalizeFilePaths([]string{"src/A.java"})
		results := e.runFileOwnershipContainment(context.Background(), owned, "/wt", runner)
		// The base containment row (junk/doc) still passes — the gap is its own row.
		c := findResult(t, results, payloads.CheckFileOwnershipContainment)
		if !c.Passed {
			t.Fatalf("containment (junk/doc) should pass; the gap is a separate row, got %+v", c)
		}
		gap := findResult(t, results, payloads.CheckFileOwnershipPlanningGap)
		if gap.Passed || !gap.Required {
			t.Fatalf("expected a Required planning-gap FAILURE, got %+v", gap)
		}
		if !strings.Contains(gap.Stderr, "other/pkg/New.java") {
			t.Errorf("expected offending path named in planning-gap, got %q", gap.Stderr)
		}
	})

	t.Run("all owned and clean passes with no advisory", func(t *testing.T) {
		runner := &fakeRunner{stdout: " M src/A.java\nA  src/B.java\n"}
		owned := workflow.NormalizeFilePaths([]string{"src/A.java", "src/B.java"})
		results := e.runFileOwnershipContainment(context.Background(), owned, "/wt", runner)
		if len(results) != 1 {
			t.Fatalf("expected only the required result, got %+v", results)
		}
		if !results[0].Passed || !results[0].Required {
			t.Errorf("expected passing required result, got %+v", results[0])
		}
	})

	t.Run("git failure is advisory not a hard fail", func(t *testing.T) {
		runner := &fakeRunner{exitCode: 128, stderr: "not a git repository"}
		owned := workflow.NormalizeFilePaths([]string{"src/A.java"})
		results := e.runFileOwnershipContainment(context.Background(), owned, "/wt", runner)
		if len(results) != 1 || !results[0].Passed || results[0].Required {
			t.Fatalf("git failure must not hard-fail, got %+v", results)
		}
	})
}

func findResult(t *testing.T, results []payloads.CheckResult, name string) payloads.CheckResult {
	t.Helper()
	for _, r := range results {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no CheckResult named %q in %+v", name, results)
	return payloads.CheckResult{}
}

// gitRun runs a git command in dir, failing the test on error.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestRunFileOwnershipContainment_RealGit validates parsePorcelain against the
// ACTUAL `git status --porcelain` output (not synthetic blobs) — the CLAUDE.md
// "real fixture not guessed" discipline. It also pins the H1 interaction: a
// gitignored scratch file is invisible to --untracked-files=all (so the junk arm
// is the fallback for repos lacking a gitignore), while a NON-ignored patch.diff
// is caught.
func TestRunFileOwnershipContainment_RealGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")

	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Base commit: an owned source file + a shared README + a .gitignore.
	write("src/A.java", "class A {}\n")
	write("README.md", "# base\n")
	write(".gitignore", "*.orig\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "base")

	// Dev changes: modify owned source (clean), co-write the shared README
	// (hard-fail doc), drop a non-ignored patch.diff (hard-fail junk), drop a
	// gitignored A.java.orig (invisible — fallback only), add a new helper
	// (advisory).
	write("src/A.java", "class A { int x; }\n")
	write("README.md", "# base\nmy section\n")
	write("patch.diff", "diff --git ...\n")
	write("src/A.java.orig", "leftover\n")
	write("src/Helper.java", "class Helper {}\n")

	owned := workflow.NormalizeFilePaths([]string{"src/A.java"})
	results := (&Executor{}).runFileOwnershipContainment(context.Background(), owned, dir, &localRunner{})

	c := findResult(t, results, "file-ownership-containment")
	if c.Passed {
		t.Fatalf("expected required FAIL (README doc co-write + patch.diff junk), got pass: %+v", c)
	}
	if !strings.Contains(c.Stderr, "README.md") {
		t.Errorf("expected README.md (doc co-write) named, got %q", c.Stderr)
	}
	if !strings.Contains(c.Stderr, "patch.diff") {
		t.Errorf("expected patch.diff (junk) named, got %q", c.Stderr)
	}
	// The gitignored *.orig must NOT appear (it's invisible to --untracked-files=all).
	if strings.Contains(c.Stderr, ".orig") {
		t.Errorf("gitignored .orig should be invisible to the gate, but appeared: %q", c.Stderr)
	}
	adv := findResult(t, results, "file-ownership-advisory")
	if !strings.Contains(adv.Stdout, "src/Helper.java") {
		t.Errorf("expected new Helper.java in advisory, got %q", adv.Stdout)
	}
}

// TestExecute_FileOwnershipContainmentWiring is the Go-level INTEGRATION test
// for Gate 2 (#175/#177): it drives the full Executor.Execute path (not just the
// inner func) against a real git worktree with FilesOwned set, proving the wire
// ValidationRequest.FilesOwned → Execute → runFileOwnershipContainment → git
// status is connected and the overall ValidationResult fails on a doc co-write.
func TestExecute_FileOwnershipContainmentWiring(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")

	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// An empty checklist so Execute proceeds past checklist loading to the
	// always-on gates (a missing checklist short-circuits to a passing result).
	write("checklist.json", `{"checks":[]}`)
	write("src/A.java", "class A {}\n")
	write("README.md", "# base\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "base")

	// Dev co-writes the shared README it does NOT own (the wedge shape) and
	// edits its owned source.
	write("README.md", "# base\nreq-2's section\n")
	write("src/A.java", "class A { int x; }\n")

	e := NewExecutor(dir, "checklist.json", 30*time.Second)
	res, err := e.Execute(context.Background(), &payloads.ValidationRequest{
		Slug:          "wiring",
		FilesModified: []string{"README.md", "src/A.java"},
		FilesOwned:    []string{"src/A.java"},
		// No TaskID → local runner executes git in the worktree (dir).
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Passed {
		t.Fatalf("expected ValidationResult.Passed=false (README co-write by non-owner), got pass: %+v", res.CheckResults)
	}
	var found *payloads.CheckResult
	for i := range res.CheckResults {
		if res.CheckResults[i].Name == "file-ownership-containment" {
			found = &res.CheckResults[i]
		}
	}
	if found == nil {
		t.Fatalf("file-ownership-containment check did not run through Execute; results=%+v", res.CheckResults)
	}
	if found.Passed || !found.Required {
		t.Errorf("containment check should be a Required failure, got %+v", *found)
	}
	if !strings.Contains(found.Stderr, "README.md") {
		t.Errorf("expected README.md named in failure, got %q", found.Stderr)
	}
}
