# Operator-declared rules (mavlink-decode 2026-05-28)

These are the operator-declared files the agent had to obey, as the
fixture shipped them:

- **`standards.json`** — empty `rules: []`. This run did not exercise
  SOP compliance, so plan-reviewer findings on SOP categories couldn't
  trigger. Take-30 shipped 17 SOPs across 6 categories; this run uses
  the minimum baseline (no SOPs) because the prompt is bounded enough
  that SOP enforcement doesn't add signal.
- **`checklist.json`** — three required checks the structural-validator
  runs at every scenario merge:
  - `go-build` (`go build ./...`, 120s timeout, required)
  - `go-vet` (`go vet ./...`, 60s timeout, required)
  - `go-test` (`go test ./...`, 300s timeout, required)

The `go-test` check is the one that actually exercised the agent's
generated `main_test.go` against the agent's `main.go` during structural
validation. **This — not the qa-reviewer — was the only test execution
gate that ran in this scenario** (qa.level=synthesis = LLM verdict, no
test execution). See the main README's "QA limitations" section for
analysis of why and what we'd want next.

The fixture matches the existing `go-project` skeleton at
`test/e2e/fixtures/go-project/.semspec/checklist.json` byte-for-byte
except for the `created_at` timestamp.
