# UI: Three view errors during E2E

**Found during**: UI E2E monitoring (2026-03-29)
**Status**: OPEN

## 1. Zero trajectories inline

The plan detail page fetches:
```
GET /agentic-loop/trajectories?workflow_slug=${planSlug}&limit=50
```

But execution loops have `workflow_slug: "semspec-task-execution"` or
`"semspec-requirement-execution"`, not the plan slug. The filter returns nothing.

**Fix options:**
- Query by `metadata_key=plan_slug&metadata_value=${slug}` instead of workflow_slug
- Or the backend should set plan slug as workflow_slug on dispatched loops

**File:** `ui/src/routes/plans/[slug]/+page.ts:25`

## 2. Graph view errors

Caddy was routing `/graphql` and `/graph-gateway/*` to `:8082` but graph-gateway
runs with `standalone_server=false` on `:8080`.

**Status:** FIXED in `0ae3dc8` — routes now go to `:8080`.

## 3. File/workspace view errors

Plan-manager workspace endpoints return 503 "sandbox not configured" because
`plan-manager` config has no `sandbox_url`.

**Fix:** Add `"sandbox_url": "http://sandbox:8090"` to plan-manager config in
`configs/e2e-gemini.json` (and other e2e configs).

**File:** `configs/e2e-gemini.json` plan-manager section
