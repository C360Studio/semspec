package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360/semspec/config"
	"github.com/c360/semspec/storage"
	"github.com/c360/semspec/tools/file"
	"github.com/c360/semspec/tools/git"
)

// TaskResult represents the result of a task execution.
type TaskResult struct {
	Success bool
	Output  string
	Error   string
}

// App is the main application that wires together all components.
type App struct {
	cfg *config.Config

	// NATS
	embeddedServer *server.Server
	natsConn       *nats.Conn
	js             jetstream.JetStream

	// Storage
	store *storage.Store

	// Tool executors
	fileExecutor *file.Executor
	gitExecutor  *git.Executor
}

// NewApp creates a new application instance.
func NewApp(cfg *config.Config) (*App, error) {
	app := &App{
		cfg:          cfg,
		fileExecutor: file.NewExecutor(cfg.Repo.Path),
		gitExecutor:  git.NewExecutor(cfg.Repo.Path),
	}
	return app, nil
}

// Start initializes and starts all components.
func (a *App) Start(ctx context.Context) error {
	// Start NATS (embedded or connect to external)
	if err := a.startNATS(ctx); err != nil {
		return fmt.Errorf("start NATS: %w", err)
	}

	// Initialize storage
	store, err := storage.NewStore(ctx, a.js)
	if err != nil {
		return fmt.Errorf("initialize storage: %w", err)
	}
	a.store = store

	fmt.Println("✓ Components initialized")
	return nil
}

func (a *App) startNATS(ctx context.Context) error {
	if a.cfg.NATS.URL != "" && !a.cfg.NATS.Embedded {
		// Connect to external NATS
		fmt.Printf("Connecting to NATS at %s...\n", a.cfg.NATS.URL)
		conn, err := nats.Connect(a.cfg.NATS.URL)
		if err != nil {
			return fmt.Errorf("connect to NATS: %w", err)
		}
		a.natsConn = conn
	} else {
		// Start embedded NATS server
		fmt.Println("Starting embedded NATS server...")
		opts := &server.Options{
			Port:      -1, // Random available port
			JetStream: true,
			NoLog:     true,
			NoSigs:    true,
		}

		ns, err := server.NewServer(opts)
		if err != nil {
			return fmt.Errorf("create embedded NATS server: %w", err)
		}

		go ns.Start()

		// Wait for server to be ready
		if !ns.ReadyForConnections(5 * time.Second) {
			ns.Shutdown()
			return fmt.Errorf("embedded NATS server failed to start")
		}

		a.embeddedServer = ns

		// Connect to embedded server
		conn, err := nats.Connect(ns.ClientURL())
		if err != nil {
			ns.Shutdown()
			return fmt.Errorf("connect to embedded NATS: %w", err)
		}
		a.natsConn = conn
	}

	// Get JetStream context
	js, err := jetstream.New(a.natsConn)
	if err != nil {
		return fmt.Errorf("create JetStream context: %w", err)
	}
	a.js = js

	return nil
}

// Shutdown gracefully stops all components.
func (a *App) Shutdown(timeout time.Duration) {
	fmt.Println("\nShutting down...")

	// Close NATS connection
	if a.natsConn != nil {
		a.natsConn.Drain()
		a.natsConn.Close()
	}

	// Shutdown embedded server
	if a.embeddedServer != nil {
		a.embeddedServer.Shutdown()
		a.embeddedServer.WaitForShutdown()
	}

	fmt.Println("Goodbye!")
}

// SubmitTask submits a task for execution and waits for the result.
func (a *App) SubmitTask(ctx context.Context, task string) (*TaskResult, error) {
	// For Phase 1, we have a simple implementation that just acknowledges the task
	// Full agentic loop integration will come in later phases

	// Create a proposal for this task
	proposal := &storage.Proposal{
		Title:       "Task: " + truncate(task, 50),
		Description: task,
	}

	proposalID, err := a.store.CreateProposal(ctx, proposal)
	if err != nil {
		return nil, fmt.Errorf("create proposal: %w", err)
	}

	// Create a task for execution
	t := &storage.Task{
		ProposalID:  proposalID.String(),
		Title:       "Execute task",
		Description: task,
	}

	taskID, err := a.store.CreateTask(ctx, t)
	if err != nil {
		return nil, fmt.Errorf("create task: %w", err)
	}

	// Mark task as in progress
	if err := a.store.UpdateTaskStatus(ctx, taskID, storage.TaskStatusInProgress); err != nil {
		return nil, fmt.Errorf("update task status: %w", err)
	}

	// For now, return a placeholder result
	// Real implementation will integrate with SemStreams agentic-loop
	result := &TaskResult{
		Success: true,
		Output:  fmt.Sprintf("Task registered: %s\nProposal ID: %s\nTask ID: %s\n\nNote: Full agentic execution will be available in Phase 2.", task, proposalID, taskID),
	}

	// Mark task complete
	if err := a.store.UpdateTaskStatus(ctx, taskID, storage.TaskStatusComplete); err != nil {
		return nil, fmt.Errorf("update task status: %w", err)
	}

	// Store result
	_, err = a.store.CreateResult(ctx, &storage.Result{
		TaskID:  taskID.String(),
		Success: true,
		Output:  result.Output,
	})
	if err != nil {
		return nil, fmt.Errorf("create result: %w", err)
	}

	return result, nil
}

// RunREPL runs the interactive REPL loop.
func (a *App) RunREPL(ctx context.Context) error {
	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print("semspec> ")

		if !scanner.Scan() {
			// EOF (Ctrl+D)
			return nil
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		// Check for exit commands
		if input == "quit" || input == "exit" {
			return nil
		}

		// Check for built-in commands
		if strings.HasPrefix(input, "/") {
			a.handleCommand(ctx, input)
			continue
		}

		// Submit as task
		result, err := a.SubmitTask(ctx, input)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			continue
		}

		if result.Success {
			fmt.Println(result.Output)
		} else {
			fmt.Fprintf(os.Stderr, "Task failed: %s\n", result.Error)
		}
		fmt.Println()
	}
}

func (a *App) handleCommand(ctx context.Context, input string) {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return
	}

	cmd := parts[0]
	switch cmd {
	case "/help":
		fmt.Println("Available commands:")
		fmt.Println("  /help     - Show this help")
		fmt.Println("  /status   - Show current status")
		fmt.Println("  /tools    - List available tools")
		fmt.Println("  /config   - Show current configuration")
		fmt.Println("  quit/exit - Exit the REPL")
		fmt.Println()
		fmt.Println("Or type any task description to execute it.")

	case "/status":
		fmt.Printf("Model: %s\n", a.cfg.Model.Default)
		fmt.Printf("Repo: %s\n", a.cfg.Repo.Path)
		if a.embeddedServer != nil {
			fmt.Println("NATS: embedded")
		} else {
			fmt.Printf("NATS: %s\n", a.cfg.NATS.URL)
		}

	case "/tools":
		fmt.Println("Available tools:")
		fmt.Println()
		fmt.Println("File operations:")
		for _, tool := range a.fileExecutor.ListTools() {
			fmt.Printf("  %s - %s\n", tool.Name, tool.Description)
		}
		fmt.Println()
		fmt.Println("Git operations:")
		for _, tool := range a.gitExecutor.ListTools() {
			fmt.Printf("  %s - %s\n", tool.Name, tool.Description)
		}

	case "/config":
		fmt.Printf("Model:\n")
		fmt.Printf("  Endpoint: %s\n", a.cfg.Model.Endpoint)
		fmt.Printf("  Default: %s\n", a.cfg.Model.Default)
		fmt.Printf("\nRepo:\n")
		fmt.Printf("  Path: %s\n", a.cfg.Repo.Path)
		fmt.Printf("\nNATS:\n")
		if a.cfg.NATS.URL != "" && !a.cfg.NATS.Embedded {
			fmt.Printf("  URL: %s\n", a.cfg.NATS.URL)
		} else {
			fmt.Println("  Mode: embedded")
		}

	default:
		fmt.Printf("Unknown command: %s\n", cmd)
		fmt.Println("Type /help for available commands.")
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
