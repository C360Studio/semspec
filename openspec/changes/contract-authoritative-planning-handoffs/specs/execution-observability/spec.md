## ADDED Requirements

### Requirement: UI shows authoritative current phase
The UI SHALL derive banners, phase labels, navigation state, and detail views from authoritative plan and execution
summaries rather than stale feed rows or previous loop labels.

#### Scenario: Planning banner does not show stale scenario generation
- **WHEN** the plan is currently in architecture, Story preparation, execution, recovery, QA, or waiting state
- **THEN** the top banner names that actual state and does not display a stale earlier phase

#### Scenario: Execution has a first-class banner
- **WHEN** the plan enters implementation or Story execution
- **THEN** the UI presents execution as a first-class phase with active work, elapsed time, waits, and current loop detail

#### Scenario: Disconnected data is visible
- **WHEN** SSE, polling, or API data is disconnected or stale
- **THEN** the UI shows the last successful update time and avoids presenting stale state as live truth

### Requirement: UI exposes execution progress and blockers
The UI SHALL show which Stories, tasks, requirements, loops, tools, and QA steps are active, complete, failed,
waiting, or blocked during execution.

#### Scenario: Operator can see what happened during execution
- **WHEN** a plan reaches a terminal, waiting, or rejected state after execution
- **THEN** the UI shows the execution timeline, active and completed Stories, recovery actions, QA evidence, and blocker reason without requiring log inspection

#### Scenario: Orphaned execution rows are identified
- **WHEN** execution rows exist without matching current Stories, requirements, or active loops
- **THEN** the UI labels them as stale or orphaned instead of presenting them as current execution work

#### Scenario: Full-auto wait is explicit
- **WHEN** an autonomous run is waiting on a human-gated action
- **THEN** the UI names the decision, policy reason, affected nodes, and next possible actions

### Requirement: UI exposes recovery and lesson activity
The UI SHALL surface recovery-agent, lesson-decomposer, and lesson-curator activity with enough context to explain
whether the output affects the current run or future runs only.

#### Scenario: Recovery action is visible
- **WHEN** recovery-agent proposes, auto-accepts, rejects, or applies a PlanDecision
- **THEN** the UI shows action kind, diagnosis, affected nodes, contract impact, and resulting phase transition

#### Scenario: Lesson decomposition is marked future-run only
- **WHEN** lesson-decomposer produces lessons after a loop or run
- **THEN** the UI indicates whether those lessons can affect the current run or are only available to future runs

#### Scenario: Lesson cost is visible
- **WHEN** lesson-decomposer or lesson-curator consumes LLM calls
- **THEN** the UI includes those calls in run cost and loop accounting rather than hiding them outside the execution timeline

### Requirement: Cost and rate displays identify evidence source
The UI MUST distinguish measured usage from estimated cost and SHALL identify the rate source used for any cost
calculation.

#### Scenario: Cost display has real usage but unknown rate
- **WHEN** token counts are known but provider pricing is not configured
- **THEN** the UI labels cost as unavailable or estimated instead of showing false precision

#### Scenario: Cost display uses configured provider rates
- **WHEN** provider rate metadata is configured for the models used in a run
- **THEN** the UI calculates cost from measured usage and displays the rate source and timestamp
