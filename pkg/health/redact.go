package health

import (
	"encoding/json"
	"strings"
)

// Redact applies the v1 default redactions to bundle IN PLACE. See
// ADR-034 §3. v1 covers the always-on subset: env-var-style field
// names and known auth-header field names. The heavier redactions
// (prompt-content stripping with allowlist file) are deferred until
// external adopters require them.
//
// What gets redacted:
//
//   - Any JSON object field whose name (case-insensitive) matches
//     one of the sensitive substrings (KEY, SECRET, TOKEN, PASSWORD,
//     PASSWD, AUTHORIZATION, API-KEY, API_KEY) has its string value
//     replaced with "<redacted>". Non-string values are left intact —
//     numbers and booleans don't carry secrets and clobbering them
//     would corrupt detector inputs.
//   - Walks Bundle.Messages[].RawData, Bundle.Plans[].Value, and
//     Bundle.Loops[].Value (the JSON-bytes blobs the bundle preserves
//     opaquely).
//
// Bundle.Bundle.Redactions is updated with the policy categories
// applied. Receivers switching on those names know exactly what's
// missing without having to diff against an unredacted bundle.
//
// Pure: deterministic over its input; no I/O. Safe to call from tests.
func Redact(bundle *Bundle) {
	if bundle == nil {
		return
	}
	for i := range bundle.Messages {
		bundle.Messages[i].RawData = redactRawJSON(bundle.Messages[i].RawData)
	}
	for i := range bundle.Plans {
		bundle.Plans[i].Value = redactRawJSON(bundle.Plans[i].Value)
	}
	for i := range bundle.Loops {
		bundle.Loops[i].Value = redactRawJSON(bundle.Loops[i].Value)
	}
	bundle.Bundle.Redactions = mergeRedactionCategories(bundle.Bundle.Redactions,
		"sensitive_field_values", "auth_header_values")
}

// redactRawJSON parses raw, walks the tree, redacts in place, and
// re-marshals. Returns the original bytes unchanged if the input is
// empty or doesn't decode (the bundle preserves opaque blobs to be
// resilient to upstream schema evolution; redacting opaque garbage
// would discard data the adopter may need).
func redactRawJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return raw
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	if walk := redactValue(v); walk != nil {
		v = walk
	}
	out, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return out
}

// redactValue recurses through maps and slices. For every map field
// whose name matches a sensitive pattern, the string value is
// replaced with the redaction sentinel. Returns the (possibly
// re-rooted) value so the caller can pick up replacement at any
// level.
func redactValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			if isSensitiveFieldName(k) {
				if _, isString := child.(string); isString {
					x[k] = redactionSentinel
					continue
				}
			}
			x[k] = redactValue(child)
		}
		return x
	case []any:
		for i, child := range x {
			x[i] = redactValue(child)
		}
		return x
	default:
		return v
	}
}

// redactionSentinel is the string substituted for any redacted
// value. Adopters can grep their bundle for this token to inspect
// what was scrubbed.
const redactionSentinel = "<redacted>"

// sensitivePatterns lists the case-insensitive substrings that
// trigger redaction when found in a JSON object field name. Kept
// minimal: false positives waste detector signal; false negatives
// leak secrets. Both are bad but the latter is unrecoverable once
// the bundle is shared.
var sensitivePatterns = []string{
	"key",
	"secret",
	"token",
	"password",
	"passwd",
	"authorization",
	"api-key",
	"api_key",
}

// isSensitiveFieldName reports whether name (case-insensitive)
// contains any sensitive substring. Pure.
func isSensitiveFieldName(name string) bool {
	lower := strings.ToLower(name)
	for _, p := range sensitivePatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// mergeRedactionCategories appends categories not already in
// existing, preserving order. Idempotent across multiple Redact
// calls — calling Redact twice should not duplicate entries.
func mergeRedactionCategories(existing []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(existing)+len(additions))
	for _, e := range existing {
		seen[e] = struct{}{}
	}
	out := append([]string(nil), existing...)
	for _, a := range additions {
		if _, ok := seen[a]; ok {
			continue
		}
		seen[a] = struct{}{}
		out = append(out, a)
	}
	return out
}
