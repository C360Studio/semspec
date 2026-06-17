package openspec

import (
	"fmt"
	"strings"

	"github.com/c360studio/semspec/workflow"
)

func writeContractReference(sb *strings.Builder, plan *workflow.Plan) {
	if plan == nil || plan.Contract == nil || plan.Contract.ID == "" {
		return
	}
	sb.WriteString("> Contract packet: `")
	sb.WriteString(plan.Contract.ID)
	sb.WriteString("`")
	if plan.Contract.Version > 0 {
		fmt.Fprintf(sb, " (v%d)", plan.Contract.Version)
	}
	sb.WriteString("\n")
	if len(plan.Contract.Amendments) > 0 {
		sb.WriteString("> Accepted amendments: ")
		for i, amendment := range plan.Contract.Amendments {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString("`")
			sb.WriteString(amendment.ID)
			sb.WriteString("`")
			if amendment.PlanDecisionID != "" {
				sb.WriteString(" via `")
				sb.WriteString(amendment.PlanDecisionID)
				sb.WriteString("`")
			}
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
}
