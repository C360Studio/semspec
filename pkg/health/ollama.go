package health

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ollamaShellTimeout caps how long any single ollama subprocess can
// run. The bundle's job is to capture a stuck/wedged daemon WITHOUT
// itself wedging — if `ollama ps` blocks beyond this window, the
// capture treats it as a failure and moves on.
const ollamaShellTimeout = 5 * time.Second

// CaptureOllama runs `ollama --version` and `ollama ps` to populate the
// Ollama-related bundle sections. Returns:
//
//   - host info (version) — nil if Ollama isn't installed or version
//     query fails, so the bundle's HostInfo.Ollama stays absent rather
//     than empty.
//   - state (running models + last error) — always non-nil so the
//     bundle records "we tried; here's why it didn't work" rather than
//     "we forgot Ollama existed."
//
// The function never returns a top-level error — Ollama failures are a
// data point, not a capture failure. Adopters running cloud LLMs will
// see LastError="exec: ollama: not found" and that's correct.
//
// cfg.SkipOllama is the orchestrator's responsibility — when true the
// orchestrator must not call this function at all. Passing cfg here
// keeps the signature symmetric with the HTTP fetchers and reserves
// space for future per-source knobs (custom binary path, etc.).
func CaptureOllama(ctx context.Context, _ CaptureConfig) (*OllamaHostInfo, *OllamaState) {
	state := &OllamaState{}

	hostInfo := captureOllamaVersion(ctx)
	if hostInfo == nil {
		// Version probe failed — almost certainly means the binary is
		// absent, in which case ps will also fail. Skip the second
		// shell-out and stamp the same diagnosis.
		state.LastError = "ollama binary not present"
		return nil, state
	}

	state.Running, state.LastError = captureOllamaRunning(ctx)
	return hostInfo, state
}

// captureOllamaVersion runs `ollama --version` and parses the trailing
// version token. Returns nil if the binary is missing or stderr is
// non-empty.
func captureOllamaVersion(ctx context.Context) *OllamaHostInfo {
	ctx, cancel := context.WithTimeout(ctx, ollamaShellTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ollama", "--version")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	// Output is typically: "ollama version is X.Y.Z\n"
	line := strings.TrimSpace(string(out))
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return &OllamaHostInfo{}
	}
	return &OllamaHostInfo{Version: parts[len(parts)-1]}
}

// captureOllamaRunning runs `ollama ps` and parses the table-formatted
// output. We don't use --format json because older Ollama releases
// don't support it; the table format is stable across versions back
// to 0.1.x.
func captureOllamaRunning(ctx context.Context) ([]OllamaRunningModel, string) {
	ctx, cancel := context.WithTimeout(ctx, ollamaShellTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ollama", "ps")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Non-zero exit usually means the daemon isn't running. Surface
		// the binary's stderr so the bundle reader sees the same message
		// the operator would if they ran the command themselves.
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return nil, msg
	}
	return parseOllamaPs(string(out)), ""
}

// parseOllamaPs walks the columnar `ollama ps` output and returns one
// OllamaRunningModel per data row. Header detection is by exact match
// on "NAME" so future column-name changes degrade visibly rather than
// silently misindexing.
//
// Pure: no I/O, deterministic given the input string. Test against a
// captured `ollama ps` blob.
func parseOllamaPs(text string) []OllamaRunningModel {
	var rows []OllamaRunningModel
	headerSeen := false
	for line := range strings.SplitSeq(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !headerSeen {
			if strings.HasPrefix(trimmed, "NAME") {
				headerSeen = true
			}
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}
		row := OllamaRunningModel{Name: fields[0], ID: fields[1]}
		// SIZE column is "<num> <unit>" (two whitespace-separated
		// tokens), PROCESSOR is "<pct> <kind>" (e.g. "100% GPU"),
		// UNTIL is a free-text time expression. Walk past the size +
		// processor pair and treat everything left as Until.
		idx := 2
		if idx+1 < len(fields) && looksLikeUnit(fields[idx+1]) {
			row.SizeBytes = parseOllamaSize(fields[idx] + fields[idx+1])
			idx += 2
		} else if idx < len(fields) {
			row.SizeBytes = parseOllamaSize(fields[idx])
			idx++
		}
		// Skip the processor column (one or two tokens). We don't carry
		// it on the bundle today — adding the field is additive within
		// v1 if a detector ever needs it.
		if idx < len(fields) && strings.Contains(fields[idx], "%") {
			idx++ // pct token
			if idx < len(fields) && isAllUpper(fields[idx]) {
				idx++ // device token, e.g. "GPU"
			}
		}
		if idx < len(fields) {
			row.Until = strings.Join(fields[idx:], " ")
		}
		rows = append(rows, row)
	}
	return rows
}

// parseOllamaSize converts a size token like "9.0GB" into a byte count.
// Returns 0 for unit-less or unparseable tokens — the bundle treats
// zero as "unknown" rather than fabricating a value.
//
// Callers handling `ollama ps` (which prints "9.0 GB" with a space) are
// expected to concatenate the two columns before calling — see
// parseOllamaPs.
func parseOllamaSize(tok string) int64 {
	if tok == "" {
		return 0
	}
	var split int
	for i, r := range tok {
		if (r >= '0' && r <= '9') || r == '.' {
			split = i + 1
			continue
		}
		break
	}
	if split == 0 {
		return 0
	}
	num, err := strconv.ParseFloat(tok[:split], 64)
	if err != nil {
		return 0
	}
	unit := strings.ToUpper(strings.TrimSpace(tok[split:]))
	if unit == "" {
		return 0
	}
	mult := int64(0)
	switch {
	case strings.HasPrefix(unit, "TB") || strings.HasPrefix(unit, "T"):
		mult = 1 << 40
	case strings.HasPrefix(unit, "GB") || strings.HasPrefix(unit, "G"):
		mult = 1 << 30
	case strings.HasPrefix(unit, "MB") || strings.HasPrefix(unit, "M"):
		mult = 1 << 20
	case strings.HasPrefix(unit, "KB") || strings.HasPrefix(unit, "K"):
		mult = 1 << 10
	case unit == "B":
		mult = 1
	}
	if mult == 0 {
		return 0
	}
	return int64(num * float64(mult))
}

// looksLikeUnit reports whether tok is a size-unit suffix as printed
// by `ollama ps` ("GB", "MB", "KB", "TB", "B"). Used to disambiguate
// the SIZE column ("9.0 GB" — two tokens) from a unit-less SIZE.
func looksLikeUnit(tok string) bool {
	switch strings.ToUpper(tok) {
	case "B", "KB", "MB", "GB", "TB":
		return true
	}
	return false
}

// isAllUpper reports whether tok is non-empty and all of its letters
// are uppercase ASCII. Used to identify the PROCESSOR-device token
// ("GPU", "CPU") so we can skip past it without parsing.
func isAllUpper(tok string) bool {
	if tok == "" {
		return false
	}
	hasLetter := false
	for _, r := range tok {
		if r >= 'A' && r <= 'Z' {
			hasLetter = true
			continue
		}
		if r >= 'a' && r <= 'z' {
			return false
		}
	}
	return hasLetter
}
