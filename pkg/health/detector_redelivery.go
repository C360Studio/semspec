package health

import (
	"sort"
	"strconv"
)

// Redelivery flags JetStream consumers whose messages are being redelivered
// — the symptom of a consumer ack_wait shorter than the time the processor
// holds a message before acking. On the AGENT stream this is expensive: a
// redelivered agentic-loop task re-runs a whole loop, and a redelivered
// agentic-model request fires a DUPLICATE PAID frontier call. The 2026-06-08
// mavlink-hard run motivated #140: prod agentic-loop ack_wait (5m) was below
// the loop timeout (30m), so a slow loop's task could redeliver mid-flight.
//
// The fix is config, not code: ack_wait must exceed the longest single
// dispatch for the consumer's workload (loop timeout for agentic-loop,
// per-call timeout for agentic-model). See #140 / configs/semspec.json.
//
// Severity ladder (NumRedelivered = messages currently in a redelivered
// state on the consumer at snapshot time):
//   - >= 1 → warning: a redelivery is happening now; look before the next run.
//   - >= RedeliveryCriticalThreshold → critical: sustained churn; duplicate
//     work / duplicate paid calls are piling up — stop and fix ack_wait.
//
// Pure; reads only Bundle.JetStream. Pinned against
// testdata/fixtures/jsz-real-2026-06-11/jsz.json (idle stack, every consumer
// NumRedelivered=0 → detector clean).
type Redelivery struct{}

// RedeliveryShape is the Diagnosis.Shape value for this detector.
const RedeliveryShape = "Redelivery"

// RedeliveryCriticalThreshold is the NumRedelivered count at/above which a
// single consumer's redelivery is treated as critical rather than warning. 3
// lets a one-off redelivery (e.g. a genuine consumer restart) stay a warning
// while a systematic ack_wait-too-short storm trips a --bail-on critical.
const RedeliveryCriticalThreshold = 3

// Name implements Detector.
func (Redelivery) Name() string { return RedeliveryShape }

// Run implements Detector. Pure; no I/O.
func (Redelivery) Run(b *Bundle) []Diagnosis {
	if b == nil || b.JetStream == nil {
		return nil
	}
	consumers, err := b.JetStream.Redeliveries()
	if err != nil {
		return []Diagnosis{{
			Shape:       RedeliveryShape,
			Severity:    SeverityUndetermined,
			Remediation: "JetStream /jsz snapshot present but its consumer detail did not decode — cannot assess redelivery. Check the NATS monitor response shape at " + b.JetStream.URL + ".",
			Evidence:    []EvidenceRef{{Kind: EvidenceJetStreamConsumer, Field: "jsz", Value: err.Error()}},
		}}
	}
	// Deterministic order → stable alert keys + golden tests.
	sort.Slice(consumers, func(i, j int) bool {
		if consumers[i].Stream != consumers[j].Stream {
			return consumers[i].Stream < consumers[j].Stream
		}
		return consumers[i].Consumer < consumers[j].Consumer
	})
	var out []Diagnosis
	for _, c := range consumers {
		if c.NumRedelivered < 1 {
			continue
		}
		sev := SeverityWarning
		if c.NumRedelivered >= RedeliveryCriticalThreshold {
			sev = SeverityCritical
		}
		out = append(out, Diagnosis{
			Shape:    RedeliveryShape,
			Severity: sev,
			Evidence: []EvidenceRef{{
				Kind:  EvidenceJetStreamConsumer,
				ID:    c.Consumer,
				Field: "num_redelivered",
				Value: c.NumRedelivered,
			}},
			Remediation: "Consumer " + c.Consumer + " on stream " + c.Stream + " has " +
				strconv.Itoa(c.NumRedelivered) + " message(s) being redelivered — its ack_wait is shorter than the time the processor holds a message before acking, so JetStream is re-dispatching in-flight work (duplicate loops; on agentic-model, duplicate PAID frontier calls). Raise the consumer's ack_wait above the longest single dispatch for its workload (see #140 / configs/semspec.json).",
			MemoryRef: "feedback_e2e_active_monitoring.md",
		})
	}
	return out
}
