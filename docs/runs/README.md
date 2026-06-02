## Real-LLM Smoke Run Archive

Curated snapshots of full end-to-end semspec runs against real LLMs.
Preserved here for progression tracking — what worked, what broke,
and what each subsequent run improved.

Each `smoke-N-...` directory contains:

- `README.md` — honest verdict + what the run proved + what it surfaced
- `planning/` — BMAD persona outputs (plan, architecture, requirements, scenarios)
- `openspec/` — ADR-040 spec-as-code projection (proposal, design, tasks, per-capability specs)
- `generated-code/` — actual source the executor pipeline produced
- `bug-evidence/` — rejection findings + final plan state
- `forensics/` — timestamped watch heartbeats + recovery/rejection trail

The full ~200MB watch sidecar artifacts (per-minute snapshots,
message log dumps, all KV buckets, all 50+ trajectories) are
preserved off-repo at `/tmp/sponsor-pack-...` when a run is fresh,
and may be archived to long-term storage. Only the curated artifacts
live in git.

### Naming

`smoke-N-<provider>-<tier>-<YYYY-MM-DD>/` where N is the run sequence
across providers (smoke 1 = first ever; smoke 6 = the sixth in
chronological order, regardless of provider).

### What goes here vs. elsewhere

- **goes here**: every run that produced learnings worth preserving
  for sponsors / future-us — includes failed runs IF the failure
  cause is well-characterized and the rendered planning docs landed.
- **doesn't go here**: smoke-test sanity runs (e.g. easy fixture
  validation), runs aborted before plan generation, smokes superseded
  by a same-day re-run that exercised the same surface.

### Index

| Run | Date | Provider · Tier | Outcome |
|---|---|---|---|
| 6 | 2026-06-02 | hybrid-gpt5 · mavlink-hard | 1/5 reqs complete · 3 bugs surfaced & fixed |
