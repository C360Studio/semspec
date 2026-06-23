package schemaparity

import (
	"reflect"
	"strings"
	"testing"
)

type sampleStruct struct {
	InSchema   string `json:"in_schema"`
	OnlyStruct string `json:"only_struct"`
	Owned      string `json:"owned"`
	Ignored    string `json:"-"`
}

// schema props with: in_schema (matches), only_schema (no struct field).
// owned is intentionally absent (it's a systemOwned struct field).
func sampleProps() map[string]any {
	return map[string]any{
		"in_schema":   map[string]any{"type": "string"},
		"only_schema": map[string]any{"type": "string"},
	}
}

// TestBidirectional_CatchesBothDirections is the negative control proving the
// guard is not vacuous: it must flag the struct-only field AND the schema-only
// field, and must NOT flag the matched field or the allowlisted systemOwned one.
func TestBidirectional_CatchesBothDirections(t *testing.T) {
	got := Bidirectional("sample", reflect.TypeOf(sampleStruct{}), sampleProps(), "owned")
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "only_struct") {
		t.Errorf("expected a violation for struct-only field only_struct; got:\n%s", joined)
	}
	if !strings.Contains(joined, "only_schema") {
		t.Errorf("expected a violation for schema-only field only_schema; got:\n%s", joined)
	}
	if strings.Contains(joined, `"in_schema"`) {
		t.Errorf("matched field in_schema should not be flagged; got:\n%s", joined)
	}
	if strings.Contains(joined, `field "owned"`) {
		t.Errorf("systemOwned field owned should not be flagged; got:\n%s", joined)
	}
}

// TestBidirectional_CleanWhenAligned proves no false positives when the struct
// and schema agree (minus the allowlisted owned field).
func TestBidirectional_CleanWhenAligned(t *testing.T) {
	props := map[string]any{
		"in_schema":   map[string]any{"type": "string"},
		"only_struct": map[string]any{"type": "string"},
	}
	if got := Bidirectional("sample", reflect.TypeOf(sampleStruct{}), props, "owned"); len(got) != 0 {
		t.Errorf("expected no violations, got: %v", got)
	}
}

// TestStaleSystemOwned flags an allowlist entry that is actually present in the
// schema (a stale exception that would mask real drift).
func TestStaleSystemOwned(t *testing.T) {
	props := map[string]any{
		"in_schema":   map[string]any{"type": "string"},
		"only_struct": map[string]any{"type": "string"},
		"owned":       map[string]any{"type": "string"},
	}
	got := Bidirectional("sample", reflect.TypeOf(sampleStruct{}), props, "owned")
	stale := false
	for _, v := range got {
		if strings.Contains(v, "remove it from the allowlist") {
			stale = true
		}
	}
	if !stale {
		t.Errorf("expected a stale-allowlist violation for owned; got: %v", got)
	}
}
