# Graph Tools Bypass Federated Querier — Single-Source Only

## Status: OPEN (low priority — see Scope Clarification)

## Severity: Low for headless semsource, Medium for non-NATS external sources

## Summary

`graph_search` and `graph_query` do direct HTTP to a single `gatewayURL`, bypassing
the federated querier entirely. With multiple semsource instances, agents only see
data from the local gateway. Only `graph_summary` uses the federated path.

## Evidence

In `tools/workflow/graph.go`:
- `graphSearch()` calls `e.executeGraphQL()` → direct HTTP POST to `gatewayURL/graphql`
- `queryGraph()` calls `e.executeGraphQL()` → same direct HTTP
- `graphSummary()` calls `e.querier.GraphSummary()` → federated fan-out ✓

The `Querier` interface (`graph/querier.go`) has no `ExecuteQuery` method, so raw
GraphQL can't be routed through federation.

## Semdragon Reference

Semdragon solves this with `GraphSearchRouter.GraphQLURLsForQuery(queryType, entityID, prefix)`:
- Agent sees available sources + prefixes via `graph_summary`
- `entity`/`relationships` queries → route to prefix-matching source
- `search`/`nlq` queries → fan out to ALL ready sources
- `prefix` queries → route to matching source

Location: `semdragon/processor/questbridge/graphsources.go:149` (`SourcesForQuery`)

## Scope Clarification (2026-04-02)

**Headless semsource** (standard E2E): All data flows via NATS `graph.ingest.entity` to the
local graph-gateway. Querying the local gateway only is **correct** — it has all entities.
No federation needed at the GraphQL layer.

**Multi-source (e2e-epic)**: External semsources also publish to shared NATS, so the local
graph-gateway aggregates everything. Local-only query is still correct.

**True federation needed only if**: External semsources have data NOT flowing through shared
NATS (e.g., standalone instances without NATS connectivity). This scenario does not currently
exist in any deployment configuration.

The separate issue of `graph-gateway/source-manifest/summary` returning empty was caused by
missing `GRAPH_SOURCES` env var in `docker/compose/e2e.yml` — fixed separately.

## Fix Required (deferred)

1. Add source-aware routing to `graph/querier.go` (like semdragon's GraphSearchRouter)
2. `graph_summary` should return source list with entity ID prefixes
3. `graph_search` and `graph_query` route to correct source(s) based on query params
4. Fan-out for NLQ/search, prefix-targeted for entity lookups
5. Remove direct `executeGraphQL` and `gatewayURL` from GraphExecutor
