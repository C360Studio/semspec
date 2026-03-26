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

// scenarioStore owns the lifecycle of scenario entities (wf.plan.scenario.*).
// Same 3-layer pattern: sync.Map cache + WriteTriple durability + reconcile on startup.
type scenarioStore struct {
	cache        sync.Map // scenarioID → *workflow.Scenario
	tripleWriter *graphutil.TripleWriter
	logger       *slog.Logger
}

func newScenarioStore(tw *graphutil.TripleWriter, logger *slog.Logger) *scenarioStore {
	return &scenarioStore{
		tripleWriter: tw,
		logger:       logger,
	}
}

// reconcile populates the cache from ENTITY_STATES on startup.
func (s *scenarioStore) reconcile(ctx context.Context) {
	reconcileCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	prefix := workflow.EntityPrefix() + ".wf.plan.scenario."
	entities, err := s.tripleWriter.ReadEntitiesByPrefix(reconcileCtx, prefix, 500)
	if err != nil {
		s.logger.Warn("Scenario reconciliation failed", "error", err)
		return
	}

	recovered := 0
	for entityID, triples := range entities {
		if triples[semspec.ScenarioStatus] == "deleted" {
			continue
		}
		sc := scenarioFromTripleMap(entityID, triples)
		if sc.ID == "" {
			continue
		}
		s.cache.Store(sc.ID, &sc)
		recovered++
	}

	if recovered > 0 {
		s.logger.Info("Scenario cache reconciled from graph", "count", recovered)
	}
}

// get returns a shallow copy of a scenario by ID from the cache.
// Returns a copy to prevent data races on concurrent mutations.
func (s *scenarioStore) get(id string) (*workflow.Scenario, bool) {
	val, ok := s.cache.Load(id)
	if !ok {
		return nil, false
	}
	sc := *val.(*workflow.Scenario)
	return &sc, true
}

// listByRequirement returns all scenarios for a requirement ID.
func (s *scenarioStore) listByRequirement(requirementID string) []workflow.Scenario {
	scenarios := make([]workflow.Scenario, 0)
	s.cache.Range(func(_, value any) bool {
		sc := value.(*workflow.Scenario)
		if sc.RequirementID == requirementID {
			scenarios = append(scenarios, *sc)
		}
		return true
	})
	sort.Slice(scenarios, func(i, j int) bool {
		return scenarios[i].CreatedAt.Before(scenarios[j].CreatedAt)
	})
	return scenarios
}

// listByPlan returns all scenarios belonging to any requirement of the given plan.
func (s *scenarioStore) listByPlan(slug string, reqs *requirementStore) []workflow.Scenario {
	planReqs := reqs.listByPlan(slug)
	reqIDs := make(map[string]bool, len(planReqs))
	for _, req := range planReqs {
		reqIDs[req.ID] = true
	}

	scenarios := make([]workflow.Scenario, 0)
	s.cache.Range(func(_, value any) bool {
		sc := value.(*workflow.Scenario)
		if reqIDs[sc.RequirementID] {
			scenarios = append(scenarios, *sc)
		}
		return true
	})
	sort.Slice(scenarios, func(i, j int) bool {
		return scenarios[i].CreatedAt.Before(scenarios[j].CreatedAt)
	})
	return scenarios
}

// save writes a scenario to cache and ENTITY_STATES triples.
func (s *scenarioStore) save(ctx context.Context, sc *workflow.Scenario) error {
	if err := s.writeTriples(ctx, sc); err != nil {
		return err
	}
	s.cache.Store(sc.ID, sc)
	return nil
}

// saveAll saves a batch of scenarios.
func (s *scenarioStore) saveAll(ctx context.Context, scenarios []workflow.Scenario, slug string) error {
	if err := workflow.ValidateSlug(slug); err != nil {
		return err
	}
	for i := range scenarios {
		if err := s.save(ctx, &scenarios[i]); err != nil {
			return fmt.Errorf("save scenario %s: %w", scenarios[i].ID, err)
		}
	}
	return nil
}

// delete removes a scenario from cache.
func (s *scenarioStore) delete(ctx context.Context, id string) error {
	if s.tripleWriter != nil {
		entityID := workflow.ScenarioEntityID(id)
		if err := s.tripleWriter.WriteTriple(ctx, entityID, semspec.ScenarioStatus, "deleted"); err != nil {
			return fmt.Errorf("write delete tombstone: %w", err)
		}
	}
	s.cache.Delete(id)
	return nil
}

// put updates the cache for a scenario mutated externally (transitional).
func (s *scenarioStore) put(sc *workflow.Scenario) {
	if sc != nil && sc.ID != "" {
		s.cache.Store(sc.ID, sc)
	}
}

// writeTriples writes all scenario fields as individual triples.
func (s *scenarioStore) writeTriples(ctx context.Context, sc *workflow.Scenario) error {
	tw := s.tripleWriter
	if tw == nil {
		return nil
	}
	entityID := workflow.ScenarioEntityID(sc.ID)

	_ = tw.WriteTriple(ctx, entityID, semspec.ScenarioGiven, sc.Given)
	_ = tw.WriteTriple(ctx, entityID, semspec.ScenarioWhen, sc.When)
	if err := tw.WriteTriple(ctx, entityID, semspec.ScenarioStatus, string(sc.Status)); err != nil {
		return fmt.Errorf("write scenario status: %w", err)
	}
	_ = tw.WriteTriple(ctx, entityID, semspec.ScenarioRequirement, workflow.RequirementEntityID(sc.RequirementID))
	_ = tw.WriteTriple(ctx, entityID, semspec.ScenarioCreatedAt, sc.CreatedAt.Format(time.RFC3339))

	title := sc.When
	if len(title) > 100 {
		title = title[:97] + "..."
	}
	_ = tw.WriteTriple(ctx, entityID, semspec.DCTitle, title)

	if thenJSON, err := json.Marshal(sc.Then); err == nil {
		_ = tw.WriteTriple(ctx, entityID, semspec.ScenarioThen, string(thenJSON))
	}
	return nil
}

// scenarioFromTripleMap reconstructs a Scenario from a predicate→value map.
func scenarioFromTripleMap(entityID string, triples map[string]string) workflow.Scenario {
	s := workflow.Scenario{
		ID: extractScenarioID(entityID),
	}

	if v := triples[semspec.ScenarioGiven]; v != "" {
		s.Given = v
	}
	if v := triples[semspec.ScenarioWhen]; v != "" {
		s.When = v
	}
	if v := triples[semspec.ScenarioStatus]; v != "" {
		s.Status = workflow.ScenarioStatus(v)
	}
	if v := triples[semspec.ScenarioCreatedAt]; v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			s.CreatedAt = t
		}
	}
	if v := triples[semspec.ScenarioRequirement]; v != "" {
		s.RequirementID = extractRequirementID(v)
	}
	if v := triples[semspec.ScenarioThen]; v != "" {
		_ = json.Unmarshal([]byte(v), &s.Then)
	}
	if s.Then == nil {
		s.Then = []string{}
	}
	return s
}

// extractScenarioID extracts the raw scenario ID from the entity ID.
func extractScenarioID(entityID string) string {
	prefix := workflow.EntityPrefix() + ".wf.plan.scenario."
	if len(entityID) > len(prefix) {
		return entityID[len(prefix):]
	}
	return entityID
}
