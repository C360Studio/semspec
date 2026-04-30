package health

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestIsSensitiveFieldName(t *testing.T) {
	cases := map[string]bool{
		"api_key":        true,
		"API_KEY":        true,
		"X-API-Key":      true,
		"authorization":  true,
		"Authorization":  true,
		"AUTHORIZATION":  true,
		"OPENAI_API_KEY": true,
		"SECRET_VALUE":   true,
		"github_token":   true,
		"db_password":    true,
		"PASSWD":         true,
		"keystore":       true, // contains "key" — false positive accepted
		// "token" matches "completion_tokens" / "prompt_tokens" by
		// substring. That's a true semantic match (both have token in
		// the name); the value-type guard in redactValue ensures
		// numeric counters aren't corrupted —
		// TestRedact_LeavesNonStringValuesIntact pins that contract.
		"completion_tokens": true,
		"prompt_tokens":     true,
		"name":              false,
		"role":              false,
		"content":           false,
		"finish_reason":     false,
	}
	for name, want := range cases {
		if got := isSensitiveFieldName(name); got != want {
			t.Errorf("isSensitiveFieldName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestRedact_ScrubsTopLevelSensitiveFields(t *testing.T) {
	bundle := &Bundle{
		Messages: []Message{
			{
				Sequence: 1,
				Subject:  "agent.request.foo",
				RawData: json.RawMessage(`{
					"id": "abc",
					"headers": {"Authorization": "Bearer sk-secret-123", "Content-Type": "application/json"},
					"api_key": "k-9999",
					"data": "non-secret"
				}`),
			},
		},
	}
	Redact(bundle)

	var got map[string]any
	if err := json.Unmarshal(bundle.Messages[0].RawData, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["api_key"] != redactionSentinel {
		t.Errorf("api_key not redacted: %v", got["api_key"])
	}
	headers := got["headers"].(map[string]any)
	if headers["Authorization"] != redactionSentinel {
		t.Errorf("Authorization not redacted: %v", headers["Authorization"])
	}
	if headers["Content-Type"] != "application/json" {
		t.Errorf("Content-Type wrongly redacted: %v", headers["Content-Type"])
	}
	if got["data"] != "non-secret" {
		t.Errorf("data wrongly redacted: %v", got["data"])
	}
	if got["id"] != "abc" {
		t.Errorf("id wrongly redacted: %v", got["id"])
	}
}

func TestRedact_RecursesIntoArraysAndNestedObjects(t *testing.T) {
	bundle := &Bundle{
		Plans: []KVEntry{
			{Key: "plan-1", Value: json.RawMessage(`{
				"requirements": [
					{"name": "req-1", "secret": "leaked", "config": {"api_key": "leaked2"}},
					{"name": "req-2", "config": {"normal": "ok"}}
				]
			}`)},
		},
	}
	Redact(bundle)

	var got map[string]any
	if err := json.Unmarshal(bundle.Plans[0].Value, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	reqs := got["requirements"].([]any)
	r0 := reqs[0].(map[string]any)
	if r0["secret"] != redactionSentinel {
		t.Errorf("nested secret not redacted: %v", r0["secret"])
	}
	cfg0 := r0["config"].(map[string]any)
	if cfg0["api_key"] != redactionSentinel {
		t.Errorf("deeply nested api_key not redacted: %v", cfg0["api_key"])
	}
	r1 := reqs[1].(map[string]any)
	cfg1 := r1["config"].(map[string]any)
	if cfg1["normal"] != "ok" {
		t.Errorf("non-sensitive sibling clobbered: %v", cfg1["normal"])
	}
}

func TestRedact_LeavesNonStringValuesIntact(t *testing.T) {
	// completion_tokens is an int detectors read; clobbering it would
	// break ThinkingSpiral. Sensitive-pattern field names with non-
	// string values are left alone (numbers and booleans don't carry
	// secrets).
	bundle := &Bundle{
		Messages: []Message{
			{Sequence: 1, Subject: "agent.response.x",
				RawData: json.RawMessage(`{"usage": {"completion_tokens": 915}, "secret_count": 5, "valid_token": true}`),
			},
		},
	}
	Redact(bundle)
	var got map[string]any
	_ = json.Unmarshal(bundle.Messages[0].RawData, &got)
	usage := got["usage"].(map[string]any)
	if usage["completion_tokens"].(float64) != 915 {
		t.Errorf("completion_tokens corrupted: %v", usage["completion_tokens"])
	}
	if got["secret_count"].(float64) != 5 {
		t.Errorf("numeric secret_count clobbered: %v", got["secret_count"])
	}
	if got["valid_token"].(bool) != true {
		t.Errorf("bool valid_token clobbered: %v", got["valid_token"])
	}
}

func TestRedact_SetsRedactionsManifest(t *testing.T) {
	bundle := &Bundle{
		Messages: []Message{{RawData: json.RawMessage(`{"x": 1}`)}},
	}
	Redact(bundle)
	if len(bundle.Bundle.Redactions) == 0 {
		t.Fatal("expected non-empty Redactions manifest")
	}
	want := []string{"sensitive_field_values", "auth_header_values"}
	for _, w := range want {
		if !slices.Contains(bundle.Bundle.Redactions, w) {
			t.Errorf("Redactions missing %q (got %v)", w, bundle.Bundle.Redactions)
		}
	}
}

func TestRedact_IsIdempotent(t *testing.T) {
	bundle := &Bundle{
		Messages: []Message{{RawData: json.RawMessage(`{"api_key":"x"}`)}},
	}
	Redact(bundle)
	first := bundle.Bundle.Redactions
	Redact(bundle)
	second := bundle.Bundle.Redactions
	if len(first) != len(second) {
		t.Errorf("Redact not idempotent: %v -> %v", first, second)
	}
}

func TestRedact_NilBundleSafe(_ *testing.T) {
	// Defensive: a nil bundle should not panic. Capture should never
	// pass nil but the standalone API should handle it.
	Redact(nil)
}

func TestRedact_MalformedJSONLeftIntact(t *testing.T) {
	// The bundle preserves opaque KV bytes to stay resilient to
	// upstream schema evolution. If a bundle reader can't decode a
	// blob, redacting it would discard data the adopter might still
	// recover from manually.
	garbage := json.RawMessage(`{not json`)
	bundle := &Bundle{
		Plans: []KVEntry{{Value: garbage}},
	}
	Redact(bundle)
	if !strings.Contains(string(bundle.Plans[0].Value), "{not json") {
		t.Errorf("malformed bytes were modified: %q", bundle.Plans[0].Value)
	}
}
