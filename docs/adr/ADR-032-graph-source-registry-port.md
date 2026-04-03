# ADR-032: Port Semdragon's GraphSourceRegistry to Semspec

## Status: PROPOSED

## Context

Graph source management in semspec has been a recurring source of bugs:

1. **Hardcoded port 8082** — `tools/workflow/graph.go:getGatewayURL()` defaults to `http://localhost:8082` (graph-gateway standalone port), but in Docker the gateway registers on the shared HTTP mux (port 8080). Agents get connection refused.

2. **Scattered env var fallbacks** — URL resolution checks `SEMSPEC_GRAPH_GATEWAY_URL`, `GRAPH_GATEWAY_URL`, `SEMSOURCE_URL`, and `GRAPH_SOURCES` in different files with different precedence. No single source of truth.

3. **Three overlapping systems** — `graph.Registry` (poll-based source discovery, 370 lines), `graph.Gatherer` (GraphQL queries + summary HTTP, 526 lines), and `GraphExecutor` (tool implementations with own fallback chains, 480 lines) — 1800+ lines doing overlapping work with inconsistent URL resolution.

4. **Semsource manifest race** — `graph.Registry.pollSource()` gets empty bodies because semsource's HTTP handlers serve before `Start()` completes. Fixed with an empty-body guard, but the polling approach itself is fragile.

5. **graph_summary fallback ignores configured sources** — When the federated querier isn't wired, the tool falls through to a `SEMSOURCE_URL`-only path, ignoring the `GRAPH_SOURCES` array that was properly configured in the registry.

Semdragon solved all of these with a single `GraphSourceRegistry` (~700 lines) in `processor/questbridge/graphsources.go`. It has been running in production and handles:

- Config-driven sources with explicit `graphql_url` + `status_url` per source
- Readiness polling with 3-failure degraded fallback (never blocks forever)
- Prefix-based query routing for entity/relationship queries
- Fan-out to all ready sources for search/NLQ queries
- Summary URL derivation from status URL
- 5-minute summary cache with formatted prompt injection
- Example entity ID fetching for agent prompt guidance
- Global singleton pattern (`SetGlobalGraphSources` / `GlobalGraphSources`)

## Decision

Port semdragon's `GraphSourceRegistry` into semspec. This is an app-level concern (semsource discovery), not a semstreams concern (semstreams doesn't assume semsource exists).

### What we port

From `semdragon/processor/questbridge/graphsources.go`:

| Component | Purpose |
|-----------|---------|
| `GraphSource` struct | Config-driven source with graphql_url, status_url, entity_prefix, type |
| `GraphSourceRegistry` | Central registry: readiness, routing, summary, caching |
| `SourcesForQuery()` | Route by query type — entity/prefix/search/summary |
| `WaitForReady()` | Readiness polling with timeout + degraded fallback |
| `FormatSummaryForPrompt()` | Cached summary formatting for agent prompts |
| `fetchExampleIDs()` | Example entity IDs per domain for prompt guidance |
| Global singleton pattern | `SetGlobalGraphSources` / `GlobalGraphSources` |

### What we replace

| Current file | Lines | Replacement |
|-------------|-------|-------------|
| `graph/registry.go` | 370 | `GraphSourceRegistry` handles source tracking + readiness |
| `graph/gatherer.go` (summary parts) | ~80 | `GraphSourceRegistry.FormatSummaryForPrompt()` |
| `tools/workflow/graph.go` (fallback chains) | ~70 | Use registry's `SourcesForQuery("summary")` |
| `tools/workflow/manifest.go` (summary fetch) | ~60 | Use registry's `FormatSummaryForPrompt()` |

### What we keep

- `graph/gatherer.go` core GraphQL methods (`ExecuteQuery`, `GetEntity`, `QueryEntitiesByPredicate`, etc.) — these are the query execution layer, not source discovery
- `graph.Querier` interface — still useful for federated query fanout
- `FederatedGraphGatherer` — builds on Querier, uses registry for source list

### Config format

Match semdragon's JSON config (already supported by our `GRAPH_SOURCES` env var pattern):

```json
{
  "graph_sources": [
    {
      "name": "local",
      "graphql_url": "http://localhost:8080/graph-gateway/graphql",
      "type": "local",
      "always_query": true
    },
    {
      "name": "workspace",
      "graphql_url": "http://semsource:8080/graph-gateway/graphql",
      "status_url": "http://semsource:8080/source-manifest/status",
      "type": "semsource",
      "entity_prefix": "semspec.semsource."
    }
  ]
}
```

### Initialization (main.go)

Replace `initGraphRegistry()` with:

```go
func initGraphSources(cfg *config.Config, logger *slog.Logger) {
    sources := parseGraphSources(cfg) // from config or GRAPH_SOURCES env var
    if len(sources) == 0 { return }
    reg := graph.NewGraphSourceRegistry(sources, logger)
    graph.SetGlobalGraphSources(reg)
}
```

No background polling loop needed — readiness is checked lazily on first query or via `WaitForReady()` at prompt assembly time.

## Migration Path

### Phase 1: Immediate fix (done)
- Fixed GRAPH stream subjects in default config
- Added infra health endpoint for Docker healthcheck
- Empty-body guard in registry polling

### Phase 2: Port GraphSourceRegistry
1. Create `graph/sources.go` — port `GraphSourceRegistry` from semdragon
2. Wire in `main.go` — replace `initGraphRegistry()` with `initGraphSources()`
3. Update `tools/workflow/graph.go` — use registry for all URL resolution
4. Update `tools/workflow/manifest.go` — use registry's `FormatSummaryForPrompt()`
5. Simplify `graph/registry.go` — remove poll loop, env var parsing (registry owns this now)
6. Update compose/Docker configs to use `graph_sources` config format
7. Update all env var references in docs

### Phase 3: Cleanup
- Remove `getGatewayURL()` and scattered env var checks
- Remove `SEMSOURCE_URL` legacy fallback (superseded by `graph_sources`)
- Remove `graph.Registry.pollLoop` (readiness is lazy, not poll-based)
- Update CLAUDE.md environment variables table

## Evidence: Bugs this prevents

| Bug | Root cause | How registry prevents it |
|-----|-----------|------------------------|
| graph_summary hardcoded 8082 | `getGatewayURL()` default | Registry has explicit graphql_url per source |
| Semsource manifest empty body | Poll before Start() completes | Lazy readiness check, not continuous polling |
| GRAPH_SOURCES ignored by tools | Fallback doesn't check registry | All tools go through registry |
| Multiple env vars with different precedence | Each file checks different vars | One config format, one parse point |
| Frontend graph_summary fails in Docker | Wrong URL construction | Registry derives summary URL from status URL |

## Consequences

- **Positive**: One source of truth for graph source configuration. Battle-tested pattern from semdragon. Eliminates an entire class of URL/readiness bugs.
- **Positive**: Summary caching (5-min TTL) reduces redundant HTTP calls during prompt assembly.
- **Positive**: Prefix-based routing enables clean multi-source federation.
- **Negative**: Port effort — ~700 lines to adapt, ~300 lines to remove, config migration.
- **Risk**: Semdragon's registry lives in `questbridge` package (semdragon-specific). Our port needs to be generic enough for semspec's component model. The core logic is app-agnostic; only the prompt formatting is domain-specific.
