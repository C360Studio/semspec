// Package prompts provides LLM prompt templates for workflow document generation
// and review output structures for plan validation and aggregation.
package prompts

// GapDetectionInstructions was the gap detection prompt (removed — nothing
// consumes <gap> blocks; the Q&A system handles questions via ask_question tool).
// Kept as empty string to avoid breaking legacy prompt concatenation in
// planner.go and developer.go.
const GapDetectionInstructions = ""

// WithGapDetection is a no-op retained for backward compatibility.
func WithGapDetection(prompt string) string {
	return prompt
}
