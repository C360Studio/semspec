package workflow

import "time"

// PlanContractID returns the stable identifier for a plan's authoritative root
// contract packet. The packet is stored on the Plan entity, but a stable ID
// gives prompts, graph facts, diagnostics, and UI rows a common reference.
func PlanContractID(slug string) string {
	return EntityPrefix() + ".wf.plan.contract." + HashInstanceID(slug, "root")
}

// NewContractPacket builds the root contract packet captured at plan creation
// time. The packet intentionally starts small: later detectors and accepted
// PlanDecisions add topology facts and amendments without mutating the root
// identity.
func NewContractPacket(slug, brief string, scope Scope, constraints []string, now time.Time) *ContractPacket {
	if now.IsZero() {
		now = time.Now()
	}
	return &ContractPacket{
		ID:          PlanContractID(slug),
		Version:     1,
		Brief:       brief,
		SourceRefs:  []ContractSourceRef{{Kind: "user_brief", Ref: slug}},
		Constraints: append([]string(nil), constraints...),
		Scope: ContractScopeSnapshot{
			Include:    append([]string(nil), scope.Include...),
			Exclude:    append([]string(nil), scope.Exclude...),
			DoNotTouch: append([]string(nil), scope.DoNotTouch...),
			Create:     append([]string(nil), scope.Create...),
		},
		CreatedAt: now,
	}
}

// EnsureContractPacket initializes p.Contract when it is absent. Existing
// packets are left untouched so accepted amendments and root identity stay
// stable.
func (p *Plan) EnsureContractPacket(brief string, now time.Time) {
	if p == nil || p.Contract != nil {
		return
	}
	p.Contract = NewContractPacket(p.Slug, brief, p.Scope, p.Constraints, now)
}
