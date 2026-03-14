package prompt

// GraphManifestFragment returns a Fragment that injects a knowledge graph
// manifest into the prompt. The fetchFn closure returns the formatted manifest
// string, or "" if unavailable.
func GraphManifestFragment(fetchFn func() string) *Fragment {
	return &Fragment{
		ID:       "core.knowledge-manifest",
		Category: CategoryKnowledgeManifest,
		Priority: 0,
		Condition: func(ctx *AssemblyContext) bool {
			return fetchFn() != ""
		},
		ContentFunc: func(ctx *AssemblyContext) string {
			return fetchFn()
		},
	}
}
