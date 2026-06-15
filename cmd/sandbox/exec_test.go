package main

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestExecCommand_HangingBackgroundedChildKilledWithinDeadline(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("process-group semantics differ on this OS")
	}
	dir := t.TempDir()

	// Background a long sleep so the shell exits immediately but a child
	// holds the inherited stdout/stderr pipe FDs open. This is the exact
	// failure pattern that left zombies in the gemini mavlink-decode run:
	// a `go run` (or any forked long-runner) outlives the parent shell and
	// keeps the stdout pipe open, so naive c.Run()-then-check-deadline
	// blocks forever in Wait().
	start := time.Now()
	stdout, stderr, exitCode, timedOut := execCommand(
		context.Background(),
		dir,
		"(sleep 300 &) ; echo started ; sleep 300",
		500*time.Millisecond,
		64*1024,
	)
	elapsed := time.Since(start)

	if !timedOut {
		t.Errorf("timedOut = false, want true (stdout=%q stderr=%q exit=%d)", stdout, stderr, exitCode)
	}
	// 500ms deadline + ~100ms group-kill propagation budget. If the bug is
	// back, this test blocks until t.Failed() at framework timeout (minutes).
	if elapsed > 3*time.Second {
		t.Errorf("execCommand took %v, want < 3s; Wait() is blocking on child-held pipe FDs (the bug this test guards)", elapsed)
	}
}

func TestExecCommand_NoChildLeakAfterTimeout(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("process-group semantics differ on this OS")
	}
	dir := t.TempDir()

	// Spawn a uniquely-named child via the shell, time it out, then verify
	// no process bearing that name survives. Marker keeps the test self-
	// contained: we only assert about the specific child this test
	// created, not the developer's whole process list.
	marker := "leakguard-" + t.Name() + "-" + time.Now().Format("150405.000000")
	cmd := "(sleep 30 ; : " + marker + ") & echo started ; sleep 30"

	_, _, _, timedOut := execCommand(
		context.Background(),
		dir,
		cmd,
		500*time.Millisecond,
		64*1024,
	)
	if !timedOut {
		t.Fatalf("execCommand did not time out as expected")
	}

	// Group kill is asynchronous w.r.t. Wait return; give the kernel a
	// moment to reap before grepping ps.
	time.Sleep(200 * time.Millisecond)

	out, _ := exec.Command("ps", "-eo", "args").CombinedOutput()
	if strings.Contains(string(out), marker) {
		t.Errorf("child process bearing marker %q survived the timeout — process group kill did not propagate.\nps output:\n%s",
			marker, string(out))
	}
}

func TestExecCommand_NormalCommandStillWorks(t *testing.T) {
	dir := t.TempDir()
	stdout, _, exitCode, timedOut := execCommand(
		context.Background(),
		dir,
		"echo hello",
		2*time.Second,
		64*1024,
	)
	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0", exitCode)
	}
	if !strings.Contains(stdout, "hello") {
		t.Errorf("stdout = %q, want it to contain 'hello'", stdout)
	}
	if timedOut {
		t.Errorf("timedOut = true, want false")
	}
}

func TestExecCommandWithEnv_PassesExtraEnvironment(t *testing.T) {
	dir := t.TempDir()
	stdout, stderr, exitCode, timedOut := execCommandWithEnv(
		context.Background(),
		dir,
		"printf '%s' \"$GRADLE_USER_HOME\"",
		2*time.Second,
		64*1024,
		[]string{"GRADLE_USER_HOME=/tmp/semspec-gradle-cache"},
	)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr=%q)", exitCode, stderr)
	}
	if timedOut {
		t.Fatalf("timedOut = true, want false")
	}
	if stdout != "/tmp/semspec-gradle-cache" {
		t.Fatalf("stdout = %q, want injected env value", stdout)
	}
}

func TestExecCommand_NonZeroExitPreserved(t *testing.T) {
	dir := t.TempDir()
	_, _, exitCode, timedOut := execCommand(
		context.Background(),
		dir,
		"exit 7",
		2*time.Second,
		64*1024,
	)
	if exitCode != 7 {
		t.Errorf("exit code = %d, want 7", exitCode)
	}
	if timedOut {
		t.Errorf("timedOut = true, want false")
	}
}
