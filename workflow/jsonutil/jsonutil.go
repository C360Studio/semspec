// Package jsonutil extracts and cleans JSON from LLM-generated text.
//
// LLMs frequently wrap JSON in markdown code fences, prefix it with prose
// ("Here is the JSON:"), trail it with explanations, or include line
// comments inside the body. ExtractJSON and ExtractJSONArray strip those
// wrappers and return a parseable string. trimToBalancedJSON handles the
// Go 1.25 json.Decoder breaking change where trailing content makes
// Unmarshal fail.
//
// ADR-035 (strict-parse discipline — no silent compensation): tolerance
// survives only as a fixed list of named, reviewed shape transforms,
// each idempotent and each emitting per-fire telemetry. Today the named
// quirks are fenced_json_wrapper, js_line_comments, trailing_commas,
// and greedy_object_fallback (A.2 observation-only — fires when prose
// is wrapped around JSON). ParseStrict reports which quirks fired so
// callers can attribute fires to a (role, model, prompt_version) tuple
// via vocabulary/observability predicates.
//
// Telemetry: per-fire `prometheus.CounterVec` (`semspec_jsonutil_quirks_fired_total`,
// labeled by `quirk`) plus a Debug log per fire. The CounterVec is
// registered via RegisterMetrics — call once during process startup
// from cmd/semspec/main.go. Until registered, the counter still
// accumulates in memory but isn't exposed at /metrics. Stats() reads
// the counter values for diagnostic endpoints and tests.
//
// Phase 2 (per-caller, not in this package) wires triple emission
// through ParseStrict's QuirksFired return value when a caller has
// per-call context (role, model, prompt_version).
//
// ExtractJSON remains a back-compat wrapper around ParseStrict and
// discards the QuirksFired info. New callers should prefer ParseStrict.
package jsonutil

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/c360studio/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// Pre-compiled regex patterns for JSON extraction from LLM responses.
var (
	// jsonBlockPattern matches JSON inside markdown code blocks: ```json { ... } ```
	jsonBlockPattern = regexp.MustCompile("(?s)```(?:json)?\\s*\\n?(\\{.*\\})\\s*```")
	// jsonObjectPattern matches any JSON object (greedy fallback).
	jsonObjectPattern = regexp.MustCompile(`(?s)\{[\s\S]*\}`)
	// jsonArrayBlockPattern matches JSON arrays inside markdown code blocks.
	jsonArrayBlockPattern = regexp.MustCompile("(?s)```(?:json)?\\s*\\n?(\\[.*\\])\\s*```")
	// jsonArrayPattern matches any JSON array (greedy fallback).
	jsonArrayPattern = regexp.MustCompile(`(?s)\[[\s\S]*\]`)
	// trailingCommaPattern matches trailing commas before ] or }.
	trailingCommaPattern = regexp.MustCompile(`,\s*([}\]])`)
)

// QuirkID identifies a named, reviewed shape transform applied by the
// parser. Adding a new QuirkID requires (1) a new constant here, (2)
// inclusion in allQuirks (so the CounterVec child is pre-warmed at
// init), (3) a fire site that records the quirk via fireQuirk, and (4)
// at least one test fixture proving the quirk fires on a real
// LLM-output shape. ADR-035 audit sites A.1, A.2, A.3.
type QuirkID string

const (
	// QuirkFencedJSONWrapper fires when the parser stripped a markdown
	// code-fence wrapper (```json ... ``` or ``` ... ```) from the input.
	QuirkFencedJSONWrapper QuirkID = "fenced_json_wrapper"

	// QuirkJSLineComments fires when the parser stripped one or more
	// JavaScript-style line comments (`// ...`) from the input. LLMs
	// trained on JS-style snippets sometimes emit these inside JSON.
	QuirkJSLineComments QuirkID = "js_line_comments"

	// QuirkTrailingCommas fires when the parser removed one or more
	// trailing commas before `}` or `]`. Strict JSON rejects them; lots
	// of code-trained models emit them anyway.
	QuirkTrailingCommas QuirkID = "trailing_commas"

	// QuirkGreedyObjectFallback fires when the parser extracted a JSON
	// object from prose-wrapped input — i.e. the input contained
	// non-JSON characters around a balanced `{...}` block, with no
	// markdown fence. ADR-035 audit site A.2: observation-only for now;
	// future strict flip would reject this shape and inject a RETRY
	// HINT asking the model to fence its output. Fires only when the
	// extracted substring is strictly shorter than the trimmed input —
	// pure-JSON inputs (no fence, but match equals trimmed input) do
	// NOT fire this quirk.
	QuirkGreedyObjectFallback QuirkID = "greedy_object_fallback"
)

// allQuirks enumerates every named quirk so init() can pre-warm the
// CounterVec children — without pre-warming, testutil.ToFloat64
// panics on Stats() reads of unfired quirks. Operators want to see
// "this quirk hasn't fired yet" as a distinct signal from "this quirk
// doesn't exist."
var allQuirks = []QuirkID{
	QuirkFencedJSONWrapper,
	QuirkJSLineComments,
	QuirkTrailingCommas,
	QuirkGreedyObjectFallback,
}

// quirksFiredCounter is the per-fire CounterVec exposed at /metrics
// when RegisterMetrics is called during startup. Single vec labeled by
// quirk handles all current and future named-quirk fires from this
// package. Pre-warmed at init so children exist before any fire or
// Stats() read.
//
// The metric name uses the canonical Prometheus convention:
// `<namespace>_<subsystem>_<name>_total` for counters. `semspec_*`
// mirrors semstreams' `semstreams_*` prefix.
var quirksFiredCounter = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "semspec_jsonutil_quirks_fired_total",
		Help: "Total fires of named jsonutil shape-transform quirks (ADR-035). Labeled by quirk name (fenced_json_wrapper, js_line_comments, trailing_commas, greedy_object_fallback).",
	},
	[]string{"quirk"},
)

// firedCounters caches per-quirk Counter handles so fireQuirk avoids
// the WithLabelValues map lookup on every fire (LLM responses parse on
// the hot path). Populated at init once per known quirk.
var firedCounters = make(map[QuirkID]prometheus.Counter, len(allQuirks))

func init() {
	// Pre-warm CounterVec children for every known quirk so Stats()
	// reads return 0 instead of panicking on unobserved children, and
	// fireQuirk can dereference a cached handle without a per-call
	// WithLabelValues lookup.
	for _, q := range allQuirks {
		firedCounters[q] = quirksFiredCounter.WithLabelValues(string(q))
	}
}

// RegisterMetrics registers jsonutil's quirk CounterVec with the given
// metrics registry so per-quirk fires surface at /metrics. Call once
// during process startup. Idempotent — semstreams' MetricsRegistry
// returns success on duplicate registration.
//
// When reg is nil, this is a no-op — the CounterVec still accumulates
// values in memory and Stats() still reads them, which keeps tests and
// nil-deps construction paths working without the metrics service.
func RegisterMetrics(reg *metric.MetricsRegistry) error {
	if reg == nil {
		return nil
	}
	if err := reg.RegisterCounterVec("jsonutil", "quirks_fired_total", quirksFiredCounter); err != nil {
		return fmt.Errorf("register jsonutil quirks counter: %w", err)
	}
	return nil
}

// fireQuirk increments the CounterVec child for the given quirk and
// emits a Debug log so operators can grep per-fire when needed. The
// child is cached at init so this call is a single Inc() on the hot
// path. Idempotent on the counter (each fire = one increment); the
// caller is responsible for ensuring it only fires when the quirk
// actually transformed the input.
func fireQuirk(q QuirkID) {
	if c, ok := firedCounters[q]; ok {
		c.Inc()
	}
	slog.Default().Debug("jsonutil quirk fired", "quirk", string(q))
}

// Stats returns a snapshot of per-quirk fire counters. Includes every
// known quirk, with zero for those that haven't fired yet. Materialized
// fresh on each call so callers don't share storage. Reads the counter
// values via prometheus testutil — works whether or not RegisterMetrics
// has been called.
func Stats() map[QuirkID]int64 {
	out := make(map[QuirkID]int64, len(allQuirks))
	for _, q := range allQuirks {
		out[q] = int64(testutil.ToFloat64(firedCounters[q]))
	}
	return out
}

// ParseResult bundles the extracted JSON with the named quirks that
// fired during extraction. Callers wiring CP-1 incident telemetry use
// QuirksFired to populate parse.incident triples per ADR-035 §3 with
// their per-call context (role, model, prompt_version). Callers that
// only need the JSON string should use ExtractJSON.
type ParseResult struct {
	// JSON is the extracted, cleaned JSON string. Empty when no JSON
	// could be extracted from the input.
	JSON string

	// QuirksFired lists the named quirks that had to be applied to
	// produce JSON. Empty when the input was already clean JSON.
	//
	// Order is deterministic: fence-or-fallback (whichever applied),
	// then comments (if any), then commas (if any). New quirks append
	// at the end of this sequence. Callers that depend on positional
	// reads can rely on this ordering across releases — pinned in
	// TestParseStrict_QuirkAttribution.
	QuirksFired []QuirkID
}

// ParseStrict extracts a JSON object from an LLM response string and
// reports which named quirks (universal shape transforms) had to be
// applied. ADR-035 audit sites A.1, A.2, A.3.
//
// The returned ParseResult.JSON has the same value the legacy
// ExtractJSON would return; ParseResult.QuirksFired is the new piece
// of information for callers that want to attribute quirk fires to a
// per-call context.
func ParseStrict(content string) ParseResult {
	var result ParseResult

	raw, fenced, fallback := extractRawJSON(content)
	if raw == "" {
		return result
	}
	if fenced {
		result.QuirksFired = append(result.QuirksFired, QuirkFencedJSONWrapper)
		fireQuirk(QuirkFencedJSONWrapper)
	}
	if fallback {
		result.QuirksFired = append(result.QuirksFired, QuirkGreedyObjectFallback)
		fireQuirk(QuirkGreedyObjectFallback)
	}

	cleaned, commentsFired, commasFired := cleanJSON(raw)
	if commentsFired {
		result.QuirksFired = append(result.QuirksFired, QuirkJSLineComments)
		fireQuirk(QuirkJSLineComments)
	}
	if commasFired {
		result.QuirksFired = append(result.QuirksFired, QuirkTrailingCommas)
		fireQuirk(QuirkTrailingCommas)
	}

	// Go 1.25 rejects trailing content after JSON. Validate and trim if
	// needed. Not a named quirk — this is stdlib-compat, not LLM-output
	// tolerance (see ADR-035 audit A.4).
	if json.Valid([]byte(cleaned)) {
		result.JSON = cleaned
		return result
	}
	if trimmed := trimToBalancedJSON(cleaned); trimmed != "" {
		result.JSON = trimmed
		return result
	}
	result.JSON = cleaned
	return result
}

// ExtractJSON extracts a JSON object from an LLM response string. It
// is now a back-compat wrapper around ParseStrict that discards the
// QuirksFired list. New callers should prefer ParseStrict so they can
// attribute quirk fires to a per-call context for incident telemetry.
func ExtractJSON(content string) string {
	return ParseStrict(content).JSON
}

// ExtractJSONArray extracts a JSON array from an LLM response string.
// Fires the same named quirks as ParseStrict when transforms apply, so
// per-quirk counters and Debug logs cover array-shaped LLM output too.
// Returns only the JSON string — no ParseResult equivalent yet, since
// no array caller needs the QuirksFired list at the call site today.
// If a future caller needs that, add ParseStrictArray.
func ExtractJSONArray(content string) string {
	var raw string
	var fenced, fallback bool
	if matches := jsonArrayBlockPattern.FindStringSubmatch(content); len(matches) > 1 {
		raw = matches[1]
		fenced = true
	} else if match := jsonArrayPattern.FindString(content); match != "" {
		raw = match
		// Same prose-wrapping discrimination as the object path: the
		// greedy fallback should only fire when the matched array is
		// strictly shorter than the trimmed input (i.e., prose was
		// wrapped around it). A pure JSON-array input doesn't fire.
		if len(match) < len(strings.TrimSpace(content)) {
			fallback = true
		}
	}
	if raw == "" {
		return ""
	}
	if fenced {
		fireQuirk(QuirkFencedJSONWrapper)
	}
	if fallback {
		fireQuirk(QuirkGreedyObjectFallback)
	}
	cleaned, commentsFired, commasFired := cleanJSON(raw)
	if commentsFired {
		fireQuirk(QuirkJSLineComments)
	}
	if commasFired {
		fireQuirk(QuirkTrailingCommas)
	}
	return cleaned
}

// extractRawJSON extracts raw JSON content from the input. Returns the
// extracted substring plus two booleans:
//
//   - fenced is true when a markdown fence wrapper was stripped
//     (QuirkFencedJSONWrapper attribution at the ParseStrict layer).
//   - fallback is true when the greedy `(?s)\{[\s\S]*\}` pattern
//     matched AND the matched substring is strictly shorter than the
//     trimmed input — prose was wrapped around the JSON, no fence.
//     QuirkGreedyObjectFallback attribution. Pure-JSON inputs without
//     fences (match equals trimmed input) return both fenced=false
//     AND fallback=false — no quirk fires for clean inputs.
func extractRawJSON(content string) (string, bool, bool) {
	// Try markdown code block first.
	if matches := jsonBlockPattern.FindStringSubmatch(content); len(matches) > 1 {
		return matches[1], true, false
	}
	// Fallback to raw JSON object.
	if match := jsonObjectPattern.FindString(content); match != "" {
		// Discriminate prose-wrapping from clean-JSON: only fire the
		// greedy-fallback quirk when prose was actually wrapped around
		// the JSON. "extracted less than we received" is the signal.
		if len(match) < len(strings.TrimSpace(content)) {
			return match, false, true
		}
		return match, false, false
	}
	return "", false, false
}

// cleanJSON removes JavaScript-style comments and trailing commas from
// JSON. LLMs commonly produce these invalid JSON artifacts. Returns the
// cleaned string plus two booleans indicating which transforms actually
// fired (so callers can attribute QuirkJSLineComments and
// QuirkTrailingCommas separately). A no-op cleanup returns the input
// unchanged with both bools false.
func cleanJSON(raw string) (string, bool, bool) {
	// Strip line comments. Track whether any line was changed so the
	// caller can attribute QuirkJSLineComments only when a real strip
	// occurred (and not on every clean-input call).
	lines := strings.Split(raw, "\n")
	cleaned := make([]string, 0, len(lines))
	commentsFired := false
	for _, line := range lines {
		stripped := stripLineComment(line)
		if stripped != line {
			commentsFired = true
		}
		cleaned = append(cleaned, stripped)
	}
	result := strings.Join(cleaned, "\n")

	// Trailing-comma replacement. Compare before/after so we attribute
	// QuirkTrailingCommas only when a real fix happened.
	fixed := trailingCommaPattern.ReplaceAllString(result, "$1")
	commasFired := fixed != result

	return fixed, commentsFired, commasFired
}

// trimToBalancedJSON finds the substring from the first { to its balanced },
// handling nested braces and string escapes. Returns "" if no balanced object found.
func trimToBalancedJSON(s string) string {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && inString {
			escaped = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if ch == '{' {
			depth++
		} else if ch == '}' {
			depth--
			if depth == 0 {
				candidate := s[start : i+1]
				if json.Valid([]byte(candidate)) {
					return candidate
				}
				return ""
			}
		}
	}
	return ""
}

// stripLineComment removes a // comment from a JSON line, respecting string values.
// For example:
//
//	"path/to/file.js",          // This is a comment  → "path/to/file.js",
//	"url": "http://example.com" // comment             → "url": "http://example.com"
//	"url": "http://example.com"                        → "url": "http://example.com" (no change)
func stripLineComment(line string) string {
	// Fast path: no // at all
	if !strings.Contains(line, "//") {
		return line
	}

	// Walk the line character by character, tracking whether we're inside a string.
	inString := false
	escaped := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && inString {
			escaped = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if !inString && ch == '/' && i+1 < len(line) && line[i+1] == '/' {
			// Found a comment outside a string — strip from here
			trimmed := strings.TrimRight(line[:i], " \t")
			return trimmed
		}
	}
	return line
}
