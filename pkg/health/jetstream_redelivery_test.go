package health

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// loadJSZFixture reads the pinned real /jsz capture. Captured 2026-06-11 from
// an idle dev stack (docker compose up -d → :8222/jsz?streams=true&consumers=
// true&accounts=true), so every consumer's num_redelivered is 0 — the parser
// is pinned against the real wire shape, not a guessed one.
func loadJSZFixture(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join("testdata", "fixtures", "jsz-real-2026-06-11", "jsz.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read jsz fixture: %v", err)
	}
	return data
}

func TestRedeliveries_RealFixture(t *testing.T) {
	snap := &JetStreamSnapshot{Status: http.StatusOK, JSZ: loadJSZFixture(t)}
	got, err := snap.Redeliveries()
	if err != nil {
		t.Fatalf("Redeliveries: %v", err)
	}
	if len(got) != 63 {
		t.Fatalf("consumer count = %d, want 63 (all streams in fixture)", len(got))
	}

	byName := make(map[string]ConsumerRedelivery, len(got))
	for _, c := range got {
		byName[c.Consumer] = c
		if c.NumRedelivered != 0 {
			t.Errorf("idle fixture consumer %q has num_redelivered=%d, want 0", c.Consumer, c.NumRedelivered)
		}
	}

	// The two consumers whose ack_wait #140 bumped must parse with the
	// AGENT stream attached.
	for _, name := range []string{"agentic-loop-agent-task-any", "agentic-model-agent-request-all"} {
		c, ok := byName[name]
		if !ok {
			t.Fatalf("expected consumer %q in fixture", name)
		}
		if c.Stream != "AGENT" {
			t.Errorf("%q stream = %q, want AGENT", name, c.Stream)
		}
	}
}

func TestRedeliveries_AbsentOrNon200(t *testing.T) {
	cases := []struct {
		name string
		snap *JetStreamSnapshot
	}{
		{"nil snapshot", nil},
		{"non-200 (monitoring disabled page)", &JetStreamSnapshot{Status: http.StatusNotFound, JSZ: json.RawMessage(`monitoring disabled`)}},
		{"empty body", &JetStreamSnapshot{Status: http.StatusOK}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.snap.Redeliveries()
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if got != nil {
				t.Fatalf("got = %v, want nil", got)
			}
		})
	}
}

func TestRedeliveries_MalformedBodyErrors(t *testing.T) {
	snap := &JetStreamSnapshot{Status: http.StatusOK, JSZ: json.RawMessage(`{"account_details": "not-an-array"}`)}
	if _, err := snap.Redeliveries(); err == nil {
		t.Fatal("expected decode error on malformed jsz body, got nil")
	}
}
