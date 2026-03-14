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
// all child processes, not just the immediate shell.
func execCommand(ctx context.Context, dir, cmd string, timeout time.Duration, maxOutputBytes int) (stdout, stderr string, exitCode int, timedOut bool) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	c := exec.CommandContext(ctx, "/bin/sh", "-c", cmd)
	c.Dir = dir
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Env = []string{
		"PATH=/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/go/bin",
		"HOME=/home/sandbox",
		"GOPATH=/go",
		"GOMODCACHE=/go/pkg/mod",
		"NODE_PATH=/usr/local/lib/node_modules",
	}

	var outBuf, errBuf cappedWriter
	outBuf.limit = maxOutputBytes
	errBuf.limit = maxOutputBytes
	c.Stdout = &outBuf
	c.Stderr = &errBuf

	runErr := c.Run()

	// Determine whether we hit the deadline.
	if ctx.Err() == context.DeadlineExceeded {
		timedOut = true
		// Kill the entire process group.
		if c.Process != nil {
			_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		}
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
