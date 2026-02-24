package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// PhasesJSONFile is the filename for machine-readable phase storage (JSON format).
const PhasesJSONFile = "phases.json"

// Sentinel errors for phase operations.
var (
	ErrPhaseNotFound      = errors.New("phase not found")
	ErrPhaseInvalidStatus = errors.New("invalid phase status transition")
	ErrPhaseNameRequired  = errors.New("phase name is required")
)

// phaseLocks provides per-slug mutex for safe concurrent phase updates.
var (
	phaseLocksMu sync.Mutex
	phaseLocks   = make(map[string]*sync.Mutex)
)

// getPhaseLock returns a mutex for the given slug, creating one if needed.
func getPhaseLock(slug string) *sync.Mutex {
	phaseLocksMu.Lock()
	defer phaseLocksMu.Unlock()

	if phaseLocks[slug] == nil {
		phaseLocks[slug] = &sync.Mutex{}
	}
	return phaseLocks[slug]
}

// CreatePhase creates a new Phase with the given parameters.
func CreatePhase(planID, planSlug string, seq int, name, description string) (*Phase, error) {
	if err := ValidateSlug(planSlug); err != nil {
		return nil, err
	}

	if name == "" {
		return nil, ErrPhaseNameRequired
	}

	return &Phase{
		ID:        PhaseEntityID(planSlug, seq),
		PlanID:    planID,
		Sequence:  seq,
		Name:      name,
		Description: description,
		DependsOn: []string{},
		Status:    PhaseStatusPending,
		CreatedAt: time.Now(),
	}, nil
}

// SavePhases saves phases to .semspec/projects/default/plans/{slug}/phases.json.
func (m *Manager) SavePhases(ctx context.Context, phases []Phase, slug string) error {
	if err := ValidateSlug(slug); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	phasesPath := filepath.Join(m.ProjectPlanPath(DefaultProjectSlug, slug), PhasesJSONFile)

	// Ensure directory exists
	dir := filepath.Dir(phasesPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	data, err := json.MarshalIndent(phases, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal phases: %w", err)
	}

	if err := os.WriteFile(phasesPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write phases: %w", err)
	}

	return nil
}

// LoadPhases loads phases from .semspec/projects/default/plans/{slug}/phases.json.
func (m *Manager) LoadPhases(ctx context.Context, slug string) ([]Phase, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	phasesPath := filepath.Join(m.ProjectPlanPath(DefaultProjectSlug, slug), PhasesJSONFile)

	data, err := os.ReadFile(phasesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []Phase{}, nil
		}
		return nil, fmt.Errorf("failed to read phases: %w", err)
	}

	var phases []Phase
	if err := json.Unmarshal(data, &phases); err != nil {
		return nil, fmt.Errorf("failed to parse phases: %w", err)
	}

	return phases, nil
}

// GetPhase retrieves a single phase by ID.
func (m *Manager) GetPhase(ctx context.Context, slug, phaseID string) (*Phase, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}

	phases, err := m.LoadPhases(ctx, slug)
	if err != nil {
		return nil, err
	}

	for i := range phases {
		if phases[i].ID == phaseID {
			return &phases[i], nil
		}
	}

	return nil, fmt.Errorf("%w: %s", ErrPhaseNotFound, phaseID)
}

// UpdatePhase updates an existing phase.
// Returns error if phase is active, complete, or failed (state guard).
func (m *Manager) UpdatePhase(ctx context.Context, slug string, req UpdatePhaseRequest) (*Phase, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	lock := getPhaseLock(slug)
	lock.Lock()
	defer lock.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	phases, err := m.LoadPhases(ctx, slug)
	if err != nil {
		return nil, err
	}

	var phaseIndex = -1
	for i := range phases {
		if phases[i].ID == req.PhaseID {
			phaseIndex = i
			break
		}
	}

	if phaseIndex == -1 {
		return nil, fmt.Errorf("%w: %s", ErrPhaseNotFound, req.PhaseID)
	}

	phase := &phases[phaseIndex]

	// State guard: cannot update active, complete, or failed phases
	if phase.Status == PhaseStatusActive || phase.Status == PhaseStatusComplete || phase.Status == PhaseStatusFailed {
		return nil, fmt.Errorf("%w: cannot update phase with status %s", ErrPhaseInvalidStatus, phase.Status)
	}

	if req.Name != nil {
		phase.Name = *req.Name
	}
	if req.Description != nil {
		phase.Description = *req.Description
	}
	if req.DependsOn != nil {
		phase.DependsOn = req.DependsOn
	}
	if req.RequiresApproval != nil {
		phase.RequiresApproval = *req.RequiresApproval
	}
	if req.AgentConfig != nil {
		phase.AgentConfig = req.AgentConfig
	}

	if err := m.SavePhases(ctx, phases, slug); err != nil {
		return nil, err
	}

	return phase, nil
}

// DeletePhase deletes a phase.
// Returns error if phase is active, complete, or failed (state guard).
// Removes deleted phase from other phases' DependsOn.
func (m *Manager) DeletePhase(ctx context.Context, slug, phaseID string) error {
	if err := ValidateSlug(slug); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	lock := getPhaseLock(slug)
	lock.Lock()
	defer lock.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	phases, err := m.LoadPhases(ctx, slug)
	if err != nil {
		return err
	}

	var deleteIndex = -1
	for i := range phases {
		if phases[i].ID == phaseID {
			deleteIndex = i
			break
		}
	}

	if deleteIndex == -1 {
		return fmt.Errorf("%w: %s", ErrPhaseNotFound, phaseID)
	}

	phase := &phases[deleteIndex]

	// State guard
	if phase.Status == PhaseStatusActive || phase.Status == PhaseStatusComplete || phase.Status == PhaseStatusFailed {
		return fmt.Errorf("%w: cannot delete phase with status %s", ErrPhaseInvalidStatus, phase.Status)
	}

	// Remove the phase
	phases = append(phases[:deleteIndex], phases[deleteIndex+1:]...)

	// Renumber sequences
	for i := range phases {
		phases[i].Sequence = i + 1
	}

	// Remove deleted phase from DependsOn lists
	for i := range phases {
		newDeps := []string{}
		for _, depID := range phases[i].DependsOn {
			if depID != phaseID {
				newDeps = append(newDeps, depID)
			}
		}
		phases[i].DependsOn = newDeps
	}

	return m.SavePhases(ctx, phases, slug)
}

// ApprovePhase approves an individual phase for execution.
func (m *Manager) ApprovePhase(ctx context.Context, slug, phaseID, approvedBy string) (*Phase, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	lock := getPhaseLock(slug)
	lock.Lock()
	defer lock.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	phases, err := m.LoadPhases(ctx, slug)
	if err != nil {
		return nil, err
	}

	var approvedPhase *Phase
	now := time.Now()

	for i := range phases {
		if phases[i].ID == phaseID {
			if !phases[i].RequiresApproval {
				return nil, fmt.Errorf("phase does not require approval")
			}
			if phases[i].Approved {
				return nil, fmt.Errorf("phase already approved")
			}
			phases[i].Approved = true
			phases[i].ApprovedBy = approvedBy
			phases[i].ApprovedAt = &now
			approvedPhase = &phases[i]
			break
		}
	}

	if approvedPhase == nil {
		return nil, fmt.Errorf("%w: %s", ErrPhaseNotFound, phaseID)
	}

	if err := m.SavePhases(ctx, phases, slug); err != nil {
		return nil, err
	}

	return approvedPhase, nil
}

// RejectPhase rejects a phase with a reason.
func (m *Manager) RejectPhase(ctx context.Context, slug, phaseID, reason string) (*Phase, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}

	if reason == "" {
		return nil, ErrRejectionReasonRequired
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	lock := getPhaseLock(slug)
	lock.Lock()
	defer lock.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	phases, err := m.LoadPhases(ctx, slug)
	if err != nil {
		return nil, err
	}

	var rejectedPhase *Phase

	for i := range phases {
		if phases[i].ID == phaseID {
			phases[i].Approved = false
			phases[i].ApprovedBy = ""
			phases[i].ApprovedAt = nil
			rejectedPhase = &phases[i]
			break
		}
	}

	if rejectedPhase == nil {
		return nil, fmt.Errorf("%w: %s", ErrPhaseNotFound, phaseID)
	}

	if err := m.SavePhases(ctx, phases, slug); err != nil {
		return nil, err
	}

	return rejectedPhase, nil
}

// ApproveAllPhases approves all phases that require approval.
// Returns the list of newly approved phases.
func (m *Manager) ApproveAllPhases(ctx context.Context, slug, approvedBy string) ([]Phase, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	lock := getPhaseLock(slug)
	lock.Lock()
	defer lock.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	phases, err := m.LoadPhases(ctx, slug)
	if err != nil {
		return nil, err
	}

	var approved []Phase
	now := time.Now()

	for i := range phases {
		if phases[i].RequiresApproval && !phases[i].Approved {
			phases[i].Approved = true
			phases[i].ApprovedBy = approvedBy
			phases[i].ApprovedAt = &now
			approved = append(approved, phases[i])
		}
	}

	if len(approved) > 0 {
		if err := m.SavePhases(ctx, phases, slug); err != nil {
			return nil, err
		}
	}

	return approved, nil
}

// ApprovePhasePlan transitions the plan from phases_generated to phases_approved.
// This is called when the phase review loop approves all phases.
func (m *Manager) ApprovePhasePlan(ctx context.Context, plan *Plan) error {
	now := time.Now()
	plan.PhasesApproved = true
	plan.PhasesApprovedAt = &now
	plan.Status = StatusPhasesApproved
	return nil
}

// ReorderPhases reorders phases according to the given phase ID order.
func (m *Manager) ReorderPhases(ctx context.Context, slug string, phaseIDs []string) error {
	if err := ValidateSlug(slug); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	lock := getPhaseLock(slug)
	lock.Lock()
	defer lock.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	phases, err := m.LoadPhases(ctx, slug)
	if err != nil {
		return err
	}

	if len(phaseIDs) != len(phases) {
		return fmt.Errorf("phase ID count (%d) does not match existing phase count (%d)", len(phaseIDs), len(phases))
	}

	// Build a lookup map
	phaseMap := make(map[string]*Phase, len(phases))
	for i := range phases {
		phaseMap[phases[i].ID] = &phases[i]
	}

	// Reorder
	reordered := make([]Phase, 0, len(phases))
	for seq, id := range phaseIDs {
		p, ok := phaseMap[id]
		if !ok {
			return fmt.Errorf("%w: %s", ErrPhaseNotFound, id)
		}
		p.Sequence = seq + 1
		reordered = append(reordered, *p)
	}

	return m.SavePhases(ctx, reordered, slug)
}

// LoadTasksByPhase loads tasks filtered by phase ID.
func (m *Manager) LoadTasksByPhase(ctx context.Context, slug, phaseID string) ([]Task, error) {
	tasks, err := m.LoadTasks(ctx, slug)
	if err != nil {
		return nil, err
	}

	var filtered []Task
	for _, t := range tasks {
		if t.PhaseID == phaseID {
			filtered = append(filtered, t)
		}
	}

	return filtered, nil
}

// CreatePhaseRequest contains parameters for creating a phase via API.
type CreatePhaseRequest struct {
	Name             string
	Description      string
	DependsOn        []string
	RequiresApproval bool
	AgentConfig      *PhaseAgentConfig
}

// UpdatePhaseRequest contains parameters for updating a phase.
// All fields except PhaseID are optional - only non-nil fields will be updated.
type UpdatePhaseRequest struct {
	PhaseID          string
	Name             *string
	Description      *string
	DependsOn        []string
	RequiresApproval *bool
	AgentConfig      *PhaseAgentConfig
}

// CreatePhaseManual creates a phase manually (not via LLM).
// The phase is created in pending status and sequence is auto-incremented.
func (m *Manager) CreatePhaseManual(ctx context.Context, slug string, req CreatePhaseRequest) (*Phase, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}

	if req.Name == "" {
		return nil, ErrPhaseNameRequired
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Verify plan exists
	plan, err := m.LoadPlan(ctx, slug)
	if err != nil {
		return nil, err
	}

	lock := getPhaseLock(slug)
	lock.Lock()
	defer lock.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Load existing phases to determine next sequence
	phases, err := m.LoadPhases(ctx, slug)
	if err != nil {
		return nil, err
	}

	nextSeq := len(phases) + 1

	phase := &Phase{
		ID:               PhaseEntityID(slug, nextSeq),
		PlanID:           plan.ID,
		Sequence:         nextSeq,
		Name:             req.Name,
		Description:      req.Description,
		DependsOn:        req.DependsOn,
		Status:           PhaseStatusPending,
		AgentConfig:      req.AgentConfig,
		RequiresApproval: req.RequiresApproval,
		CreatedAt:        time.Now(),
	}

	if phase.DependsOn == nil {
		phase.DependsOn = []string{}
	}

	phases = append(phases, *phase)
	if err := m.SavePhases(ctx, phases, slug); err != nil {
		return nil, err
	}

	return phase, nil
}
