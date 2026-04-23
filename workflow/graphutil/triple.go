// Package graphutil provides shared graph write helpers used across orchestrator
// components. Centralising writeTriple and portSubject here removes the
// verbatim copy that previously existed in review-orchestrator,
// execution-orchestrator, scenario-executor, plan-coordinator, and
// plan-decision-handler.
package graphutil

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
)

// TripleWriter provides graph triple write capabilities via NATS request/reply.
// It wraps a NATS client and logger, eliminating per-component boilerplate for
// the writeTriple pattern.
//
// Usage:
//
//	tw := &graphutil.TripleWriter{
//	    NATSClient:    deps.NATSClient,
//	    Logger:        logger,
//	    ComponentName: "my-component",
//	}
//	if err := tw.WriteTriple(ctx, entityID, wf.Phase, "generating"); err != nil {
//	    // handle error
//	}
type TripleWriter struct {
	NATSClient    *natsclient.Client
	Logger        *slog.Logger
	ComponentName string
}

// WriteTriple sends an AddTripleRequest to graph-ingest via NATS request/reply.
// graph-ingest handles CAS writes to ENTITY_STATES KV and returns a KVRevision.
//
// Pass numeric values (int, int64, float64) directly — do not format them as
// strings. The graph store accepts any JSON-serialisable object value.
//
// Returns an error on failure; callers should error-check critical triples
// (e.g., workflow.phase) and can safely ignore non-critical ones with _.
func (tw *TripleWriter) WriteTriple(ctx context.Context, entityID, predicate string, object any) error {
	req := graph.AddTripleRequest{
		Triple: message.Triple{
			Subject:    entityID,
			Predicate:  predicate,
			Object:     object,
			Source:     tw.ComponentName,
			Timestamp:  time.Now(),
			Confidence: 1.0,
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		tw.Logger.Warn("Failed to marshal triple request", "predicate", predicate, "error", err)
		return fmt.Errorf("marshal triple request: %w", err)
	}

	if tw.NATSClient == nil {
		return nil
	}

	respData, err := tw.NATSClient.RequestWithRetry(ctx, "graph.mutation.triple.add", data, 5*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		tw.Logger.Warn("Triple write request failed",
			"predicate", predicate, "entity_id", entityID, "error", err)
		return fmt.Errorf("triple write request: %w", err)
	}

	var resp graph.AddTripleResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		tw.Logger.Warn("Failed to unmarshal triple response", "predicate", predicate, "error", err)
		return fmt.Errorf("unmarshal triple response: %w", err)
	}

	if !resp.Success {
		tw.Logger.Warn("Triple write rejected by graph-ingest",
			"predicate", predicate, "entity_id", entityID, "error", resp.Error)
		return fmt.Errorf("triple write rejected: %s", resp.Error)
	}

	return nil
}

// ReadEntity fetches an entity's triples from ENTITY_STATES via graph-ingest
// NATS request/reply. Returns a map of predicate → object (as string).
// Non-string objects are JSON-encoded.
func (tw *TripleWriter) ReadEntity(ctx context.Context, entityID string) (map[string]string, error) {
	if tw.NATSClient == nil {
		return nil, fmt.Errorf("NATS client not configured")
	}

	reqData, err := json.Marshal(map[string]string{"id": entityID})
	if err != nil {
		return nil, fmt.Errorf("marshal entity query: %w", err)
	}

	respData, err := tw.NATSClient.RequestWithRetry(ctx, "graph.ingest.query.entity", reqData, 5*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		return nil, fmt.Errorf("query entity %s: %w", entityID, err)
	}

	var entity graph.EntityState
	if err := json.Unmarshal(respData, &entity); err != nil {
		return nil, fmt.Errorf("unmarshal entity %s: %w", entityID, err)
	}

	result := make(map[string]string, len(entity.Triples))
	for _, t := range entity.Triples {
		switch v := t.Object.(type) {
		case string:
			result[t.Predicate] = v
		case float64:
			if v == float64(int64(v)) {
				result[t.Predicate] = fmt.Sprintf("%d", int64(v))
			} else {
				result[t.Predicate] = fmt.Sprintf("%g", v)
			}
		case bool:
			result[t.Predicate] = fmt.Sprintf("%t", v)
		default:
			data, _ := json.Marshal(v)
			result[t.Predicate] = string(data)
		}
	}

	return result, nil
}

// ReadEntitiesByPrefix fetches all entities matching an ID prefix from
// ENTITY_STATES via graph-ingest. Returns a map of entityID → predicate map.
// Each predicate maps to the last observed value; for multi-valued predicates
// use ReadEntitiesByPrefixMulti.
func (tw *TripleWriter) ReadEntitiesByPrefix(ctx context.Context, prefix string, limit int) (map[string]map[string]string, error) {
	if tw.NATSClient == nil {
		return nil, fmt.Errorf("NATS client not configured")
	}

	if limit <= 0 {
		limit = 100
	}

	reqData, err := json.Marshal(map[string]any{"prefix": prefix, "limit": limit})
	if err != nil {
		return nil, fmt.Errorf("marshal prefix query: %w", err)
	}

	respData, err := tw.NATSClient.RequestWithRetry(ctx, "graph.ingest.query.prefix", reqData, 10*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		return nil, fmt.Errorf("query prefix %s: %w", prefix, err)
	}

	var resp struct {
		Entities []graph.EntityState `json:"entities"`
	}
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal prefix response: %w", err)
	}

	result := make(map[string]map[string]string, len(resp.Entities))
	for _, entity := range resp.Entities {
		triples := make(map[string]string, len(entity.Triples))
		for _, t := range entity.Triples {
			switch v := t.Object.(type) {
			case string:
				triples[t.Predicate] = v
			case float64:
				if v == float64(int64(v)) {
					triples[t.Predicate] = fmt.Sprintf("%d", int64(v))
				} else {
					triples[t.Predicate] = fmt.Sprintf("%g", v)
				}
			case bool:
				triples[t.Predicate] = fmt.Sprintf("%t", v)
			default:
				data, _ := json.Marshal(v)
				triples[t.Predicate] = string(data)
			}
		}
		result[entity.ID] = triples
	}

	return result, nil
}

// ReadEntitiesByPrefixMulti fetches all entities matching an ID prefix from
// ENTITY_STATES via graph-ingest. Returns a map of entityID → predicate →
// []values, preserving every value written for multi-valued predicates (e.g.
// RequirementDependsOn, ScenarioThen, PlanDecisionMutates).
func (tw *TripleWriter) ReadEntitiesByPrefixMulti(ctx context.Context, prefix string, limit int) (map[string]map[string][]string, error) {
	if tw.NATSClient == nil {
		return nil, fmt.Errorf("NATS client not configured")
	}

	if limit <= 0 {
		limit = 100
	}

	reqData, err := json.Marshal(map[string]any{"prefix": prefix, "limit": limit})
	if err != nil {
		return nil, fmt.Errorf("marshal prefix query: %w", err)
	}

	respData, err := tw.NATSClient.RequestWithRetry(ctx, "graph.ingest.query.prefix", reqData, 10*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		return nil, fmt.Errorf("query prefix %s: %w", prefix, err)
	}

	var resp struct {
		Entities []graph.EntityState `json:"entities"`
	}
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal prefix response: %w", err)
	}

	result := make(map[string]map[string][]string, len(resp.Entities))
	for _, entity := range resp.Entities {
		multi := make(map[string][]string, len(entity.Triples))
		for _, t := range entity.Triples {
			var s string
			switch v := t.Object.(type) {
			case string:
				s = v
			case float64:
				if v == float64(int64(v)) {
					s = fmt.Sprintf("%d", int64(v))
				} else {
					s = fmt.Sprintf("%g", v)
				}
			case bool:
				s = fmt.Sprintf("%t", v)
			default:
				data, _ := json.Marshal(v)
				s = string(data)
			}
			multi[t.Predicate] = append(multi[t.Predicate], s)
		}
		result[entity.ID] = multi
	}

	return result, nil
}
