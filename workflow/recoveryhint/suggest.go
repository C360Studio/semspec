// Package recoveryhint computes directive recovery suggestions for
// tool calls that fail with a "near-miss" error (e.g. graph_query
// "not found") and emits per-fire telemetry to the SKG via
// tool.recovery.* predicates.
//
// Symmetric to workflow/parseincident — that package handles
// parse-checkpoint compensation (CP-1/CP-2 in ADR-035 framing); this
// package handles tool-call recovery hints. They live in separate
// packages because the shapes don't unify: parse incidents pivot on
// (checkpoint, outcome, quirk); tool recovery pivots on (tool_name,
// outcome, candidate). Sharing a single helper would force one shape
// to model both; better to keep them clean and let operators query
// each namespace independently.
package recoveryhint

// Suggest ranks candidates against target by Levenshtein distance and
// returns the closest n. Stable for tied distances (preserves input
// order). Returns nil when candidates is empty or n <= 0.
//
// Levenshtein on the full string is intentionally simple — splitting
// by `.` and weighting per-segment distance would be marginally
// better at picking namespace-boundary matches but adds complexity
// not yet justified by data. Promote to segment-aware ranking when a
// real fixture shows the simple ranker missing the right match.
func Suggest(target string, candidates []string, n int) []string {
	if len(candidates) == 0 || n <= 0 {
		return nil
	}
	type ranked struct {
		s   string
		d   int
		idx int // for stable sort on ties
	}
	scored := make([]ranked, 0, len(candidates))
	for i, c := range candidates {
		if c == "" {
			continue
		}
		scored = append(scored, ranked{s: c, d: levenshtein(target, c), idx: i})
	}
	// Insertion sort by (distance, idx) — tiny input set, keeps stable.
	for i := 1; i < len(scored); i++ {
		j := i
		for j > 0 {
			a, b := scored[j], scored[j-1]
			if a.d < b.d || (a.d == b.d && a.idx < b.idx) {
				scored[j], scored[j-1] = scored[j-1], scored[j]
				j--
				continue
			}
			break
		}
	}
	if n > len(scored) {
		n = len(scored)
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, scored[i].s)
	}
	return out
}

// levenshtein computes edit distance between a and b. Standard DP.
// Allocates O(min(len(a), len(b))) space.
func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	// Ensure b is the shorter — keeps the row buffer minimal.
	if la < lb {
		a, b = b, a
		la, lb = lb, la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			ins := curr[j-1] + 1
			del := prev[j] + 1
			sub := prev[j-1] + cost
			m := ins
			if del < m {
				m = del
			}
			if sub < m {
				m = sub
			}
			curr[j] = m
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}
