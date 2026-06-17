package prompt

import "github.com/c360studio/semspec/workflow"

// ContractProjectionProfile is the closed set of contract views rendered to
// BMAD/OpenSpec roles. Multiple prompt roles may share one profile when their
// contract obligations are equivalent.
type ContractProjectionProfile string

const (
	ContractProjectionPlanner              ContractProjectionProfile = "planner"
	ContractProjectionArchitect            ContractProjectionProfile = "architect"
	ContractProjectionRequirementGenerator ContractProjectionProfile = "requirement-generator"
	ContractProjectionStoryPreparer        ContractProjectionProfile = "story-preparer"
	ContractProjectionScenarioGenerator    ContractProjectionProfile = "scenario-generator"
	ContractProjectionDeveloper            ContractProjectionProfile = "developer"
	ContractProjectionReviewer             ContractProjectionProfile = "reviewer"
	ContractProjectionRecovery             ContractProjectionProfile = "recovery"
	ContractProjectionQA                   ContractProjectionProfile = "qa"
)

// ContractProjection is the prompt-safe role view of workflow.ContractPacket.
// It keeps contract identity stable while trimming sections that a role cannot
// act on directly.
type ContractProjection struct {
	Role    Role
	Profile ContractProjectionProfile

	ID      string
	Version int
	Brief   string

	SourceRefs            []workflow.ContractSourceRef
	Constraints           []string
	AcceptanceObligations []string
	ForbiddenMoves        []string
	Scope                 ContractScopeProjection
	TopologyFacts         []ContractTopologyFact
	Amendments            []ContractAmendmentProjection
	ValidationFindings    []ContractValidationFindingProjection
}

// ContractScopeProjection is a prompt-safe copy of the root scope snapshot.
type ContractScopeProjection struct {
	Include    []string
	Exclude    []string
	DoNotTouch []string
	Create     []string
}

// ContractTopologyFact is a prompt-safe copy of workflow.TopologyFact.
type ContractTopologyFact struct {
	Kind     string
	Path     string
	Value    string
	Evidence []string
}

// ContractAmendmentProjection is a prompt-safe copy of accepted amendment
// evidence.
type ContractAmendmentProjection struct {
	ID             string
	PlanDecisionID string
	ImpactKind     workflow.ContractImpactKind
	ImpactSummary  string
	AffectedIDs    []string
}

// ContractValidationFindingProjection is a prompt-safe copy of contract
// validation output.
type ContractValidationFindingProjection struct {
	ID       string
	Severity string
	Category string
	Message  string
	Evidence []string
}

func PlannerContractProjection(plan *workflow.Plan) *ContractProjection {
	return BuildContractProjection(plan, RolePlanner)
}

func ArchitectContractProjection(plan *workflow.Plan) *ContractProjection {
	return BuildContractProjection(plan, RoleArchitect)
}

func RequirementGeneratorContractProjection(plan *workflow.Plan) *ContractProjection {
	return BuildContractProjection(plan, RoleRequirementGenerator)
}

func StoryPreparerContractProjection(plan *workflow.Plan) *ContractProjection {
	return BuildContractProjection(plan, RoleStoryPreparer)
}

func ScenarioGeneratorContractProjection(plan *workflow.Plan) *ContractProjection {
	return BuildContractProjection(plan, RoleScenarioGenerator)
}

func DeveloperContractProjection(plan *workflow.Plan) *ContractProjection {
	return BuildContractProjection(plan, RoleDeveloper)
}

func ReviewerContractProjection(plan *workflow.Plan) *ContractProjection {
	return BuildContractProjection(plan, RoleReviewer)
}

func RecoveryContractProjection(plan *workflow.Plan) *ContractProjection {
	return BuildContractProjection(plan, RoleRecoveryAgent)
}

func QAContractProjection(plan *workflow.Plan) *ContractProjection {
	return BuildContractProjection(plan, RolePlanQAReviewer)
}

// BuildContractProjection builds the role-scoped view for plan.Contract.
func BuildContractProjection(plan *workflow.Plan, role Role) *ContractProjection {
	if plan == nil {
		return nil
	}
	return BuildContractProjectionFromPacket(plan.Contract, role)
}

// BuildContractProjectionFromPacket builds the role-scoped view for a contract
// packet. It returns nil for nil packets so legacy plans remain compatible.
func BuildContractProjectionFromPacket(packet *workflow.ContractPacket, role Role) *ContractProjection {
	if packet == nil {
		return nil
	}
	profile := ContractProjectionProfileForRole(role)
	proj := &ContractProjection{
		Role:    role,
		Profile: profile,
		ID:      packet.ID,
		Version: packet.Version,
		Brief:   packet.Brief,
	}

	if includeSourceRefs(profile) {
		proj.SourceRefs = append([]workflow.ContractSourceRef(nil), packet.SourceRefs...)
	}
	if includeConstraints(profile) {
		proj.Constraints = append([]string(nil), packet.Constraints...)
	}
	if includeAcceptanceObligations(profile) {
		proj.AcceptanceObligations = append([]string(nil), packet.AcceptanceObligations...)
	}
	if includeForbiddenMoves(profile) {
		proj.ForbiddenMoves = append([]string(nil), packet.ForbiddenMoves...)
	}
	if includeScope(profile) {
		proj.Scope = copyContractScope(packet.Scope)
	}
	if includeTopologyFacts(profile) {
		proj.TopologyFacts = copyTopologyFacts(packet.TopologyFacts)
	}
	if includeAmendments(profile) {
		proj.Amendments = copyContractAmendments(packet.Amendments)
	}
	if includeValidationFindings(profile) {
		proj.ValidationFindings = copyContractValidationFindings(packet.ValidationFindings)
	}

	return proj
}

// ContractProjectionProfileForRole maps concrete prompt roles to the contract
// view they should receive.
func ContractProjectionProfileForRole(role Role) ContractProjectionProfile {
	switch role {
	case RolePlanner:
		return ContractProjectionPlanner
	case RoleArchitect:
		return ContractProjectionArchitect
	case RoleRequirementGenerator:
		return ContractProjectionRequirementGenerator
	case RoleStoryPreparer:
		return ContractProjectionStoryPreparer
	case RoleScenarioGenerator:
		return ContractProjectionScenarioGenerator
	case RoleDeveloper:
		return ContractProjectionDeveloper
	case RolePlanReviewer, RoleTaskReviewer, RoleReviewer, RoleScenarioReviewer, RoleValidator:
		return ContractProjectionReviewer
	case RoleRecoveryAgent:
		return ContractProjectionRecovery
	case RoleQA, RolePlanQAReviewer:
		return ContractProjectionQA
	default:
		return ContractProjectionPlanner
	}
}

func includeSourceRefs(profile ContractProjectionProfile) bool {
	return profile == ContractProjectionPlanner || profile == ContractProjectionRecovery || profile == ContractProjectionQA
}

func includeConstraints(profile ContractProjectionProfile) bool {
	return profile != ""
}

func includeAcceptanceObligations(profile ContractProjectionProfile) bool {
	return profile != ContractProjectionArchitect
}

func includeForbiddenMoves(profile ContractProjectionProfile) bool {
	return profile != ContractProjectionRequirementGenerator
}

func includeScope(profile ContractProjectionProfile) bool {
	switch profile {
	case ContractProjectionScenarioGenerator:
		return false
	default:
		return true
	}
}

func includeTopologyFacts(profile ContractProjectionProfile) bool {
	switch profile {
	case ContractProjectionArchitect,
		ContractProjectionStoryPreparer,
		ContractProjectionDeveloper,
		ContractProjectionReviewer,
		ContractProjectionRecovery,
		ContractProjectionQA:
		return true
	default:
		return false
	}
}

func includeAmendments(profile ContractProjectionProfile) bool {
	return profile != ContractProjectionPlanner
}

func includeValidationFindings(profile ContractProjectionProfile) bool {
	switch profile {
	case ContractProjectionReviewer, ContractProjectionRecovery, ContractProjectionQA:
		return true
	default:
		return false
	}
}

func copyContractScope(scope workflow.ContractScopeSnapshot) ContractScopeProjection {
	return ContractScopeProjection{
		Include:    append([]string(nil), scope.Include...),
		Exclude:    append([]string(nil), scope.Exclude...),
		DoNotTouch: append([]string(nil), scope.DoNotTouch...),
		Create:     append([]string(nil), scope.Create...),
	}
}

func copyTopologyFacts(facts []workflow.TopologyFact) []ContractTopologyFact {
	if len(facts) == 0 {
		return nil
	}
	out := make([]ContractTopologyFact, 0, len(facts))
	for _, fact := range facts {
		out = append(out, ContractTopologyFact{
			Kind:     fact.Kind,
			Path:     fact.Path,
			Value:    fact.Value,
			Evidence: append([]string(nil), fact.Evidence...),
		})
	}
	return out
}

func copyContractAmendments(amendments []workflow.ContractAmendment) []ContractAmendmentProjection {
	if len(amendments) == 0 {
		return nil
	}
	out := make([]ContractAmendmentProjection, 0, len(amendments))
	for _, amendment := range amendments {
		out = append(out, ContractAmendmentProjection{
			ID:             amendment.ID,
			PlanDecisionID: amendment.PlanDecisionID,
			ImpactKind:     amendment.Impact.Kind,
			ImpactSummary:  amendment.Impact.Summary,
			AffectedIDs:    append([]string(nil), amendment.Impact.AffectedIDs...),
		})
	}
	return out
}

func copyContractValidationFindings(findings []workflow.ContractValidationFinding) []ContractValidationFindingProjection {
	if len(findings) == 0 {
		return nil
	}
	out := make([]ContractValidationFindingProjection, 0, len(findings))
	for _, finding := range findings {
		out = append(out, ContractValidationFindingProjection{
			ID:       finding.ID,
			Severity: finding.Severity,
			Category: finding.Category,
			Message:  finding.Message,
			Evidence: append([]string(nil), finding.Evidence...),
		})
	}
	return out
}
