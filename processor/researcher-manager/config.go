package researchermanager

// Config holds the researcher-manager component's tunable parameters. Kept
// minimal in R1 — only the KV bucket name is configurable so e2e fixtures
// can isolate from a shared instance if needed. Dispatch model, iter budget,
// and persona are R3 concerns and will land alongside the actual dispatch
// wiring.
type Config struct {
	// Bucket is the KV bucket name. Defaults to workflow.ResearchBucket
	// ("RESEARCH") when empty.
	Bucket string `json:"bucket,omitempty" schema:"type:string,description:KV bucket name for research records,category:basic"`
}
