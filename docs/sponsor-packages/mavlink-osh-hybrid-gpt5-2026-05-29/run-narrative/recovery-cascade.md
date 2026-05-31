# Autonomous QA Recovery Cascade — Narrated

This document narrates the exact moment the autonomous QA-recovery
chain fired on this run. Compare to the 2026-05-29 morning gemini-only
validation (see `project_autonomous_qa_recovery_chain_verified_2026_05_29.md`
in memory) — the same wire fires here with the dev/reviewer pair on
gpt-5.5 instead.

## Timeline of the cascade

```
02:09:21 INFO Loop completion received via KV
              workflow_step=review outcome=success tdd_cycle=2
              ← reviewer approved one of req 2's nodes

02:09:21 INFO Code review verdict
              task_id=node-e2eb7b9e88f6d0ae-a614303e verdict=approved

02:09:21 INFO Worktree merged successfully
              ← node's code accepted and merged

02:09:43 INFO Loop completion received via KV
              workflow_step=requirement outcome=success
              ← REQUIREMENT 1 actually completed (this is the "success"
                that the 1h outer cap later overrode)

02:09:44 INFO Plan decision added
              kind=execution_exhausted
              affected=[requirement.7cb2e6e5ae4b.2]
              decision_id=...exhaust.t.7cb2e6e5ae4b.2.1780106983950585553
              ← PR #34's budget gate FIRES — requirement-executor
                detected req 2 had exhausted its TDD cycle budget on
                one of its nodes

02:09:51 INFO Plan decision added
              kind=requirement_change
              affected=[requirement.7cb2e6e5ae4b.2]
              decision_id=...recovery.94f4e13a
              proposed_by=recovery-agent
              ← recovery-agent (gemini-pro) read the trajectory of
                the exhausted node and proposed a requirement_change
                7 seconds later

02:09:51 INFO Plan decision accepted via mutation
              proposal_id=...recovery.94f4e13a
              accepted_by=auto:recovery
              ← PR #33's auto_accept_recovery setting accepted the
                proposal automatically (NO HUMAN IN THE LOOP)

02:09:51 INFO Published cascade request (auto-accept)
              subject=workflow.trigger.plan-decision-cascade
              ← plan-decision-handler picks up the trigger

02:09:51 INFO handling change proposal cascade
              proposal_id=...recovery.94f4e13a

02:09:51 INFO Auto-accepted recovery PlanDecision

02:09:51 INFO cascade complete
              affected_requirements=1
              affected_scenarios=3

02:09:51 INFO Resuming exec on accepted PlanDecision
              component=requirement-executor
              requirement_id=requirement.7cb2e6e5ae4b.2

02:09:51 WARN Failed to delete old requirement branch on recovery resume
              ← benign: branch already collected by an earlier sweep

02:09:51 INFO Resuming requirement from awaiting-recovery — re-decomposing
              recovery_restart=1
              max_recovery_restarts=1
              ← the requirement-executor now re-runs decomposition
                on the failed requirement, producing fresh task nodes

02:10:05 INFO Task execution created via mutation
02:10:05 INFO Worktree created
02:10:05 INFO Dispatched developer
              ← first new node from the re-decomposed req 2 fires
```

## Why this matters

The mavlink-decode tier (2026-05-29 morning) validated this cascade
with gemini-pro on every role. THIS pack proves the same cascade fires
with gpt-5.5 dev/reviewer. The wire is model-agnostic.

After the cascade triggered, fresh nodes from the re-decomposed
requirement 2 went through full dev → validator → reviewer cycles and
got approved:

```
02:30:57 verdict=approved task=node-ff4556cff760fe67 tdd_cycle=0  ← first try!
02:35:14 verdict=approved task=node-4ee1d9c3b6d6a81b tdd_cycle=1
```

So recovery cycle 1 was actively converging when the 1-hour outer cap
hit at 02:42:29. This is the budget mismatch identified in
`operator-rules/config-tuning-applied.md` — the recovery system was
healthy, the outer wall was just too low.

## Why one PlanDecision stays as `proposed`

```
{ "id": "983950585553",
  "proposed_by": "requirement-executor",
  "kind": "execution_exhausted",
  "status": "proposed" }      ← intentionally human-gated (PR #34)

{ "id": "ery.94f4e13a",
  "proposed_by": "recovery-agent",
  "kind": "requirement_change",
  "status": "accepted" }       ← auto-accepted (PR #33)
```

The first decision (kind=execution_exhausted) is the BUDGET GATE from
PR #34. It's intentionally NOT auto-accepted — the system surfaces
"requirement N has exhausted its budget" for human review. The system
fires it as a signal, doesn't act on it. The recovery-agent reads
that signal AND PROPOSES a separate `requirement_change` decision —
THAT is auto-accepted. Clean separation: budget gate = surfacing
information; recovery proposal = autonomous action.
