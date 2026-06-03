package workflow

import (
	"fmt"
	"sort"
)

// DeriveStoryScheduling computes Story.DependsOn edges for every Story in
// the slice and mutates the slices in place. It must be called after Sarah
// has emitted Stories (with RequirementIDs and CapabilityNames populated)
// and before the plan transitions to ready_for_execution.
//
// The algorithm runs three passes:
//
//   - Pass 1 — Semantic edges: for each Story S, compute the transitive
//     prereq closure of S.RequirementIDs over the Requirement DAG. For each
//     prereq requirement not already in S.RequirementIDs, add ALL other
//     stories whose RequirementIDs contains that prereq to S.DependsOn.
//     Uses all-coverers wait semantics: S must wait for every Story covering
//     each prereq, not just one.
//
//   - Pass 2 — Resource edges: for each unordered pair (a, b) of Stories
//     whose FilesOwned overlap:
//     (a) if a.ComponentName == b.ComponentName, return
//     ErrSameComponentFileConflict (Sarah emission bug — must collapse).
//     (b) otherwise, add a deterministic serialization edge (lower Story.ID
//     first) via appendUnique.
//
//   - Pass 3 — Cycle detection: three-color DFS on the merged DAG. A cycle
//     here indicates an invalid coverage partition (Story covers
//     non-contiguous layers of the Requirement DAG). Returns
//     ErrCoveragePartitionCyclic with the offending Story IDs and a
//     remediation hint.
//
// MUTATES the DependsOn slices of the input stories in place. Callers must
// not assume that pre-existing DependsOn values are preserved; run this
// function on the raw Sarah-emitted slice (DependsOn empty) for correctness.
// Idempotent: appendUnique prevents duplicates so calling twice yields the
// same DependsOn set.
//
// Returns nil when all edges are derived and the graph is acyclic. Returns a
// wrapped ErrSameComponentFileConflict or ErrCoveragePartitionCyclic on
// invalid inputs.
func DeriveStoryScheduling(stories []Story, reqs []Requirement) error {
	if len(stories) == 0 {
		return nil
	}

	// Pass 1 — Semantic edges.
	reqClosure := buildReqClosure(reqs)
	storyByID := make(map[string]*Story, len(stories))
	for i := range stories {
		storyByID[stories[i].ID] = &stories[i]
	}

	// coverers maps requirementID → IDs of all Stories covering it.
	coverers := make(map[string][]string, len(reqs))
	for _, s := range stories {
		for _, rid := range s.RequirementIDs {
			coverers[rid] = appendUnique(coverers[rid], s.ID)
		}
	}

	for i := range stories {
		s := &stories[i]
		// The set of requirements this story owns.
		ownedSet := make(map[string]struct{}, len(s.RequirementIDs))
		for _, rid := range s.RequirementIDs {
			ownedSet[rid] = struct{}{}
		}

		// Transitive closure of all owned requirements.
		allPrereqs := make(map[string]struct{})
		for _, rid := range s.RequirementIDs {
			for prereq := range reqClosure[rid] {
				allPrereqs[prereq] = struct{}{}
			}
		}

		// For each prereq not already owned by this story, add all coverers.
		for prereq := range allPrereqs {
			if _, owned := ownedSet[prereq]; owned {
				continue
			}
			for _, covererID := range coverers[prereq] {
				if covererID != s.ID {
					s.DependsOn = appendUnique(s.DependsOn, covererID)
				}
			}
		}
	}

	// Pass 2 — Resource edges.
	if err := applyResourceEdges(stories); err != nil {
		return err
	}

	// Pass 3 — Cycle detection on merged scheduler DAG.
	if cycle := detectSchedulerCycle(stories); cycle != nil {
		return fmt.Errorf("%w: cycle %v — a Story covers non-contiguous layers of the Requirement DAG; split or merge the offending Stories so coverage spans a contiguous DAG prefix",
			ErrCoveragePartitionCyclic, cycle)
	}

	return nil
}

// applyResourceEdges implements Pass 2 of DeriveStoryScheduling: it scans all
// unordered pairs of Stories for file-ownership overlap, returns
// ErrSameComponentFileConflict when two Stories anchor the same component and
// share files, and adds deterministic serialization edges (lower Story.ID
// runs first) for cross-component overlaps.
func applyResourceEdges(stories []Story) error {
	// Build the Pass-1 closure so we can skip pairs already ordered.
	p1Closure := buildStoryClosure(stories)

	// Normalize FilesOwned once per story for efficiency.
	normalizedFiles := make(map[string]map[string]struct{}, len(stories))
	for _, s := range stories {
		set := make(map[string]struct{}, len(s.FilesOwned))
		for _, f := range s.FilesOwned {
			if n := NormalizeFilePath(f); n != "" {
				set[n] = struct{}{}
			}
		}
		normalizedFiles[s.ID] = set
	}

	for i := 0; i < len(stories); i++ {
		a := &stories[i]
		for j := i + 1; j < len(stories); j++ {
			b := &stories[j]

			// Skip if already ordered by Pass 1.
			if alreadyOrdered(a.ID, b.ID, p1Closure) {
				continue
			}

			overlap := sharedFiles(normalizedFiles[a.ID], normalizedFiles[b.ID])
			if len(overlap) == 0 {
				continue
			}

			if a.ComponentName == b.ComponentName {
				return fmt.Errorf("%w: stories %q and %q anchor component %q and share files %v — collapse to one Story covering both coverage sets",
					ErrSameComponentFileConflict, a.ID, b.ID, a.ComponentName, overlap)
			}

			// Different components: add deterministic serialization edge
			// (lower ID runs first).
			lower, higher := a, b
			ids := []string{a.ID, b.ID}
			sort.Strings(ids)
			if ids[0] == b.ID {
				lower, higher = b, a
			}
			higher.DependsOn = appendUnique(higher.DependsOn, lower.ID)
		}
	}
	return nil
}

// buildReqClosure builds a transitive prerequisite closure for every
// Requirement.ID over the Requirement DAG. The result maps reqID → set of
// all transitive prereqs (not including reqID itself). Uses BFS per node;
// O(n²) worst case, acceptable for plan-level requirement counts (<100).
func buildReqClosure(reqs []Requirement) map[string]map[string]struct{} {
	// Build adjacency: reqID → direct DependsOn IDs.
	adj := make(map[string][]string, len(reqs))
	for _, r := range reqs {
		adj[r.ID] = r.DependsOn
	}

	closure := make(map[string]map[string]struct{}, len(reqs))
	for _, r := range reqs {
		seen := make(map[string]struct{})
		queue := append([]string(nil), adj[r.ID]...)
		for len(queue) > 0 {
			head := queue[0]
			queue = queue[1:]
			if _, ok := seen[head]; ok {
				continue
			}
			seen[head] = struct{}{}
			queue = append(queue, adj[head]...)
		}
		closure[r.ID] = seen
	}
	return closure
}

// buildStoryClosure builds the transitive DependsOn closure for each Story
// after Pass 1 edges have been added. Used by Pass 2 to skip pairs already
// ordered. Returns map[storyID] → set of storyIDs reachable from it.
func buildStoryClosure(stories []Story) map[string]map[string]struct{} {
	adj := make(map[string][]string, len(stories))
	for _, s := range stories {
		adj[s.ID] = s.DependsOn
	}

	closure := make(map[string]map[string]struct{}, len(stories))
	for _, s := range stories {
		seen := make(map[string]struct{})
		queue := append([]string(nil), adj[s.ID]...)
		for len(queue) > 0 {
			head := queue[0]
			queue = queue[1:]
			if _, ok := seen[head]; ok {
				continue
			}
			seen[head] = struct{}{}
			queue = append(queue, adj[head]...)
		}
		closure[s.ID] = seen
	}
	return closure
}

// alreadyOrdered returns true if aID is in bID's DependsOn closure OR bID
// is in aID's DependsOn closure — meaning the pair has an ordering edge.
func alreadyOrdered(aID, bID string, closure map[string]map[string]struct{}) bool {
	if _, ok := closure[aID][bID]; ok {
		return true
	}
	if _, ok := closure[bID][aID]; ok {
		return true
	}
	return false
}

// detectSchedulerCycle runs three-color DFS on the full DependsOn graph of
// stories (which may have been augmented by Pass 1 and Pass 2). Returns a
// non-nil slice containing the IDs of stories in the first cycle detected,
// or nil when the graph is acyclic.
func detectSchedulerCycle(stories []Story) []string {
	adj := make(map[string][]string, len(stories))
	for _, s := range stories {
		adj[s.ID] = s.DependsOn
	}

	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(stories))
	path := make([]string, 0, len(stories)) // current DFS path for cycle extraction

	var cycleFound []string

	var visit func(id string) bool
	visit = func(id string) bool {
		color[id] = gray
		path = append(path, id)
		for _, dep := range adj[id] {
			switch color[dep] {
			case gray:
				// Cycle: extract the path segment from dep's first appearance.
				start := -1
				for k, n := range path {
					if n == dep {
						start = k
						break
					}
				}
				if start >= 0 {
					cycleFound = append([]string(nil), path[start:]...)
				} else {
					cycleFound = []string{id, dep}
				}
				return true
			case white:
				if visit(dep) {
					return true
				}
			}
		}
		path = path[:len(path)-1]
		color[id] = black
		return false
	}

	for _, s := range stories {
		if color[s.ID] == white {
			if visit(s.ID) {
				return cycleFound
			}
		}
	}
	return nil
}

// appendUnique appends id to slice only if it is not already present.
// Returns the (possibly unchanged) slice. O(n) per call; acceptable for
// Story counts (<20 per plan).
func appendUnique(slice []string, id string) []string {
	for _, existing := range slice {
		if existing == id {
			return slice
		}
	}
	return append(slice, id)
}
