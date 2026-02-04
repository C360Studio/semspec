# ADR-001: UI Observability Boundaries

## Status

Proposed

## Context

Semspec operates as a semantic development agent built on semstreams infrastructure. Two distinct UIs exist:

1. **semstreams-ui**: Generic flow builder for platform operators
2. **semspec-ui**: Agent interface for developers using semspec

Users of semspec-ui need visibility into agent behavior (loops, multi-agent coordination, task progress) but don't need flow editing or deep infrastructure metrics. Conversely, platform operators use semstreams-ui and Grafana for flow design and system observability.

The question: What observability belongs in semspec-ui vs. deferring to other tools?

## Decision

### Principle: User-Centric Observability

semspec-ui shows what **agent users** need to understand and control their work. Deep infrastructure observability is delegated to purpose-built tools.

### Boundary Definition

| Concern | semspec-ui | semstreams-ui | Grafana |
|---------|------------|---------------|---------|
| **Loop State** | Primary owner | - | Metrics only |
| **Multi-Agent Coordination** | Primary owner | - | - |
| **Task/Proposal Progress** | Primary owner | - | - |
| **Chat History** | Primary owner | - | - |
| **Agent Decisions/Reasoning** | Primary owner | - | - |
| **Top-Level Health** | Summary indicator | Full detail | Alerting |
| **System Metrics** | Summary/sparklines | Full detail | Dashboards |
| **Component Logs** | - | Primary owner | Aggregation |
| **Flow Creation/Editing** | - | Primary owner | - |
| **Flow Deployment** | - | Primary owner | - |
| **Knowledge Graph (domain)** | Query/explore | Generic viz | - |
| **Historical Analytics** | - | - | Primary owner |

### What semspec-ui SHOULD Show

1. **Loop & Agent State** (Primary concern)
   - Active loops with state, iterations, model
   - Multi-agent relationships (parent/child loops)
   - Loop timeline/history for a session
   - Pause/resume/cancel controls

2. **Task Progress** (Workflow visibility)
   - Current proposal/task state
   - Workflow stage (exploring → designing → implementing)
   - Approval/governance status
   - Context/memory indicators

3. **Agent Reasoning** (Transparency)
   - Tool calls made (what, when, result)
   - Decision points (why agent chose X)
   - Token usage (cost awareness)
   - Model being used

4. **Health Summary** (Glanceable)
   - Overall system status (green/yellow/red)
   - Active component count
   - Link to "Open in semstreams-ui" for details
   - Link to "Open in Grafana" for metrics

5. **Domain Graph** (Optional, if valuable)
   - Code entity relationships
   - Proposal → spec → implementation links
   - Semantic connections discovered

### What semspec-ui Should NOT Own

1. **Flow Editing** - semstreams-ui concern
2. **Component Configuration** - semstreams-ui concern
3. **Detailed Logs** - semstreams-ui + log aggregation
4. **Prometheus Metrics Deep Dive** - Grafana
5. **Historical Analytics** - Grafana dashboards
6. **Alerting Configuration** - Grafana/Alertmanager

### Integration Points

```
┌─────────────────────────────────────────────────────────────┐
│                     User Journeys                            │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  Developer using semspec:                                    │
│  ┌──────────┐                                               │
│  │semspec-ui│ ──chat──> loops, tasks, agent state           │
│  └────┬─────┘                                               │
│       │ "View system details"                               │
│       ▼                                                     │
│  ┌──────────────┐                                           │
│  │semstreams-ui │ ──monitor──> logs, health, components     │
│  └──────────────┘                                           │
│                                                              │
│  Platform operator:                                          │
│  ┌──────────────┐                                           │
│  │semstreams-ui │ ──build──> flows, deploy, validate        │
│  └──────┬───────┘                                           │
│         │ "View metrics dashboard"                          │
│         ▼                                                   │
│  ┌─────────┐                                                │
│  │ Grafana │ ──analyze──> metrics, trends, alerts           │
│  └─────────┘                                                │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

## Consequences

### Positive

- **Focused UX**: Each UI serves its audience without feature bloat
- **Clear ownership**: No ambiguity about where to add features
- **Composable**: Tools integrate via links, not duplication
- **Simpler semspec-ui**: Can focus on agent-specific value

### Negative

- **Multiple tools**: Users may need to context-switch
- **Link maintenance**: Deep links between tools need coordination
- **Partial picture**: semspec-ui alone won't show everything

### Neutral

- **Grafana dependency**: Optional but recommended for production
- **semstreams-ui optional**: Not required for basic semspec usage

## Implementation Notes

### semspec-ui Enhancements (if approved)

1. **Dashboard page**: Loop summary, active tasks, health sparkline
2. **Tasks page**: Workflow state machine visualization
3. **History page**: Past sessions, loop outcomes, token usage
4. **Settings page**: Model preferences, link to external tools

### Deep Links

- Health indicator → `semstreams-ui/flows/{flow-id}?tab=health`
- Metrics summary → `grafana/d/semspec-overview`
- Loop details → internal `/loops/{loop-id}` (semspec-ui owns)

## Related

- semstreams ADR-003: Component Lifecycle Status
- semstreams ADR-016: Agentic Governance Layer
- semspec roadmap: Dashboard and monitoring features
