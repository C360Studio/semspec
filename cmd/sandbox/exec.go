package main

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// execCommand runs cmd (interpreted via /bin/sh -c) inside dir with the given
// timeout. It captures stdout and stderr independently and returns the exit
// code. Output is capped at maxOutputBytes per stream.
//
// The subprocess is run in its own process group so that a timeout kill reaches
// all child processes, not just the immediate shell. On deadline we eagerly
// SIGKILL the entire process group BEFORE waiting for the leader to exit —
// otherwise, when a child inherits the stdout/stderr pipe FDs and never closes
// them (e.g. a backgrounded `go run` that hangs in time.Sleep), Wait blocks
// indefinitely because the pipe stays open as long as any descendant holds it.
// Without the eager group-kill, the deadline path never executes and child
// processes leak into the sandbox container, accumulating across calls
// (verified empirically 2026-05-28 during the gemini mavlink-decode run: a
// dev agent's `go run generator.go` hangs left 14 zombie processes over
// 33 minutes of accumulation).
func execCommand(ctx context.Context, dir, cmd string, timeout time.Duration, maxOutputBytes int) (stdout, stderr string, exitCode int, timedOut bool) {
	return execCommandWithEnv(ctx, dir, cmd, timeout, maxOutputBytes, nil)
}

func execCommandWithEnv(ctx context.Context, dir, cmd string, timeout time.Duration, maxOutputBytes int, extraEnv []string) (stdout, stderr string, exitCode int, timedOut bool) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Build a bare exec.Command (not CommandContext) so we control cancellation
	// ourselves — CommandContext only SIGKILLs the leader, which is the bug
	// this function exists to fix.
	c := exec.Command("/bin/sh", "-c", cmd)
	c.Dir = dir
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Env = append([]string{
		"PATH=/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/go/bin",
		"HOME=/home/sandbox",
		"GOPATH=/go",
		"GOMODCACHE=/go/pkg/mod",
		"NODE_PATH=/usr/local/lib/node_modules",
	}, extraEnv...)

	var outBuf, errBuf cappedWriter
	outBuf.limit = maxOutputBytes
	errBuf.limit = maxOutputBytes
	c.Stdout = &outBuf
	c.Stderr = &errBuf

	if err := c.Start(); err != nil {
		errBuf.Write([]byte(err.Error()))
		return outBuf.String(), errBuf.String(), 1, false
	}

	waitErr := make(chan error, 1)
	go func() { waitErr <- c.Wait() }()

	var runErr error
	select {
	case runErr = <-waitErr:
		// Process exited on its own (success, failure, or self-imposed signal).
	case <-ctx.Done():
		// Deadline. Kill the entire process group — leader plus every child
		// holding the inherited stdout/stderr pipe FDs. Wait() returns once
		// the kernel reaps them and the pipes close.
		timedOut = true
		if c.Process != nil {
			_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		}
		runErr = <-waitErr
	}

	// Extract exit code.
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if !timedOut {
			exitCode = 1
		}
	}

	return outBuf.String(), errBuf.String(), exitCode, timedOut
}

// cappedWriter accumulates written bytes up to limit, silently discarding the
// rest. It appends a truncation notice when the cap is hit.
type cappedWriter struct {
	buf    bytes.Buffer
	limit  int
	capped bool
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	if w.capped {
		return len(p), nil
	}
	remaining := w.limit - w.buf.Len()
	if remaining <= 0 {
		w.cap()
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = io.Copy(&w.buf, bytes.NewReader(p[:remaining]))
		w.cap()
	} else {
		_, _ = w.buf.Write(p)
	}
	return len(p), nil
}

func (w *cappedWriter) cap() {
	if !w.capped {
		w.capped = true
		_, _ = w.buf.WriteString("\n[output truncated]")
	}
}

func (w *cappedWriter) String() string {
	return strings.TrimRight(w.buf.String(), "\n")
}
