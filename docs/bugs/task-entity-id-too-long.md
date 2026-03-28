# Bug: Task completion entity ID exceeds 6-part format

**Severity**: Medium — graph-ingest rejects the entity, task not queryable in graph
**Component**: `execution-manager` (`processor/execution-manager/component.go`)
**Found during**: UI E2E T2 @easy with Gemini (2026-03-28, run 3)
**Status**: OPEN

## Summary

When execution-manager publishes a task completion entity to graph-ingest, the
constructed entity ID contains nested requirement IDs and UUIDs that violate the
6-part dotted notation format. graph-ingest rejects it with `validateEntityID`.

## Error

```
level=ERROR msg="Failed to create entity" component=graph-ingest
entity_id=semspec.local.exec.task.run.bc6572319e9f-node-semspec.local.exec.req.run.bc6572319e9f-requirement-bc6572319e9f-2-set_health_json_content_type-7ca2c639-2ea6-4933-9ff3-81ff087e4e8f
error="Component.validateEntityID: invalid entity ID format: expected 6 ASCII alphanumeric parts (org.platform.domain.system.type.instance), got 11 parts or non-ASCII characters failed: invalid data format"
```

## Root Cause

The instance segment contains dots from the nested entity ID hierarchy:

```
semspec.local.exec.task.run.<instance>
```

Where `<instance>` is the full task ID:
```
bc6572319e9f-node-semspec.local.exec.req.run.bc6572319e9f-requirement-bc6572319e9f-2-set_health_json_content_type-7ca2c639-2ea6-4933-9ff3-81ff087e4e8f
```

The dots in `semspec.local.exec.req.run` within the instance part create extra
segments, pushing the total from 6 to 11 parts.

## Expected Behavior

The instance segment should be a flat identifier without dots. Options:
1. Hash the task ID to a fixed-length string (e.g., SHA-256 prefix)
2. Replace dots with hyphens in the instance segment before constructing the entity ID
3. Use only the UUID portion of the task ID as the instance

## Impact

- Task completion entities are not stored in the graph
- No graph-queryable record of which tasks completed/failed
- Does not block execution — the error is logged and execution continues

## Files

- `processor/execution-manager/component.go` — `publishEntity` call after TDD pipeline completion
- Graph-ingest `validateEntityID` — enforces 6-part format
