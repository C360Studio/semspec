# Recovery trace — a model hallucination, caught and corrected autonomously

This is the most important single piece of evidence in the pack. It shows the
substrate doing what the sponsor brief promised: turning a model error into a
specific, recoverable signal instead of a silent wedge or a dead run.

## What went wrong

The developer agent (gemini) created its implementation under the package
`org.sensorhub.impl.driver.mavsdk` — it **hallucinated `impl.driver`** by
inferring it from the component *name* `mavsdk-driver`, while ignoring the
explicit paths the Story declared (`org.sensorhub.impl.sensor.mavsdk`).

Files it wrote out-of-territory:

```
src/main/java/org/sensorhub/impl/driver/mavsdk/MavSdkDriver.java
src/main/java/org/sensorhub/impl/driver/mavsdk/MavSdkServerHandler.java
src/main/java/org/sensorhub/impl/driver/mavsdk/UnmannedConfig.java
src/main/java/org/sensorhub/impl/driver/mavsdk/UnmannedLocationOutput.java
src/test/java/org/sensorhub/impl/driver/mavsdk/MavSdkDriverIntegrationTest.java
src/test/java/org/sensorhub/impl/driver/mavsdk/MavSdkDriverTest.java
```

## How the substrate caught and fixed it (no human involved)

1. **Deterministic gate (ADR-049 Move-3).** The structural validator compares
   created files against the Story's declared `files_owned` territory
   (`…impl.sensor.mavsdk/…`). The hallucinated `…impl.driver.mavsdk/…` files are
   outside every owned prefix → `Ownership/planning gap at dev gate — fast-failing
   to recovery (ADR-049)`. This is a structural check, not an LLM judgment.

2. **Fast-fail to recovery, not a blind retry.** `requirement-executor` logged
   `Deferred terminal-fail; awaiting recovery PlanDecision` — it did **not**
   burn the dev-retry budget or escalate to a human.

3. **Recovery agent diagnosis (verbatim from `bmad/qa-summary.md`):**
   > The developer agent created implementation files under
   > `src/main/java/org/sensorhub/impl/driver/mavsdk/` instead of the declared
   > `src/main/java/org/sensorhub/impl/sensor/mavsdk/`. The agent hallucinated the
   > `impl.driver` package, likely inferring it incorrectly from the component
   > name `mavsdk-driver` while ignoring the explicit paths in the story's file
   > scope. Since the architecture and story correctly specify the `impl/sensor`
   > paths, refining the prompt to explicitly enforce the correct package and
   > directory structure will fix the wedge.

4. **Action chosen: `refine_prompt`** (kind `requirement_change`,
   auto-accepted). Not `escalate_human`, not `architecture_revise` — the
   architecture and Story were correct; only the dev deviated.

5. **PR #188 story reset.** The owned Story walked the valid state machine
   `executing → failed → pending → ready → executing` and re-dispatched the dev
   loop with the corrected package guidance. **No `nodes_completed=0`
   false-complete, no deadlock** — the exact failure mode that aborted an
   earlier run, now fixed.

6. **Outcome:** the re-dispatched developer wrote to the correct
   `…impl.sensor.mavsdk/…` paths, the build converged, and the node completed.
   The plan went on to assemble cleanly and pass QA.

## Trajectories (in the captured graph state)

- Wedged dev trajectory: `3352c8ff-a7b5-4e73-bd1a-b2709692a6bb`
- Recovery agent trajectory: `1ddbe5be-9de1-4cd9-9c13-1e93440a3941`
- Plan decision: `plan-decision.dd236a6cb88b.recovery.e11f50d3` (status `accepted`)

This single episode exercises ADR-049 (Move-3 territory gate), ADR-037 (wedge
recovery with trajectory access), and PR #188 (owned-story reset on dev-gate
recovery) — end to end, on a real error, in one autonomous pass.
