package health

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// jszWith builds a minimal /jsz body using the field names verified against
// the real capture (see TestRedeliveries_RealFixture), with one AGENT
// consumer at the given redelivery count. Lets the threshold tests vary the
// count without a stack.
func jszWith(consumer string, numRedelivered int) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{
		"account_details": [{
			"stream_detail": [{
				"name": "AGENT",
				"consumer_detail": [{
					"stream_name": "AGENT",
					"name": %q,
					"num_redelivered": %d,
					"num_ack_pending": 1,
					"num_pending": 0
				}]
			}]
		}]
	}`, consumer, numRedelivered))
}

func TestRedeliveryDetector_RealFixtureIsClean(t *testing.T) {
	b := &Bundle{JetStream: &JetStreamSnapshot{Status: http.StatusOK, JSZ: loadJSZFixture(t)}}
	if got := (Redelivery{}).Run(b); got != nil {
		t.Fatalf("idle fixture should produce no diagnoses, got %d: %+v", len(got), got)
	}
}

func TestRedeliveryDetector_Thresholds(t *testing.T) {
	cases := []struct {
		name       string
		redelivers int
		wantSev    Severity
		wantCount  int
	}{
		{"zero is clean", 0, "", 0},
		{"one is warning", 1, SeverityWarning, 1},
		{"below critical threshold is warning", RedeliveryCriticalThreshold - 1, SeverityWarning, 1},
		{"at critical threshold is critical", RedeliveryCriticalThreshold, SeverityCritical, 1},
		{"well above threshold is critical", RedeliveryCriticalThreshold + 10, SeverityCritical, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := &Bundle{JetStream: &JetStreamSnapshot{Status: http.StatusOK, JSZ: jszWith("agentic-model-agent-request-all", tc.redelivers)}}
			got := (Redelivery{}).Run(b)
			if len(got) != tc.wantCount {
				t.Fatalf("diagnoses = %d, want %d", len(got), tc.wantCount)
			}
			if tc.wantCount == 0 {
				return
			}
			d := got[0]
			if d.Severity != tc.wantSev {
				t.Errorf("severity = %q, want %q", d.Severity, tc.wantSev)
			}
			if d.Shape != RedeliveryShape {
				t.Errorf("shape = %q, want %q", d.Shape, RedeliveryShape)
			}
			if len(d.Evidence) != 1 || d.Evidence[0].Kind != EvidenceJetStreamConsumer {
				t.Fatalf("evidence = %+v, want one EvidenceJetStreamConsumer", d.Evidence)
			}
			if d.Evidence[0].ID != "agentic-model-agent-request-all" {
				t.Errorf("evidence ID = %q, want consumer name", d.Evidence[0].ID)
			}
		})
	}
}

func TestRedeliveryDetector_NoJetStreamIsNil(t *testing.T) {
	for _, tc := range []struct {
		name string
		b    *Bundle
	}{
		{"nil bundle", nil},
		{"nil jetstream (offline replay)", &Bundle{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := (Redelivery{}).Run(tc.b); got != nil {
				t.Fatalf("got %+v, want nil", got)
			}
		})
	}
}

func TestRedeliveryDetector_MalformedIsUndetermined(t *testing.T) {
	b := &Bundle{JetStream: &JetStreamSnapshot{Status: http.StatusOK, JSZ: json.RawMessage(`{"account_details": 42}`)}}
	got := (Redelivery{}).Run(b)
	if len(got) != 1 || got[0].Severity != SeverityUndetermined {
		t.Fatalf("got %+v, want one undetermined diagnosis", got)
	}
	if got[0].Remediation == "" {
		t.Error("undetermined diagnosis must set Remediation pointing at the missing input")
	}
}
