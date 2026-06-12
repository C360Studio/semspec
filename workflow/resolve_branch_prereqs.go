package workflow

import "sort"

// ownerRequirementFor returns the requirement ID whose branch carries the work
// for the story covering reqID — the DeterministicStoryOwner of that story.
//
// Under ADR-044 M:N, only a Story's owner runs the dev loop; non-owner
// requirements fast-complete with no commits on their own branch. So a dependent
// must derive from the OWNER's branch, never a non-owner's (which is empty).
// When no Story covers reqID (legacy/mock plans without Stories), reqID owns its
// own work.
func ownerRequirementFor(reqID string, stories []Story) string {
	for _, s := range stories {
		if storyCoversReq(s, reqID) {
			if owner := DeterministicStoryOwner(s); owner != "" {
				return owner
			}
		}
	}
	return reqID
}

// ResolveRequirementBranchPrereqs returns the set of OWNER requirement IDs whose
// branches req's branch must derive FROM, so a DependsOn edge drives git branch
// derivation (the dependent forks from its prerequisites' work) rather than only
// dispatch timing. Without this, every requirement branch forks from the plan
// base and two requirements editing a shared file conflict at plan-level assembly.
//
// The result is the union of two sources, both mapped to owner branches:
//
//  1. req.DependsOn — the requirement's own semantic prerequisites (authored by
//     John), each mapped to the owner requirement of its covering Story.
//  2. For every Story covering req, the DeterministicStoryOwner of each
//     Story.DependsOn entry. These carry the Pass-2 file-overlap serialization
//     edges (DeriveStoryScheduling) that are NEVER copied to Requirement.DependsOn
//     — the load-bearing gap this resolves.
//
// req's own ID is always excluded. The result is deterministic (sorted) and
// de-duplicated, so the same owner reached via both sources appears once.
func ResolveRequirementBranchPrereqs(req Requirement, stories []Story) []string {
	set := make(map[string]struct{})

	// Source 1: the requirement's own semantic prereqs, mapped to owner branches.
	for _, dep := range req.DependsOn {
		if owner := ownerRequirementFor(dep, stories); owner != req.ID {
			set[owner] = struct{}{}
		}
	}

	// Source 2: Story-level edges (including Pass-2 file-overlap) for every Story
	// covering req — the edges that never reach Requirement.DependsOn.
	storyByID := make(map[string]Story, len(stories))
	for _, s := range stories {
		storyByID[s.ID] = s
	}
	for _, s := range stories {
		if !storyCoversReq(s, req.ID) {
			continue
		}
		for _, depStoryID := range s.DependsOn {
			depStory, ok := storyByID[depStoryID]
			if !ok {
				continue
			}
			if owner := DeterministicStoryOwner(depStory); owner != "" && owner != req.ID {
				set[owner] = struct{}{}
			}
		}
	}

	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func storyCoversReq(s Story, reqID string) bool {
	for _, rid := range s.RequirementIDs {
		if rid == reqID {
			return true
		}
	}
	return false
}
