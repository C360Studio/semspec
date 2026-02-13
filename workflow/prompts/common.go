package prompts

// GapDetectionInstructions provides shared instructions for LLMs to signal knowledge gaps.
// This is appended to all workflow prompts.
const GapDetectionInstructions = `
## Knowledge Gaps

If you encounter any uncertainty, unknown information, or need clarification during document generation, signal this with a <gap> block. DO NOT guess or make assumptions about uncertain information.

**When to use gaps:**
- Missing API or interface details
- Unclear requirements or specifications
- Architecture decisions that need stakeholder input
- Security considerations requiring expert review
- Performance trade-offs needing team discussion

**Gap format:**
` + "```xml" + `
<gap>
  <topic>category.subcategory</topic>
  <question>Your specific question here?</question>
  <context>Why you need this information</context>
  <urgency>normal</urgency>
</gap>
` + "```" + `

**Topic categories:**
- api.* - API/interface questions (e.g., api.semstreams, api.authentication)
- architecture.* - Design decisions (e.g., architecture.database, architecture.messaging)
- requirements.* - Requirements clarification (e.g., requirements.auth, requirements.ux)
- security.* - Security considerations (e.g., security.tokens, security.encryption)
- performance.* - Performance trade-offs (e.g., performance.caching, performance.indexing)

**Urgency levels:**
- low - Nice to know, can proceed with reasonable assumption
- normal - Should be answered before implementation
- high - Important decision, should be answered soon
- blocking - Cannot proceed without this information

Include gaps inline where they're relevant. The workflow will pause until critical gaps are answered.
`

// WithGapDetection appends gap detection instructions to a prompt.
func WithGapDetection(prompt string) string {
	return prompt + "\n" + GapDetectionInstructions
}
