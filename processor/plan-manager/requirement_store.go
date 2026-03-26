package planmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
)

// requirementStore owns the lifecycle of requirement entities (wf.plan.req.*).
// Same 3-layer pattern: sync.Map cache + WriteTriple durability + reconcile on startup.
type requirementStore struct {
	cache        sync.Map // requirementID → *workflow.Requirement
	tripleWriter *graphutil.TripleWriter
	logger       *slog.Logger
}

func newRequirementStore(tw *graphutil.TripleWriter, logger *slog.Logger) *requirementStore {
	return &requirementStore{
		tripleWriter: tw,
		logger:       logger,
	}
}

// reconcile populates the cache from ENTITY_STATES on startup.
func (s *requirementStore) reconcile(ctx context.Context) {
	reconcileCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	prefix := workflow.EntityPrefix() + ".wf.plan.req."
	entities, err := s.tripleWriter.ReadEntitiesByPrefix(reconcileCtx, prefix, 500)
	if err != nil {
		s.logger.Warn("Requirement reconciliation failed", "error", err)
		return
	}

	recovered := 0
	for entityID, triples := range entities {
		if triples[semspec.RequirementStatus] == "deleted" {
			continue
		}
		req := requirementFromTripleMap(entityID, triples)
		if req.ID == "" {
			continue
		}
		s.cache.Store(req.ID, &req)
		recovered++
	}

	if recovered > 0 {
		s.logger.Info("Requirement cache reconciled from graph", "count", recovered)
	}
}

// get returns a shallow copy of a requirement by ID from the cache.
// Returns a copy to prevent data races on concurrent mutations.
func (s *requirementStore) get(id string) (*workflow.Requirement, bool) {
	val, ok := s.cache.Load(id)
	if !ok {
		return nil, false
	}
	r := *val.(*workflow.Requirement)
	return &r, true
}

// listByPlan returns all requirements for a plan slug, sorted by creation time.
func (s *requirementStore) listByPlan(slug string) []workflow.Requirement {
	planEntityID := workflow.PlanEntityID(slug)
	reqs := make([]workflow.Requirement, 0)
	s.cache.Range(func(_, value any) bool {
		req := value.(*workflow.Requirement)
		if req.PlanID == planEntityID {
			reqs = append(reqs, *req)
		}
		return true
	})
	sort.Slice(reqs, func(i, j int) bool {
		return reqs[i].CreatedAt.Before(reqs[j].CreatedAt)
	})
	return reqs
}

// save writes a requirement to cache and ENTITY_STATES triples.
func (s *requirementStore) save(ctx context.Context, req *workflow.Requirement) error {
	if err := s.writeTriples(ctx, req); err != nil {
		return err
	}
	s.cache.Store(req.ID, req)
	return nil
}

// saveAll saves a batch of requirements (with DAG validation).
func (s *requirementStore) saveAll(ctx context.Context, requirements []workflow.Requirement, slug string) error {
	if err := workflow.ValidateSlug(slug); err != nil {
		return err
	}
	if err := workflow.ValidateRequirementDAG(requirements); err != nil {
		return fmt.Errorf("invalid requirement DAG: %w", err)
	}

	planEntityID := workflow.PlanEntityID(slug)
	for i := range requirements {
		if requirements[i].PlanID == "" {
			requirements[i].PlanID = planEntityID
		}
		if err := s.save(ctx, &requirements[i]); err != nil {
			return fmt.Errorf("save requirement %s: %w", requirements[i].ID, err)
		}
	}
	return nil
}

// delete removes a requirement from cache and tombstones in ENTITY_STATES.
func (s *requirementStore) delete(ctx context.Context, id string) error {
	if s.tripleWriter != nil {
		entityID := workflow.RequirementEntityID(id)
		if err := s.tripleWriter.WriteTriple(ctx, entityID, semspec.RequirementStatus, "deleted"); err != nil {
			return fmt.Errorf("write delete tombstone: %w", err)
		}
	}
	s.cache.Delete(id)
	return nil
}

// put updates the cache for a requirement mutated externally (transitional).
func (s *requirementStore) put(req *workflow.Requirement) {
	if req != nil && req.ID != "" {
		s.cache.Store(req.ID, req)
	}
}

// writeTriples writes all requirement fields as individual triples.
func (s *requirementStore) writeTriples(ctx context.Context, req *workflow.Requirement) error {
	tw := s.tripleWriter
	if tw == nil {
		return nil
	}
	entityID := workflow.RequirementEntityID(req.ID)

	_ = tw.WriteTriple(ctx, entityID, semspec.RequirementTitle, req.Title)
	_ = tw.WriteTriple(ctx, entityID, semspec.DCTitle, req.Title)
	if err := tw.WriteTriple(ctx, entityID, semspec.RequirementStatus, string(req.Status)); err != nil {
		return fmt.Errorf("write requirement status: %w", err)
	}
	_ = tw.WriteTriple(ctx, entityID, semspec.RequirementPlan, req.PlanID)
	_ = tw.WriteTriple(ctx, entityID, semspec.RequirementCreatedAt, req.CreatedAt.Format(time.RFC3339))
	_ = tw.WriteTriple(ctx, entityID, semspec.RequirementUpdatedAt, req.UpdatedAt.Format(time.RFC3339))
	if req.Description != "" {
		_ = tw.WriteTriple(ctx, entityID, semspec.RequirementDescription, req.Description)
	}
	if dependsJSON, err := json.Marshal(req.DependsOn); err == nil {
		_ = tw.WriteTriple(ctx, entityID, semspec.RequirementDependsOn, string(dependsJSON))
	}
	return nil
}

// requirementFromTripleMap reconstructs a Requirement from a predicate→value map.
func requirementFromTripleMap(entityID string, triples map[string]string) workflow.Requirement {
	req := workflow.Requirement{
		ID:     extractRequirementID(entityID),
		PlanID: triples[semspec.RequirementPlan],
	}

	if v := triples[semspec.RequirementTitle]; v != "" {
		req.Title = v
	}
	if v := triples[semspec.RequirementStatus]; v != "" {
		req.Status = workflow.RequirementStatus(v)
	}
	if v := triples[semspec.RequirementDescription]; v != "" {
		req.Description = v
	}
	if v := triples[semspec.RequirementCreatedAt]; v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			req.CreatedAt = t
		}
	}
	if v := triples[semspec.RequirementUpdatedAt]; v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			req.UpdatedAt = t
		}
	}
	if v := triples[semspec.RequirementDependsOn]; v != "" {
		_ = json.Unmarshal([]byte(v), &req.DependsOn)
	}
	if req.DependsOn == nil {
		req.DependsOn = []string{}
	}
	return req
}

// extractRequirementID extracts the raw requirement ID from the entity ID.
func extractRequirementID(entityID string) string {
	prefix := workflow.EntityPrefix() + ".wf.plan.req."
	if len(entityID) > len(prefix) {
		return entityID[len(prefix):]
	}
	return entityID
}
