package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/agentic"
	agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"
	"github.com/google/uuid"
)

// debugHTTPClient is a shared HTTP client for debug queries.
// Using a shared client enables connection reuse.
var debugHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
}

// DebugCommand implements the /debug command for trace correlation and debugging.
type DebugCommand struct{}

// Config returns the command configuration.
func (c *DebugCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/debug\s+(\w+)\s*(.*)$`,
		Permission:  "view",
		RequireLoop: false,
		Help:        "/debug trace|last|snapshot|workflow|loop <id> [--verbose] - Debug and trace correlation tools",
	}
}

// Execute runs the debug command.
func (c *DebugCommand) Execute(
	ctx context.Context,
	cmdCtx *agenticdispatch.CommandContext,
	msg agentic.UserMessage,
	args []string,
	loopID string,
) (agentic.UserResponse, error) {
	if len(args) < 1 {
		return c.showHelp(msg), nil
	}

	subcommand := strings.ToLower(strings.TrimSpace(args[0]))
	remaining := ""
	if len(args) > 1 {
		remaining = strings.TrimSpace(args[1])
	}

	// Parse ID and flags
	parts := strings.Fields(remaining)
	id := ""
	verbose := false

	for _, part := range parts {
		if part == "--verbose" || part == "-v" {
			verbose = true
		} else if id == "" {
			id = part
		}
	}

	switch subcommand {
	case "trace":
		return c.showTrace(ctx, msg, id)
	case "last":
		return c.showLast(ctx, msg)
	case "snapshot":
		return c.exportSnapshot(ctx, msg, id, verbose)
	case "workflow":
		return c.showWorkflow(ctx, msg, id)
	case "loop":
		return c.showLoop(ctx, msg, id)
	case "help":
		return c.showHelp(msg), nil
	default:
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Unknown debug subcommand: %s\n\nRun `/debug help` for available commands.", subcommand),
			Timestamp:   time.Now(),
		}, nil
	}
}

// showHelp displays help for the debug command.
func (c *DebugCommand) showHelp(msg agentic.UserMessage) agentic.UserResponse {
	help := `## Debug Commands

Trace correlation and debugging tools for observability.

### Commands

| Command | Description |
|---------|-------------|
| /debug trace <id> | Show all messages in a trace |
| /debug last | Show the most recent trace |
| /debug snapshot <id> [--verbose] | Export trace to .semspec/debug/ |
| /debug workflow <id> | Show workflow execution state |
| /debug loop <id> | Show agent loop state |

### Examples

` + "```" + `
# Query the most recent trace
/debug last

# Query a specific trace (from command response)
/debug trace abc123def456

# Export snapshot for agent debugging
/debug snapshot abc123def456 --verbose

# Check workflow state
/debug workflow add-user-auth

# Check loop state
/debug loop loop_456
` + "```" + `

### How to Get a Trace ID

Trace IDs are returned in command responses (e.g., /propose, /design).
Use ` + "`/debug last`" + ` to query the most recent trace.
`

	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		Type:        agentic.ResponseTypeResult,
		Content:     help,
		Timestamp:   time.Now(),
	}
}

// TraceResponse represents the response from the message-logger trace endpoint.
type TraceResponse struct {
	TraceID string       `json:"trace_id"`
	Count   int          `json:"count"`
	Entries []TraceEntry `json:"entries"`
}

// TraceEntry represents an entry from the message-logger trace endpoint.
type TraceEntry struct {
	Sequence    int64           `json:"sequence"`
	Timestamp   time.Time       `json:"timestamp"`
	Subject     string          `json:"subject"`
	MessageType string          `json:"message_type,omitempty"`
	TraceID     string          `json:"trace_id,omitempty"`
	SpanID      string          `json:"span_id,omitempty"`
	Summary     string          `json:"summary"`
	RawData     json.RawMessage `json:"raw_data,omitempty"`
}

// showTrace queries the message-logger for trace entries.
func (c *DebugCommand) showTrace(ctx context.Context, msg agentic.UserMessage, traceID string) (agentic.UserResponse, error) {
	if traceID == "" {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     "Trace ID required.\n\nUsage: `/debug trace <trace_id>`",
			Timestamp:   time.Now(),
		}, nil
	}

	gatewayURL := getGatewayURL()
	entries, err := queryTrace(ctx, gatewayURL, traceID)
	if err != nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to query trace: %v\n\nThe message-logger trace endpoint may not be available.", err),
			Timestamp:   time.Now(),
		}, nil
	}

	if len(entries) == 0 {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeResult,
			Content:     fmt.Sprintf("No messages found for trace: %s\n\nThis trace may have expired or the ID may be incorrect.", traceID),
			Timestamp:   time.Now(),
		}, nil
	}

	content := formatTraceEntries(traceID, entries)

	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		Type:        agentic.ResponseTypeResult,
		Content:     content,
		Timestamp:   time.Now(),
	}, nil
}

// exportSnapshot exports a trace snapshot to file (and optionally KV).
func (c *DebugCommand) exportSnapshot(ctx context.Context, msg agentic.UserMessage, traceID string, verbose bool) (agentic.UserResponse, error) {
	if traceID == "" {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     "Trace ID required.\n\nUsage: `/debug snapshot <trace_id> [--verbose]`",
			Timestamp:   time.Now(),
		}, nil
	}

	gatewayURL := getGatewayURL()
	entries, err := queryTrace(ctx, gatewayURL, traceID)
	if err != nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to query trace: %v", err),
			Timestamp:   time.Now(),
		}, nil
	}

	if len(entries) == 0 {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("No messages found for trace: %s", traceID),
			Timestamp:   time.Now(),
		}, nil
	}

	// Build markdown snapshot
	snapshot := buildSnapshot(traceID, entries, verbose)

	// Get repo root for file storage
	repoRoot := os.Getenv("SEMSPEC_REPO_PATH")
	if repoRoot == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			return agentic.UserResponse{
				ResponseID:  uuid.New().String(),
				ChannelType: msg.ChannelType,
				ChannelID:   msg.ChannelID,
				UserID:      msg.UserID,
				Type:        agentic.ResponseTypeError,
				Content:     fmt.Sprintf("Failed to get working directory: %v", err),
				Timestamp:   time.Now(),
			}, nil
		}
	}

	// Create debug directory and write file
	debugDir := filepath.Join(repoRoot, ".semspec", "debug")
	if err := os.MkdirAll(debugDir, 0755); err != nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to create debug directory: %v", err),
			Timestamp:   time.Now(),
		}, nil
	}

	filename := fmt.Sprintf("%s.md", traceID)
	snapshotPath := filepath.Join(debugDir, filename)
	if err := os.WriteFile(snapshotPath, []byte(snapshot), 0644); err != nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to write snapshot: %v", err),
			Timestamp:   time.Now(),
		}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Debug Snapshot Exported\n\n"))
	sb.WriteString(fmt.Sprintf("**Trace ID**: `%s`\n", traceID))
	sb.WriteString(fmt.Sprintf("**Messages**: %d\n", len(entries)))
	sb.WriteString(fmt.Sprintf("**File**: `.semspec/debug/%s`\n\n", filename))

	if verbose {
		sb.WriteString("### Preview\n\n")
		// Show first few entries
		previewCount := 5
		if len(entries) < previewCount {
			previewCount = len(entries)
		}
		for i := 0; i < previewCount; i++ {
			e := entries[i]
			sb.WriteString(fmt.Sprintf("- `%s` %s\n", e.Timestamp.Format("15:04:05.000"), e.Subject))
		}
		if len(entries) > previewCount {
			sb.WriteString(fmt.Sprintf("- ... and %d more\n", len(entries)-previewCount))
		}
	}

	sb.WriteString("\nThis snapshot can be shared with Claude for debugging context.")

	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		Type:        agentic.ResponseTypeResult,
		Content:     sb.String(),
		Timestamp:   time.Now(),
	}, nil
}

// showWorkflow displays workflow execution state.
func (c *DebugCommand) showWorkflow(ctx context.Context, msg agentic.UserMessage, id string) (agentic.UserResponse, error) {
	if id == "" {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     "Workflow ID or slug required.\n\nUsage: `/debug workflow <id>`",
			Timestamp:   time.Now(),
		}, nil
	}

	// Get repo root
	repoRoot := os.Getenv("SEMSPEC_REPO_PATH")
	if repoRoot == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			return agentic.UserResponse{
				ResponseID:  uuid.New().String(),
				ChannelType: msg.ChannelType,
				ChannelID:   msg.ChannelID,
				UserID:      msg.UserID,
				Type:        agentic.ResponseTypeError,
				Content:     fmt.Sprintf("Failed to get working directory: %v", err),
				Timestamp:   time.Now(),
			}, nil
		}
	}

	manager := workflow.NewManager(repoRoot)

	// Try to load as a change slug first
	change, err := manager.LoadChange(id)
	if err != nil {
		// If not found as slug, try to find by workflow execution ID
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Workflow not found: %s\n\nTry using the change slug (e.g., `add-user-auth`).", id),
			Timestamp:   time.Now(),
		}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Workflow Debug: %s\n\n", change.Slug))
	sb.WriteString(fmt.Sprintf("**Title**: %s\n", change.Title))
	sb.WriteString(fmt.Sprintf("**Status**: %s\n", formatStatus(change.Status)))
	sb.WriteString(fmt.Sprintf("**Author**: %s\n", change.Author))
	sb.WriteString(fmt.Sprintf("**Created**: %s\n", change.CreatedAt.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("**Updated**: %s\n\n", change.UpdatedAt.Format(time.RFC3339)))

	sb.WriteString("### Files State\n\n")
	sb.WriteString("| File | Exists |\n")
	sb.WriteString("|------|--------|\n")
	sb.WriteString(fmt.Sprintf("| proposal.md | %v |\n", change.Files.HasProposal))
	sb.WriteString(fmt.Sprintf("| design.md | %v |\n", change.Files.HasDesign))
	sb.WriteString(fmt.Sprintf("| spec.md | %v |\n", change.Files.HasSpec))
	sb.WriteString(fmt.Sprintf("| tasks.md | %v |\n\n", change.Files.HasTasks))

	// Show file paths
	sb.WriteString("### Directory\n\n")
	sb.WriteString(fmt.Sprintf("`.semspec/changes/%s/`\n", change.Slug))

	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		Type:        agentic.ResponseTypeResult,
		Content:     sb.String(),
		Timestamp:   time.Now(),
	}, nil
}

// showLoop displays agent loop state from KV.
func (c *DebugCommand) showLoop(ctx context.Context, msg agentic.UserMessage, loopID string) (agentic.UserResponse, error) {
	if loopID == "" {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     "Loop ID required.\n\nUsage: `/debug loop <loop_id>`",
			Timestamp:   time.Now(),
		}, nil
	}

	gatewayURL := getGatewayURL()
	loopState, err := queryLoopKV(ctx, gatewayURL, loopID)
	if err != nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to query loop: %v\n\nThe loop may not exist or KV access is unavailable.", err),
			Timestamp:   time.Now(),
		}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Loop Debug: %s\n\n", loopID))

	// Pretty print the loop state
	prettyJSON, err := json.MarshalIndent(loopState, "", "  ")
	if err != nil {
		sb.WriteString("```json\n")
		sb.WriteString(fmt.Sprintf("%v", loopState))
		sb.WriteString("\n```\n")
	} else {
		sb.WriteString("```json\n")
		sb.WriteString(string(prettyJSON))
		sb.WriteString("\n```\n")
	}

	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		Type:        agentic.ResponseTypeResult,
		Content:     sb.String(),
		Timestamp:   time.Now(),
	}, nil
}

// showLast queries the most recent trace from message-logger.
func (c *DebugCommand) showLast(ctx context.Context, msg agentic.UserMessage) (agentic.UserResponse, error) {
	gatewayURL := getGatewayURL()
	entries, err := queryRecentEntries(ctx, gatewayURL, 1)
	if err != nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to query recent entries: %v\n\nThe message-logger may not be available.", err),
			Timestamp:   time.Now(),
		}, nil
	}

	if len(entries) == 0 {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeResult,
			Content:     "No recent messages found.\n\nThe message-logger may be empty or unavailable.",
			Timestamp:   time.Now(),
		}, nil
	}

	// Get the trace ID from the most recent entry
	traceID := entries[0].TraceID
	if traceID == "" {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     "Most recent message has no trace ID.\n\nTry running a command that generates traces (e.g., /propose).",
			Timestamp:   time.Now(),
		}, nil
	}

	// Now query the full trace
	return c.showTrace(ctx, msg, traceID)
}

// queryRecentEntries queries the message-logger for recent entries.
func queryRecentEntries(ctx context.Context, gatewayURL string, limit int) ([]TraceEntry, error) {
	url := fmt.Sprintf("%s/message-logger/entries?limit=%d", gatewayURL, limit)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := debugHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("message-logger returned %d: %s", resp.StatusCode, string(body))
	}

	var entries []TraceEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return entries, nil
}

// queryTrace queries the message-logger for trace entries.
func queryTrace(ctx context.Context, gatewayURL, traceID string) ([]TraceEntry, error) {
	url := fmt.Sprintf("%s/message-logger/trace/%s", gatewayURL, traceID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := debugHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // No entries found
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("message-logger returned %d: %s", resp.StatusCode, string(body))
	}

	var traceResp TraceResponse
	if err := json.NewDecoder(resp.Body).Decode(&traceResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return traceResp.Entries, nil
}

// queryLoopKV queries the AGENT_LOOPS KV bucket for a loop state.
func queryLoopKV(ctx context.Context, gatewayURL, loopID string) (map[string]any, error) {
	url := fmt.Sprintf("%s/message-logger/kv/AGENT_LOOPS/%s", gatewayURL, loopID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := debugHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("loop not found: %s", loopID)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("message-logger returned %d: %s", resp.StatusCode, string(body))
	}

	// The KV endpoint returns {key, value, revision, ...}
	var kvEntry struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&kvEntry); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Parse the value as JSON
	var loopState map[string]any
	if err := json.Unmarshal([]byte(kvEntry.Value), &loopState); err != nil {
		// If not JSON, return raw value
		return map[string]any{"raw_value": kvEntry.Value}, nil
	}

	return loopState, nil
}

// formatTraceEntries formats trace entries for display.
func formatTraceEntries(traceID string, entries []TraceEntry) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Trace: %s\n\n", traceID))

	if len(entries) == 0 {
		sb.WriteString("No messages found.\n")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("**Messages**: %d\n\n", len(entries)))

	// Sort by sequence number (chronological order)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Sequence < entries[j].Sequence
	})

	// Calculate time span
	start := entries[0].Timestamp
	end := entries[len(entries)-1].Timestamp
	duration := end.Sub(start)
	sb.WriteString(fmt.Sprintf("**Duration**: %v\n", duration.Round(time.Millisecond)))
	sb.WriteString(fmt.Sprintf("**Start**: %s\n", start.Format(time.RFC3339Nano)))
	sb.WriteString(fmt.Sprintf("**End**: %s\n\n", end.Format(time.RFC3339Nano)))

	sb.WriteString("### Messages\n\n")
	sb.WriteString("| Time | Subject | Summary |\n")
	sb.WriteString("|------|---------|--------|\n")

	baseTime := entries[0].Timestamp
	for _, e := range entries {
		offset := e.Timestamp.Sub(baseTime)
		offsetStr := fmt.Sprintf("+%v", offset.Round(time.Millisecond))
		summary := e.Summary
		if len(summary) > 50 {
			summary = summary[:47] + "..."
		}
		sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", offsetStr, e.Subject, summary))
	}

	return sb.String()
}

// buildSnapshot builds a markdown snapshot for file export.
func buildSnapshot(traceID string, entries []TraceEntry, verbose bool) string {
	var sb bytes.Buffer

	sb.WriteString(fmt.Sprintf("# Debug Snapshot: %s\n\n", traceID))
	sb.WriteString(fmt.Sprintf("Generated: %s\n\n", time.Now().Format(time.RFC3339)))

	sb.WriteString("## Overview\n\n")
	sb.WriteString(fmt.Sprintf("- **Trace ID**: `%s`\n", traceID))
	sb.WriteString(fmt.Sprintf("- **Message Count**: %d\n", len(entries)))

	if len(entries) == 0 {
		sb.WriteString("\nNo messages found.\n")
		return sb.String()
	}

	// Sort by sequence number (chronological order)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Sequence < entries[j].Sequence
	})

	start := entries[0].Timestamp
	end := entries[len(entries)-1].Timestamp
	duration := end.Sub(start)
	sb.WriteString(fmt.Sprintf("- **Duration**: %v\n", duration.Round(time.Millisecond)))
	sb.WriteString(fmt.Sprintf("- **Start**: %s\n", start.Format(time.RFC3339Nano)))
	sb.WriteString(fmt.Sprintf("- **End**: %s\n", end.Format(time.RFC3339Nano)))

	sb.WriteString("\n## Message Flow\n\n")

	baseTime := entries[0].Timestamp
	for i, e := range entries {
		offset := e.Timestamp.Sub(baseTime)
		sb.WriteString(fmt.Sprintf("### %d. %s\n\n", i+1, e.Subject))
		sb.WriteString(fmt.Sprintf("- **Sequence**: %d\n", e.Sequence))
		sb.WriteString(fmt.Sprintf("- **Offset**: +%v\n", offset.Round(time.Millisecond)))
		sb.WriteString(fmt.Sprintf("- **Timestamp**: %s\n", e.Timestamp.Format(time.RFC3339Nano)))
		if e.MessageType != "" {
			sb.WriteString(fmt.Sprintf("- **Type**: %s\n", e.MessageType))
		}
		if e.SpanID != "" {
			sb.WriteString(fmt.Sprintf("- **Span ID**: %s\n", e.SpanID))
		}
		sb.WriteString(fmt.Sprintf("- **Summary**: %s\n", e.Summary))

		if verbose && len(e.RawData) > 0 {
			sb.WriteString("\n**Raw Data**:\n")
			sb.WriteString("```json\n")
			// Pretty print if possible
			var prettyJSON bytes.Buffer
			if err := json.Indent(&prettyJSON, e.RawData, "", "  "); err == nil {
				sb.Write(prettyJSON.Bytes())
			} else {
				sb.Write(e.RawData)
			}
			sb.WriteString("\n```\n")
		}

		sb.WriteString("\n")
	}

	sb.WriteString("---\n\n")
	sb.WriteString("*This snapshot can be provided to Claude for debugging context.*\n")

	return sb.String()
}
