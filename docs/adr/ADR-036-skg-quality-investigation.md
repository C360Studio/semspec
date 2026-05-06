# ADR-036 — SKG Quality Investigation: bring graph_search back from "empty"

Status: Proposed (2026-05-06) — investigation in progress.
Blocker: yes. No further paid agentic runs against @hard until graph quality
is re-validated end-to-end.

## Problem

The federated Semantic Knowledge Graph (SKG) is functionally invisible to
agentic workloads. Hybrid @hard 2026-05-06 (commit `06f0eeb`) audit:

- 8 successful `graph_search` calls returned content
- **7 of 8** returned literally `"Found 0 entities but no summary available"`
- The 1 that returned matches surfaced our own workflow entities
  (`semspec.workspace.wf.plan.req.*`, `.scenario.*`) — not the
  OSH/Meshtastic/OGC code or doc entities the agent asked about
- 10K+ entities indexed across 4 namespaces; the agent cannot reach them via
  natural-language queries

Behavioral consequence: agents call graph tools at iter 0–9 of a dev loop,
get empty results, **abandon graph for the remainder of the loop**, fall
back to `bash + curl maven-central`. The federated graph stops earning
its keep within the first 90 seconds of every run.

This is a multi-day repeating finding (see `project_hybrid_hard_run_2026_05_04_graph_unused.md`,
`project_hybrid_hard_run_2026_05_04_graph_unused.md`). Token spend on
@hard scenarios is wasted as long as the SKG is dead — the entire
"graph-first architecture" premise collapses if `graph_search` returns
nothing.

## Two suspected root causes (independent)

### A. AST entities are content-sparse for NL retrieval

`processor/ast/entities.go:CodeEntity` carries only:

- `Name` — short identifier (e.g., `MeshtasticDriver`)
- `Path` — file path
- `Package` — package path
- `DocComment` — javadoc (often empty in real code)
- structural relationships (`contains`, `calls`, `references`) as entity IDs

For an NL query like *"Meshtastic Java client Maven dependency"*:

- BM25 over `Name` matches only if a class is literally `MeshtasticJavaClient`
- BM25 over `DocComment` matches only if javadoc literally contains "Maven dependency"
- Vector similarity over those sparse fields embeds mostly the identifier — not enough signal

8K AST entities are functionally invisible to NL search; great for
`traverse(callers of foo)` style structural queries, useless for
semantic.

### B. Doc entities ARE indexed (~1100 with `source.doc.content`) and STILL return empty

This is the bigger surprise. Workspace + OSH + Meshtastic + OGC ship
~1117 doc chunks with actual prose content (READMEs, markdown). Yet
graph_search returns 0 entities for queries that should match them.
Possibilities (need live probe):

1. graph_search's local_search corpus excludes doc entities
2. BM25 index doesn't include the `source.doc.content` predicate values
3. semembed didn't get to embed doc entities (timing/race during ingest)
4. Query classifier routes NL to a code-only index by default
5. Doc-content vector similarity is below the relevance threshold
6. local_search is only walking from a seed entity that's never a doc

Cause B is more impactful than A — fixing AST sparsity is moot if doc
search itself is broken.

## Investigation plan (5 phases, each a stack configuration)

Each phase uses a clean stack with NO mock-LLM or agentic load. We bring
up only the SKG substrate (nats + semspec + semembed + seminstruct-* +
graph-gateway + workspace semsource), seed it, and probe directly via
GraphQL against `graph-gateway`. **Zero paid agentic API calls.**

Acceptance criteria per phase: a fixed canon of probe queries
(`canon-queries.json` — 10–15 representative NL questions an agent
might ask, against known-good content) must return non-empty,
on-topic results.

### Phase 1 — Baseline: `enable_llm:false`, no external sources

**Stack**: nats + semspec + workspace-only semsource +
semembed + seminstruct-fast + seminstruct-answer (no summary needed).
Workspace fixture: `osh-driver-meshtastic` (minimal Java skeleton).

**Probe**: graph_summary, graph_query by known entity ID, graph_search
NL queries against:
- workspace README content ("What is this driver for?")
- workspace pom.xml content ("What dependencies are configured?")
- workspace Java AST class names ("MeshtasticDriver class")

**Pass**: at least 1 NL query returns ≥1 useful workspace doc/AST entity.

**Fail**: even baseline doc retrieval is broken — the bug is local-search
itself, not external-source quality. File semstreams ask immediately.

### Phase 2 — Same stack, `enable_llm:true` on graph-clustering

Adds community-summary path via seminstruct-summary (8B). 12G memory
cap, 1 parallel slot. graph_search globalSearch path becomes available.

**Probe**: globalSearch ("how is the driver structured?") — should return
a coherent synthesized answer over community summaries, not the
template-fallback used when synthesizer fails or COMMUNITY_INDEX is
empty.

**Pass**: globalSearch returns a sentence-shaped answer that quotes or
paraphrases workspace content; no `degraded_reason=*` in the response.

**Fail**: seminstruct-8B can't keep up — measure `community_summary` p95
latency at workspace scale (~10 entities, ~1 community). If p95 > 60s,
file semstreams ask for community-summary throughput.

### Phase 3 — Add Meshtastic external source

Re-introduces `semsource-meshtastic` from `e2e-epic.yml` overlay.
~50–200 entities expected (Meshtastic README + Java client).

**Probe**:
- NL: *"Meshtastic Java client serial communication"* → must return
  Meshtastic-domain entities (`meshtastic.semsource.web.*` or `.java.*`),
  not workspace entities
- predicate: `entitiesByPredicate(predicate: "source.doc.content")`
  filtered to Meshtastic prefix → must return doc entities

**Pass**: cross-namespace NL search ranks Meshtastic content first when
the query is Meshtastic-shaped.

**Fail**: ranking is broken — workspace entities outrank external
content despite content match. File semsource ask: configure relevance
weighting per namespace OR file semstreams ask: graph-clustering needs
namespace-aware community detection.

### Phase 4 — Add OGC Connected Systems source

Adds OGC docs (~few hundred entities). Same probe shape as Phase 3 but
with OGC-domain queries.

**Pass**: same as Phase 3 for OGC content.

### Phase 5 — Add OpenSensorHub core (full @hard topology)

Adds OSH (largest source, ~7800 entities — Java AST heavy). This is the
@hard scenario stack minus agentic load.

**Probe**: the actual queries the @hard agentic dev loops issued, taken
verbatim from the 2026-05-06 trajectory:

- *"Meshtastic Java client Maven dependency"*
- *"Meshtastic Java client library serial communication dependencies"*
- *"OpenSensorHub OSH API dependencies versions Maven"*

**Pass**: these queries return at least one on-topic result from the
correct namespace. AST entities surface for code-shape queries; doc
entities surface for prose-shape queries.

**Fail**: this is the production-blocking case. Fixes shape:

- AST richness — emit `code.doc.snippet` (first N lines of body)
- Doc index inclusion — confirm semsource doc entities are in the
  vector + BM25 indices
- Cross-namespace ranking — namespace-aware boosting in local_search

## Out of scope

- Refactoring graph-clustering's enable_llm tunables (separate ask
  tracked in `project_graph_clustering_enable_llm_regression_2026_05_04.md`)
- Replacing seminstruct with cloud (`project_gemini_lite_for_graph_synthesis.md`
  — defer until Phase 2 measurements are in)
- Changing the agentic-loop's "give up on graph" behavior — fix the
  graph; behavioral fix is a band-aid

## Asks tracked here

- **semstreams**: depending on phase outcomes, possibly:
  - graph-query local_search corpus inclusion of doc entities
  - graph-clustering community-summary throughput tuning surface
  - graph-search ranking weights per namespace
- **semsource**:
  - AST entity content enrichment (`code.doc.snippet`?)
  - confirm doc-entity content predicate is fed into the embedding +
    BM25 indices

Update `reference_semstreams_asks.md` and `reference_semsource_asks.md`
as phases complete.

## Status & signoff

Until Phase 5 passes, treat any "graph-first architecture" claim as
aspirational. Memory file `project_skg_quality_investigation.md`
tracks per-phase state. No further paid @hard runs gated on agent
graph use until then.
