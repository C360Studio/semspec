// Package client provides test clients for e2e scenarios.
package client

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// CLIClient manages a semspec CLI process for E2E testing.
// It spawns the semspec CLI as a subprocess and communicates via stdin/stdout.
type CLIClient struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  io.ReadCloser

	mu        sync.Mutex
	started   bool
	responses chan Response
	errors    chan error
	done      chan struct{}
}

// Response represents a parsed response from the CLI.
type Response struct {
	Type    string // "result", "error", "status", "prompt", "stream"
	Content string
	Raw     string
}

// promptPattern matches CLI prompts like "> " or "> > "
var promptPattern = regexp.MustCompile(`^(> )+$`)

// NewCLIClient creates a new CLI client that will spawn the semspec binary.
func NewCLIClient(binaryPath, configPath, workspacePath string) (*CLIClient, error) {
	args := []string{"cli", "--log-level", "error"}
	if configPath != "" {
		args = append(args, "--config", configPath)
	}
	if workspacePath != "" {
		args = append(args, "--repo", workspacePath)
	}

	cmd := exec.Command(binaryPath, args...)
	cmd.Env = append(os.Environ(), "NO_COLOR=1")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	c := &CLIClient{
		cmd:       cmd,
		stdin:     stdin,
		stdout:    stdout,
		stderr:    stderr,
		responses: make(chan Response, 10),
		errors:    make(chan error, 10),
		done:      make(chan struct{}),
	}

	return c, nil
}

// Start starts the CLI process and begins reading responses.
func (c *CLIClient) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return fmt.Errorf("already started")
	}

	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("start command: %w", err)
	}
	c.started = true

	// Read stdout responses in background
	go c.readResponses(ctx)

	// Drain stderr in background
	go c.drainStderr(ctx)

	return nil
}

// isPromptLine checks if a line is just a prompt
func isPromptLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return true
	}
	// Match lines that are just prompts like "> " or "> > " or "Goodbye!"
	if promptPattern.MatchString(line) {
		return true
	}
	if trimmed == "Goodbye!" {
		return true
	}
	return false
}

// readResponses reads stdout and parses CLI responses.
// The cli-input component outputs responses in this format:
// [RESULT]
// content here
//
// [ERROR] error message
//
// [STATUS] status message
func (c *CLIClient) readResponses(ctx context.Context) {
	defer close(c.done)

	scanner := bufio.NewScanner(c.stdout)
	var currentType string
	var contentBuffer strings.Builder

	flushBuffer := func() {
		content := strings.TrimSpace(contentBuffer.String())
		if currentType != "" && content != "" {
			c.responses <- Response{
				Type:    currentType,
				Content: content,
				Raw:     contentBuffer.String(),
			}
		}
		contentBuffer.Reset()
		currentType = ""
	}

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()

		// Skip prompt lines
		if isPromptLine(line) {
			// When we see a prompt after content, flush the buffer
			// This handles single-line responses like [STATUS] message\n>
			if currentType != "" && contentBuffer.Len() > 0 {
				flushBuffer()
			}
			continue
		}

		// Detect response type markers
		switch {
		case strings.HasPrefix(line, "[RESULT]"):
			flushBuffer()
			currentType = "result"
			// Content may follow on same line or next lines
			rest := strings.TrimPrefix(line, "[RESULT]")
			if rest = strings.TrimSpace(rest); rest != "" {
				contentBuffer.WriteString(rest)
				contentBuffer.WriteString("\n")
			}
		case strings.HasPrefix(line, "[ERROR]"):
			flushBuffer()
			currentType = "error"
			rest := strings.TrimPrefix(line, "[ERROR]")
			contentBuffer.WriteString(strings.TrimSpace(rest))
			// Single-line response - flush immediately
			flushBuffer()
		case strings.HasPrefix(line, "[STATUS]"):
			flushBuffer()
			currentType = "status"
			rest := strings.TrimPrefix(line, "[STATUS]")
			contentBuffer.WriteString(strings.TrimSpace(rest))
			// Single-line response - flush immediately
			flushBuffer()
		case strings.HasPrefix(line, "[PROMPT]"):
			flushBuffer()
			currentType = "prompt"
			rest := strings.TrimPrefix(line, "[PROMPT]")
			contentBuffer.WriteString(strings.TrimSpace(rest))
			// Single-line response - flush immediately
			flushBuffer()
		default:
			// Accumulate content for current response type
			if currentType != "" {
				contentBuffer.WriteString(line)
				contentBuffer.WriteString("\n")
			}
			// Ignore lines outside of response markers
		}
	}

	// Flush any remaining content
	flushBuffer()

	if err := scanner.Err(); err != nil && err != io.EOF {
		c.errors <- fmt.Errorf("scanner error: %w", err)
	}
}

// drainStderr reads and discards stderr to prevent blocking.
func (c *CLIClient) drainStderr(ctx context.Context) {
	scanner := bufio.NewScanner(c.stderr)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
			// Log stderr for debugging if needed
			// For now, just drain it
		}
	}
}

// SendCommand sends a command and waits for a response.
func (c *CLIClient) SendCommand(ctx context.Context, command string) (*Response, error) {
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return nil, fmt.Errorf("client not started")
	}
	c.mu.Unlock()

	// Drain any pending responses first
	c.DrainResponses()

	// Write command
	_, err := fmt.Fprintln(c.stdin, command)
	if err != nil {
		return nil, fmt.Errorf("write command: %w", err)
	}

	// Wait for response with timeout
	timeout := time.After(30 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout:
			return nil, fmt.Errorf("timeout waiting for response")
		case err := <-c.errors:
			return nil, err
		case resp := <-c.responses:
			return &resp, nil
		}
	}
}

// SendCommandMultiResponse sends a command and collects multiple responses.
// This is useful for commands that produce status updates followed by a result.
func (c *CLIClient) SendCommandMultiResponse(ctx context.Context, command string, maxResponses int) ([]Response, error) {
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return nil, fmt.Errorf("client not started")
	}
	c.mu.Unlock()

	// Drain any pending responses first
	c.DrainResponses()

	// Write command
	_, err := fmt.Fprintln(c.stdin, command)
	if err != nil {
		return nil, fmt.Errorf("write command: %w", err)
	}

	// Collect responses
	responses := make([]Response, 0, maxResponses)
	timeout := time.After(30 * time.Second)

	for i := 0; i < maxResponses; i++ {
		select {
		case <-ctx.Done():
			return responses, ctx.Err()
		case <-timeout:
			return responses, nil
		case err := <-c.errors:
			return responses, err
		case resp := <-c.responses:
			responses = append(responses, resp)
			// If we got a result or error, that's typically the final response
			if resp.Type == "result" || resp.Type == "error" {
				return responses, nil
			}
		}
	}

	return responses, nil
}

// WaitForReady waits for the CLI to be ready to accept commands.
// It gives the process time to initialize and connect to NATS.
func (c *CLIClient) WaitForReady(ctx context.Context) error {
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return fmt.Errorf("client not started")
	}
	c.mu.Unlock()

	// Give the process time to initialize
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(3 * time.Second):
		return nil
	}
}

// DrainResponses drains any pending responses from the channel.
func (c *CLIClient) DrainResponses() {
	for {
		select {
		case <-c.responses:
			// Discard
		default:
			return
		}
	}
}

// Close terminates the CLI process.
func (c *CLIClient) Close() error {
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	// Send /quit command
	fmt.Fprintln(c.stdin, "/quit")
	c.stdin.Close()

	// Wait with timeout
	done := make(chan error, 1)
	go func() {
		done <- c.cmd.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		c.cmd.Process.Kill()
		return fmt.Errorf("process killed after timeout")
	}
}

// IsRunning returns whether the CLI process is running.
func (c *CLIClient) IsRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.started && c.cmd.ProcessState == nil
}
