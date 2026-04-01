# ADR-030: BMAD Alignment — Personas, Architecture Phase, and Prompt Vocabulary

**Status:** Accepted
**Date:** 2026-04-01
**Authors:** Coby, Claude
**Depends on:** ~~ADR-027 (Always-On Agent Teams)~~ — dependency removed; roles are the persistent identity
now (lessons learned system)

## Context

An early adopter wants semspec to align more closely with BMAD (Breakthrough Method for Agile
AI-Driven Development, v6.2.2). BMAD is an open-source framework that structures AI-driven
development using named agent personas (Mary the Analyst, Winston the Architect, Bob the Scrum
Master) and a four-phase workflow (Analysis → Planning → Solutioning → Implementation).

Our architecture is superior in several ways — persistent knowledge graph, role-scoped lessons
learned, SOP enforcement, reactive execution, developer-led TDD execution. But the feedback reveals
three real gaps:

1. **Vocabulary/naming** — BMAD uses memorable persona names and familiar artifacts (PRD,
   Architecture Document). Our role names (`requirement-generator`, `scenario-generator`) are
   functional but alien to BMAD users.

2. **Architecture planning** — BMAD's "Solutioning" phase produces Architecture Documents with
   technology choices, deployment topology, and IaC decisions *before* implementation. Semspec
   jumped from requirements to scenarios with no architecture step.

3. **Prompt tuning requires `go build`** — All prompt content lived in compiled Go
   (`prompt/domain/software.go` — 1700 lines). Changing an agent's identity or behavioral framing
   required rebuilding the binary. This blocked adopters from customizing personas without forking.

### How personas attach (post ADR-027 removal)

Roles are the persistent identity — not agents, not teams:

- Personas are optional display labels configured per-role in `semspec.json`
- Role-scoped lessons give our "personas" memory — something BMAD's static markdown files can't do
- Five roles: planner, plan-reviewer, developer, reviewer, architect
- BMAD persona names are cosmetic (config), not separate role identities

### Relationship to semstreams vocabulary

Semstreams vocabulary (`vocabulary.Register()`, `PredicateMetadata`) is a **semantic graph predicate
registry** for knowledge triples. The prompt vocabulary is a **display/rendering concern** for
prompt assembly. Different systems, no overlap:

| Semstreams vocabulary | Prompt vocabulary |
|---|---|
| Graph predicates: `semspec.plan.status` | Display labels: `Agent: "Analyst"` |
| Triple creation, NATS wildcards, RDF export | Prompt fragment rendering, UI, logs |
| Rarely changes (new predicates = new `init()`) | Adopter-tunable via JSON config |

The persona's `DisplayName` is written to the graph as an `agent.identity.display_name` triple
during startup — single source (role config), two consumers (prompt assembly + graph).

---

## Decision

### 1. Add AgentPersona to agent entities

Adopt semdragon's `AgentPersona` pattern. Each agent gets optional persona configuration that is
injected into prompt assembly without code changes.

```go
// workflow/agent.go
type AgentPersona struct {
    DisplayName  string   `json:"display_name"`  // "Mary", "Winston" — UI + logs
    SystemPrompt string   `json:"system_prompt"` // injected at CategoryPersona
    Backstory    string   `json:"backstory"`     // optional character narrative
    Traits       []string `json:"traits"`        // personality attributes
    Style        string   `json:"style"`         // communication style
}
```

Configured per-role in `configs/semspec.json`:

```json
{
  "execution-manager": {
    "personas": {
      "planner": {
        "display_name": "Mary",
        "system_prompt": "You are a strategic business analyst...",
        "traits": ["methodical", "evidence-driven", "thorough"],
        "style": "structured"
      },
      "developer": {
        "display_name": "Amelia",
        "system_prompt": "You are a senior implementation engineer...",
        "traits": ["pragmatic", "test-driven"],
        "style": "concise"
      }
    }
  }
}
```

When no persona is configured, the existing domain fragments provide the identity (no behavioral
change for current users).

**Prompt injection:** `CategoryPersona` (priority 450, between `CategoryDomainContext` and
`CategoryToolGuidance`) in `prompt/fragment.go`. The assembler injects persona content when
`AssemblyContext.Persona` is populated. This overlays on top of domain fragments — persona refines
identity, domain defines process.

**Graph integration:** On startup, write `agent.identity.display_name` triple from
`Persona.DisplayName`. The persona is the config source; the graph triple is a read-optimized
projection.

### 2. Add prompt Vocabulary struct

A first-class vocabulary data structure for display labels, following semdragon's `domain.Vocabulary`
pattern. This replaces hardcoded strings in fragment `Content` fields.

```go
// prompt/vocabulary.go
type Vocabulary struct {
    Agent     string          `json:"agent"`      // "Developer", "Analyst"
    Task      string          `json:"task"`       // "Task", "Sprint", "Story"
    Plan      string          `json:"plan"`       // "Plan", "Project Brief", "PRD"
    Review    string          `json:"review"`     // "Code Review", "Quality Gate"
    Team      string          `json:"team"`       // "Team", "Party", "Squad"
    RoleNames map[Role]string `json:"role_names"` // planner→"Strategic Analyst"
}
```

Default vocabulary loaded from `configs/vocabulary.json`. Processors pass it into
`AssemblyContext.Vocabulary`. Fragments reference `ctx.Vocabulary.Agent` instead of hardcoding
"developer".

**Migration path:** Update fragments incrementally — each fragment that currently says "You are a
developer..." changes to use `ctx.Vocabulary.Agent`. Gradual migration without breaking existing
behavior (default vocabulary matches current hardcoded values).

### 3. Architecture phase — always-on, planner-gated

An architecture generation step sits between requirements and scenarios in the pipeline. The phase
is always present in the state machine; the planner decides whether to skip it based on complexity.

```
requirements_generated → generating_architecture → architecture_generated → generating_scenarios
```

**State machine additions:**

- `StatusGeneratingArchitecture` — architecture-generator has claimed `requirements_generated`
- `StatusArchitectureGenerated` — architecture phase complete (or skipped)

**Planner-gated skip:** The planner sets `skip_architecture: true` in its output for trivial changes
(single-file edits, documentation updates). The architecture-generator checks this flag and passes
through immediately with zero LLM calls when set. When in doubt, the planner defaults to
`skip_architecture: false`.

**ArchitectureDocument struct** (stored as `plan.Architecture` alongside `plan.Goal`,
`plan.Context`, `plan.Scope`):

```go
type ArchitectureDocument struct {
    TechnologyChoices   []TechChoice   `json:"technology_choices"`
    ComponentBoundaries []ComponentDef `json:"component_boundaries"`
    Decisions           []ArchDecision `json:"decisions"`
}
```

Key decisions are also written as graph triples via KV twofer for cross-plan queryability.
`workflow-documents` writes `.semspec/plans/{slug}/architecture.md` on `architecture_generated`.

**Scenario-generator** watches `architecture_generated` (not `requirements_generated`).

### 4. Targeted review findings with phase routing

`PlanReviewFinding` carries `Phase` and `TargetID` fields, enabling surgical re-entry when
Round 2 review fails:

```go
type PlanReviewFinding struct {
    // ...
    Phase    string `json:"phase,omitempty"`     // "plan", "requirements", "architecture", "scenarios"
    TargetID string `json:"target_id,omitempty"` // e.g., "REQ-2", "SCEN-3"
}
```

The `determineR2ReentryPoint` handler in plan-manager routes to the minimal re-entry point based on
the earliest failing phase:

| Failing phase | Re-entry status | What is cleared |
|---|---|---|
| `plan` | `StatusCreated` | Requirements, scenarios, architecture |
| `requirements` | `StatusApproved` | Requirements, scenarios, architecture |
| `architecture` | `StatusRequirementsGenerated` | Architecture, scenarios |
| `scenarios` | `StatusArchitectureGenerated` | Scenarios only |

Without phase markers, the handler falls back to `StatusApproved` (clear everything).

### 5. Per-step model differentiation (9 capabilities)

Nine distinct capabilities, each independently configurable in the model registry:

| Capability | Usage |
|---|---|
| `planning` | Plan drafting, planner role |
| `requirement_generation` | Requirements from plans |
| `scenario_generation` | BDD scenarios from requirements |
| `plan_review` | Strategic plan assessment (completeness + SOP) |
| `architecture` | Technology choices, component boundaries, deployment topology |
| `coding` | Code generation, TDD execution |
| `reviewing` | Code review, quality analysis |
| `qa` | Integration/e2e test execution (ADR-031, deferred) |
| `fast` | Quick responses, simple tasks |

`plan_review` is intentionally separate from `reviewing` — plan assessment requires different
reasoning than code review, and adopters may want different models for each.

### 6. Developer role replaces tester + builder

The 4-stage execution pipeline (tester → builder → validator → reviewer) has been collapsed to 3
stages: **developer → validator → reviewer**. The developer role handles TDD internally: it writes
tests first, then implements against them. Tester and builder as separate roles have been deleted.

### 7. QA role — constants defined, implementation deferred

`RoleQA` (in `prompt/fragment.go`) and `CapabilityQA` (in `model/capability.go`) are defined.
Full implementation is deferred to ADR-031.

### 8. Ship a BMAD preset

`configs/presets/bmad.json` maps BMAD personas to semspec roles. Config only, no code.

**Updated BMAD role mapping:**

| BMAD Persona | Semspec Role | Pipeline Step | Capability |
|---|---|---|---|
| Mary (Analyst) | planner | Plan drafting | planning |
| John (PM) | requirement-generator | Requirements | requirement_generation |
| Winston (Architect) | architect | Architecture | architecture |
| Bob (Scrum Master) | scenario-generator | Scenarios | scenario_generation |
| (plan reviewer) | plan-reviewer | Plan review | plan_review |
| Amelia (Engineer) | developer | TDD execution | coding |
| (code reviewer) | reviewer | Code review | reviewing |
| Murat (Test Architect) | qa (ADR-031) | QA testing | qa |

---

## What We Are NOT Doing

| BMAD concept | Why we skip it |
|---|---|
| "Parade of markdown" agent definitions | Our roles have persistent lessons + graph-backed memory. Static markdown is a regression |
| `project-context.md` | Knowledge graph + SOPs + ProjectConfig is strictly superior |
| Separate tester/builder roles | Developer handles TDD internally (developer → validator → reviewer) |
| Upfront task decomposition | Reactive execution at runtime is strictly better for complex codebases |
| BMAD persona names as defaults | Users opt in via role persona config; defaults stay professional |
| IaC execution (Terraform, etc.) | Sandbox security for cloud API access needs separate research spike |
| Infrastructure specialist agents (Alex, Morgan, Taylor) | Defer — architecture phase covers the high-value gap; dedicated infra roles are niche |

---

## Impact on Lessons Learned System

ADR-027 (Always-On Agent Teams) has been superseded by the role-scoped lessons learned system.
Agent identity, teams, benching, and Q1/Q2/Q3 scoring have been removed.

Personas now attach to roles via config — no agent entities needed:

| Component | ADR-030 Addition |
|---|---|
| Role config in `semspec.json` | Optional persona display name per role |
| `prompt.Vocabulary` | First-class display labels loaded from config |
| `buildAssemblyContext()` | Populate `AssemblyContext.Persona` from role config |
| HTTP endpoints (`GET /lessons`) | Lesson-based, not agent-based |

---

## Implementation

### Key files

| File | Change |
|---|---|
| `workflow/agent.go` | `AgentPersona` struct |
| `workflow/types.go` | `StatusGeneratingArchitecture`, `StatusArchitectureGenerated`, `ArchitectureDocument`, `TechChoice`, `ComponentDef`, `ArchDecision` |
| `prompt/vocabulary.go` | `Vocabulary` struct + default loader; `RoleQA` constant |
| `prompt/fragment.go` | `CategoryPersona` (priority 450), `RoleQA`, `RoleArchitect` |
| `prompt/context.go` | `Vocabulary` and `Persona` in `AssemblyContext` |
| `prompt/assembler.go` | Persona fragment injection |
| `prompt/domain/software.go` | Migrate hardcoded strings to `ctx.Vocabulary.*` |
| `model/capability.go` | `CapabilityArchitecture`, `CapabilityPlanReview`, `CapabilityQA`, `CapabilityFast` |
| `workflow/prompts/plan_reviewer.go` | `PlanReviewFinding.Phase`, `PlanReviewFinding.TargetID` |
| `processor/plan-manager/mutations.go` | `determineR2ReentryPoint` phase routing |
| `processor/execution-manager/config.go` | `Persona` in role config |
| `processor/architecture-generator/` | New component — watches `PLAN_STATES`, skip-through on `skip_architecture` |
| `output/workflow-documents/component.go` | Handle `architecture_generated`, write `architecture.md` |
| `configs/vocabulary.json` | Default vocabulary (matches current hardcoded values) |
| `configs/presets/bmad.json` | BMAD persona + vocabulary preset |

---

## Verification

### Vocabulary + Personas

1. Configure BMAD personas per role in `semspec.json`
2. Start semspec → verify persona display names in logs
3. Trigger a plan → verify persona prompt in assembled system prompt (trajectory inspection)
4. `GET /execution-manager/lessons` → verify role config in response
5. `task e2e:mock -- hello-world` passes (regression gate)

### Architecture Phase

1. Create a plan with a non-trivial goal → verify pipeline passes through
   `generating_architecture` → `architecture_generated`
2. Verify architect agent dispatched with `RoleArchitect` capability
3. Verify `architecture.md` written to `.semspec/plans/{slug}/`
4. Verify architecture decisions appear as graph triples
5. Verify scenario-generator triggers on `architecture_generated`
6. Create a trivial plan → verify planner sets `skip_architecture: true` → verify pass-through
   (zero architecture LLM calls)

### Targeted Review Routing

1. Force a Round 2 review failure with `phase: "architecture"` finding
2. Verify re-entry at `StatusRequirementsGenerated` (not `StatusApproved`)
3. Verify only architecture + scenarios are cleared; requirements preserved

### BMAD Preset

1. Copy `configs/presets/bmad.json` into active config
2. Restart semspec → verify BMAD persona names in logs and agent responses
3. No code changes required — config only

---

## Open Questions

1. **Architecture review gating** — Should the architecture document go through plan-reviewer (like
   R1/R2), or is generate-and-proceed sufficient for v1?
2. **Vocabulary in UI** — Should `prompt.Vocabulary` labels flow to the frontend (plan stage names,
   agent display names), or backend-only for now?
3. **Sandbox IaC** — Cloud API access from sandbox containers is a security question. Defer to
   dedicated spike, but flag it as a known limitation for BMAD adopters expecting
   Terraform/CloudFormation generation.
