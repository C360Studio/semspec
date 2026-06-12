package scenarioorchestrator

import (
	"context"
	"fmt"
	"sort"

	"github.com/c360studio/semspec/tools/sandbox"
	"github.com/c360studio/semspec/workflow"
)

// sandboxClient is the narrow slice of the sandbox API the orchestrator needs
// to resolve a requirement's branch-derivation base. Declared as an interface
// so tests inject a stub and assert the merge it performs. Satisfied by
// *sandbox.Client.
type sandboxClient interface {
	MergeBranches(ctx context.Context, req sandbox.MergeBranchesRequest) (*sandbox.MergeBranchesResult, error)
}

// newSandboxClient returns a sandboxClient backed by the real sandbox HTTP
// client, or an untyped nil interface when url is empty. The constructor avoids
// the Go nil-interface gotcha where a typed nil (*sandbox.Client)(nil) assigned
// to the interface field reads as non-nil.
func newSandboxClient(url string) sandboxClient {
	c := sandbox.NewClient(url)
	if c == nil {
		return nil
	}
	return c
}

// requirementBranch is the per-requirement work branch convention shared with
// the requirement-executor (initReqExecution) and plan-level assembly.
func requirementBranch(reqID string) string {
	return "semspec/requirement-" + reqID
}

// resolveRequirementBase computes the git ref req's branch must derive FROM so
// a DependsOn edge drives branch DERIVATION, not just dispatch timing. This is
// the orchestrator-side hook of the branch-derivation fix (design §2): a
// dependent forks from its prerequisites' work (already containing their
// shared-file edits) instead of the plan base, so plan-level assembly becomes a
// fast-forward rather than a conflict.
//
// The prerequisite set is ResolveRequirementBranchPrereqs (pure, owner-mapped),
// then:
//
//	0 prereqs -> planBranch (DAG root; "" lets the executor fall back to HEAD)
//	1 prereq  -> that single prerequisite owner branch (pure fork, no merge)
//	>1        -> merge the owner branches into "semspec/reqbase-<reqID>" (sorted,
//	             deterministic) and derive from it
//
// The >1 merge needs a sandbox; without one it is a misconfiguration (the
// sandbox is mandatory for execution) and we fail loud rather than silently
// forking from the plan base and re-introducing the assembly conflict.
func (c *Component) resolveRequirementBase(
	ctx context.Context,
	req workflow.Requirement,
	stories []workflow.Story,
	planBranch string,
) (string, error) {
	owners := workflow.ResolveRequirementBranchPrereqs(req, stories)

	switch len(owners) {
	case 0:
		// DAG root — fork from the plan base (empty => executor uses HEAD).
		return planBranch, nil
	case 1:
		return requirementBranch(owners[0]), nil
	}

	// >1 prerequisite: assemble the owner branches onto a per-requirement base
	// branch so the executor sees a single ready ref. Sorted for a deterministic
	// reqbase tree (idempotent across re-dispatch).
	branches := make([]string, 0, len(owners))
	for _, owner := range owners {
		branches = append(branches, requirementBranch(owner))
	}
	sort.Strings(branches)

	if c.sandbox == nil {
		return "", fmt.Errorf(
			"requirement %s has %d branch prerequisites but no sandbox is configured to merge them into a derivation base",
			req.ID, len(owners))
	}

	reqBase := "semspec/reqbase-" + req.ID
	result, err := c.sandbox.MergeBranches(ctx, sandbox.MergeBranchesRequest{
		Target:   reqBase,
		Base:     planBranch, // empty => sandbox uses HEAD
		Branches: branches,
		Trailers: map[string]string{"Reqbase": req.ID},
	})
	if err != nil {
		return "", fmt.Errorf("merge branch prerequisites for requirement %s into %s: %w", req.ID, reqBase, err)
	}
	return result.Target, nil
}
