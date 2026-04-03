# Pipeline Stalls at requirements_generated — Missing architecture-generator in UI E2E Config

## Status: FIXED (config gap, not code bug)

## Severity: Critical (blocks entire t1 pipeline)

## Root Cause

The BMAD refactor added an `architecture-generator` component between requirements and
scenarios. The pipeline flow is now:

```
requirements_generated → architecture-generator → architecture_generated → scenario-generator
```

The backend E2E config (`e2e-mock.json`) was updated with the new component, but the UI
E2E config (`e2e-mock-ui.json`) was not. The architecture-generator is missing from both
the components section and the model_registry capabilities.

## Fix Applied

Added to `configs/e2e-mock-ui.json`:
1. `architecture` capability in model_registry (uses mock-planner, 0 LLM calls)
2. `architecture-generator` component definition

## Found During

UI E2E regression testing (2026-04-02) after BMAD alignment backend refactor.
