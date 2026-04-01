# ADR-030: BMAD Alignment — Personas, Architecture Phase, and Prompt Vocabulary

**Status:** Proposed
**Date:** 2026-03-31
**Authors:** Coby, Claude
**Depends on:** ~~ADR-027 (Always-On Agent Teams)~~ — dependency removed; roles are the persistent identity now (lessons learned system)

## Context

An early adopter wants semspec to align more closely with BMAD (Breakthrough Method for Agile AI-Driven Development, v6.2.2). BMAD is an open-source framework that structures AI-driven development using named agent personas (Mary the Analyst, Winston the Architect, Bob the Scrum Master) and a four-phase workflow (Analysis → Planning → Solutioning → Implementation).

Our architecture is superior in several ways — persistent knowledge graph, role-scoped lessons learned, SOP enforcement, reactive execution, TDD pipeline. But the feedback reveals three real gaps:

1. **Vocabulary/naming** — BMAD uses memorable persona names and familiar artifacts (PRD, Architecture Document). Our role names (`requirement-generator`, `scenario-generator`) are functional but alien to BMAD users.

2. **Architecture planning** — BMAD's "Solutioning" phase produces Architecture Documents with technology choices, deployment topology, and IaC decisions *before* implementation. Semspec jumps from requirements to scenarios with no architecture step.

3. **Prompt tuning requires `go build`** — All prompt content lives in compiled Go (`prompt/domain/software.go` — 1700 lines). Changing an agent's identity or behavioral framing requires rebuilding the binary. Semdragon has the same problem — both codebases hardcode prompt text in Go structs. This blocks adopters from customizing personas without forking.

### How personas attach (post ADR-027 removal)

Roles are the persistent identity — not agents, not teams:
- Personas are optional display labels configured per-role in `semspec.json`
- Role-scoped lessons give our "personas" memory — something BMAD's static markdown files can't do
- Five roles: planner, plan-reviewer, developer, reviewer, architect
- BMAD persona names are cosmetic (config), not separate role identities

### Relationship to semstreams vocabulary

Semstreams vocabulary (`vocabulary.Register()`, `PredicateMetadata`) is a **semantic graph predicate registry** for knowledge triples. The proposed prompt vocabulary is a **display/rendering concern** for prompt assembly. Different systems, no overlap:

| Semstreams vocabulary | Prompt vocabulary |
|---|---|
| Graph predicates: `semspec.plan.status` | Display labels: `Agent: "Analyst"` |
| Triple creation, NATS wildcards, RDF export | Prompt fragment rendering, UI, logs |
| Rarely changes (new predicates = new `init()`) | Adopter-tunable via JSON config |

The persona's `DisplayName` is written to the graph as an `agent.identity.display_name` triple during `seedTeams()` — single source (roster config), two consumers (prompt assembly + graph).

---

## Decision

### 1. Add AgentPersona to agent entities

Adopt semdragon's `AgentPersona` pattern. Each agent gets optional persona configuration that is injected into prompt assembly without code changes.

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

Configured via team roster in `configs/semspec.json`:

```json
{
  "teams": {
    "enabled": true,
    "roster": [
      {
        "name": "alpha",
        "members": [
          {
            "role": "planner",
            "model": "claude-sonnet",
            "persona": {
              "display_name": "Mary",
              "system_prompt": "You are a strategic business analyst who explores problem spaces through systematic research and evidence gathering.",
              "traits": ["methodical", "evidence-driven", "thorough"],
              "style": "structured"
            }
          },
          {
            "role": "builder",
            "model": "claude-sonnet",
            "persona": {
              "display_name": "Amelia",
              "system_prompt": "You are a senior implementation engineer who writes clean, tested, production-quality code.",
              "traits": ["pragmatic", "test-driven"],
              "style": "concise"
            }
          }
        ]
      }
    ]
  }
}
```

When no persona is configured, the existing domain fragments provide the identity (no behavioral change for current users).

**Prompt injection:** Add `CategoryPersona` (priority 450, between `CategoryDomainContext` and `CategoryToolGuidance`) to `prompt/fragment.go`. The assembler injects persona content when `AssemblyContext.Persona` is populated. This overlays on top of domain fragments — persona refines identity, domain defines process.

**Graph integration:** During `seedTeams()`, write `agent.identity.display_name` triple from `Persona.DisplayName`. The persona is the config source; the graph triple is a read-optimized projection.

### 2. Add prompt Vocabulary struct

Add a first-class vocabulary data structure for display labels, following semdragon's `domain.Vocabulary` pattern. This replaces hardcoded strings in fragment `Content` fields.

```go
// prompt/vocabulary.go
type Vocabulary struct {
    Agent     string          `json:"agent"`      // "Developer", "Analyst", "Adventurer"
    Task      string          `json:"task"`       // "Task", "Sprint", "Story"
    Plan      string          `json:"plan"`       // "Plan", "Project Brief", "PRD"
    Review    string          `json:"review"`     // "Code Review", "Quality Gate"
    Team      string          `json:"team"`       // "Team", "Party", "Squad"
    RoleNames map[Role]string `json:"role_names"` // planner→"Strategic Analyst"
}
```

Default vocabulary loaded from `configs/vocabulary.json`. Processors pass it into `AssemblyContext.Vocabulary`. Fragments reference `ctx.Vocabulary.Agent` instead of hardcoding "developer".

**Migration path:** Update fragments incrementally — each fragment that currently says "You are a developer..." changes to use `ctx.Vocabulary.Agent`. This can be done gradually without breaking existing behavior (default vocabulary matches current hardcoded values).

### 3. Add optional architecture phase to plan pipeline

Insert an optional architecture generation step between requirements and scenarios:

```
requirements_generated → [architecture] → generating_scenarios
```

When `architecture_review: true` in plan-manager config:

1. New status: `StatusGeneratingArchitecture` (in-progress claim) and `StatusArchitectureGenerated` (terminal for this phase)
2. New component: `processor/architecture-generator/` watches `PLAN_STATES` for `requirements_generated`
3. Dispatches architect agent via agentic-dispatch with `RoleArchitect` and `CapabilityArchitecture`
4. Architect produces structured output: technology choices, component boundaries, deployment topology, data flow, IaC decisions
5. Output stored as `plan.Architecture` field (alongside `plan.Goal`, `plan.Context`, `plan.Scope`) in PLAN_STATES
6. Key decisions written as graph triples via KV twofer for cross-plan queryability
7. workflow-documents writes `.semspec/plans/{slug}/architecture.md` on `architecture_generated` status
8. scenario-generator watches for `architecture_generated` instead of `requirements_generated` (when architecture is enabled)

When `architecture_review: false` (default): existing flow unchanged — scenario-generator continues to watch `requirements_generated`.

**Architecture as plan attribute, not separate entity:** The architecture document is plan-scoped — it doesn't have an independent lifecycle. Storing it as `plan.Architecture` (like `plan.Goal`) keeps the data model simple. Individual decisions (e.g., "use PostgreSQL for persistence") are also written as graph triples so they're queryable across plans.

### 4. Ship a BMAD preset

Once personas and vocabulary are configurable, ship `configs/presets/bmad.json` mapping BMAD personas to semspec roles:

| BMAD Persona | Semspec Role | Notes |
|---|---|---|
| Mary + John + Bob | `planner` | Three BMAD personas collapse into one role (plan, requirements, scenarios) |
| Winston (Architect) | `architect` (new) | Technology choices, deployment topology |
| Amelia + Murat | `developer` | TDD internally (writes tests + implementation) |
| (no BMAD equivalent) | `reviewer` | Code review + lesson extraction |
| (no BMAD equivalent) | `plan-reviewer` | Plan review + lesson extraction |

This is a config file, not code. Adopters copy it and customize.

---

## What We Are NOT Doing

| BMAD concept | Why we skip it |
|---|---|
| "Parade of markdown" agent definitions | Our roles have persistent lessons + graph-backed memory. Static markdown is a regression |
| `project-context.md` | Knowledge graph + SOPs + ProjectConfig is strictly superior |
| Single-agent implementation | TDD pipeline (tester→builder→validator→reviewer) is non-negotiable |
| Upfront task decomposition | Reactive execution at runtime is strictly better for complex codebases |
| BMAD persona names as defaults | Users opt in via roster persona config; defaults stay professional |
| IaC execution (Terraform, etc.) | Sandbox security for cloud API access needs separate research spike |
| Infrastructure specialist agents (Alex, Morgan, Taylor) | Defer — architecture phase covers the high-value gap; dedicated infra roles are niche |

---

## Impact on Lessons Learned System

ADR-027 (Always-On Agent Teams) has been superseded by the role-scoped lessons learned system. Agent identity, teams, benching, and Q1/Q2/Q3 scoring have been removed.

Personas now attach to roles via config — no agent entities needed:

| Component | ADR-030 Addition |
|---|---|
| Role config in `semspec.json` | Optional persona display name per role |
| `prompt.Vocabulary` | First-class display labels loaded from config |
| `buildAssemblyContext()` | Populate `AssemblyContext.Persona` from role config |
| HTTP endpoints (`GET /lessons`) | Lesson-based, not agent-based |

---

## Implementation Sequence

| Phase | Scope | Risk | Dependency |
|---|---|---|---|
| **1. Vocabulary struct** | `prompt.Vocabulary`, config loading, fragment migration | Low | None |
| **2. Persona injection** | `CategoryPersona` fragment, assembler wiring | Low | Phase 1 |
| **3. Architecture phase** | New statuses, architect role, architecture-generator component | Medium | Phase 1 (vocabulary used in architect fragments) |
| **4. BMAD preset** | `configs/presets/bmad.json` — config only, no code | Low | Phases 1-2 |
| **Future** | Static fragment externalization to YAML | Medium | Separate ADR |
| **Future** | IaC sandbox security model | High | Separate spike |

### Key files

| File | Change |
|---|---|
| `workflow/agent.go` | Add `AgentPersona` struct (in ADR-027) |
| `prompt/vocabulary.go` | New file — `Vocabulary` struct + default loader |
| `prompt/fragment.go` | Add `CategoryPersona`, `RoleArchitect` |
| `prompt/context.go` | Add `Vocabulary` and `Persona` to `AssemblyContext` |
| `prompt/assembler.go` | Inject persona fragment when populated |
| `prompt/domain/software.go` | Migrate hardcoded strings to `ctx.Vocabulary.*` |
| `model/capability.go` | Add `CapabilityArchitecture` |
| `workflow/types.go` | Add `StatusGeneratingArchitecture`, `StatusArchitectureGenerated` |
| `processor/execution-manager/config.go` | Add `Persona` to `TeamMemberEntry` (in ADR-027) |
| `processor/architecture-generator/` | New component — follows standard component pattern |
| `output/workflow-documents/component.go` | Handle `architecture_generated` status, write `architecture.md` |
| `configs/vocabulary.json` | Default vocabulary (matches current hardcoded values) |
| `configs/presets/bmad.json` | BMAD persona + vocabulary preset |

---

## Verification

### Phase 1-2 (Vocabulary + Personas)
1. Configure a BMAD persona roster in `semspec.json`
2. Start semspec → verify agent display names in logs
3. Trigger a plan → verify persona prompt appears in assembled system prompt (via trajectory inspection)
4. `GET /execution-manager/agents` → verify display names in response
5. `task e2e:mock -- hello-world` passes (regression gate)

### Phase 3 (Architecture Phase)
1. Set `architecture_review: true` in plan-manager config
2. Create a plan → verify pipeline pauses after `requirements_generated`
3. Verify architect agent dispatched with `RoleArchitect` capability
4. Verify `architecture.md` written to `.semspec/plans/{slug}/`
5. Verify architecture decisions appear as graph triples
6. Verify scenario-generator triggers on `architecture_generated`
7. Set `architecture_review: false` → verify existing flow unchanged

### Phase 4 (BMAD Preset)
1. Copy `configs/presets/bmad.json` into active config
2. Restart semspec → verify BMAD persona names in logs and agent responses
3. No code changes required — config only

---

## Open Questions

1. **Architecture review gating** — Should the architecture document go through plan-reviewer (like R1/R2), or is a simpler generate-and-proceed sufficient for v1?
2. **Vocabulary in UI** — Should `prompt.Vocabulary` labels flow to the frontend (plan stage names, agent display names), or backend-only for now?
3. **Sandbox IaC** — Cloud API access from sandbox containers is a security question. Defer to dedicated spike, but flag it as a known limitation for BMAD adopters expecting Terraform/CloudFormation generation.
