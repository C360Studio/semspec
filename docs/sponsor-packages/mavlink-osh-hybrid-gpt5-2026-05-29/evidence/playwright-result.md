# Playwright Result — Run #2 hybrid-gpt5

```
#32 7.589 ✓ 3706 modules transformed.
#32 23.65 ✓ 3719 modules transformed.
#32 24.33 ✓ built in 15.94s
#32 24.85 ✓ built in 23.33s
# at cleanup. Without this every failed-run forensics loses the
  ✓  1 [t2] › e2e/plan-lifecycle-llm-mavlink-osh.spec.ts:172:2 › @t2 @mavlink-hard plan-lifecycle-llm-mavlink-osh › plan created with goal (9ms)
[mavlink-osh:review] Stage: (start) -> ready_for_approval (0s)
[mavlink-osh:review] Stage: ready_for_approval -> reviewed (9s)
[mavlink-osh:review] Promoting at reviewed gate
[mavlink-osh:review] Stage: reviewed -> generating_requirements (12s)
[mavlink-osh:review] Reached generating_requirements in 12.1s
  ✓  2 [t2] › e2e/plan-lifecycle-llm-mavlink-osh.spec.ts:178:2 › @t2 @mavlink-hard plan-lifecycle-llm-mavlink-osh › plan reviewed and approved (12.1s)
[mavlink-osh:requirements] Stage: (start) -> generating_requirements (0s)
[mavlink-osh:requirements] Stage: generating_requirements -> generating_architecture (15s)
[mavlink-osh:requirements] Reached generating_architecture in 15.0s
[mavlink-osh] 2 requirements generated
  ✓  3 [t2] › e2e/plan-lifecycle-llm-mavlink-osh.spec.ts:193:2 › @t2 @mavlink-hard plan-lifecycle-llm-mavlink-osh › requirements generated (15.0s)
[mavlink-osh:architecture] Stage: (start) -> generating_architecture (0s)
[mavlink-osh:architecture] Stage: generating_architecture -> generating_scenarios (108s)
[mavlink-osh:architecture] Reached generating_scenarios in 108.1s
[mavlink-osh] Architecture: 2 actors, 1 integrations, harness_profiles=[mavlink.px4-sitl.mavsdk-smoke, mavlink.raw-mavlink-direct]
  ✓  4 [t2] › e2e/plan-lifecycle-llm-mavlink-osh.spec.ts:209:2 › @t2 @mavlink-hard plan-lifecycle-llm-mavlink-osh › architecture generated with harness profile selection (1.8m)
[mavlink-osh:scenarios] Stage: (start) -> generating_scenarios (0s)
[mavlink-osh:scenarios] Stage: generating_scenarios -> reviewing_scenarios (9s)
[mavlink-osh:scenarios] Stage: reviewing_scenarios -> generating_requirements (90s)
[mavlink-osh:scenarios] Stage: generating_requirements -> generating_architecture (123s)
[mavlink-osh:scenarios] Stage: generating_architecture -> generating_scenarios (246s)
[mavlink-osh:scenarios] Stage: generating_scenarios -> reviewing_scenarios (258s)
[mavlink-osh:scenarios] Stage: reviewing_scenarios -> scenarios_reviewed (270s)
[mavlink-osh:scenarios] Reached scenarios_reviewed in 270.3s
[mavlink-osh] 10 scenarios generated
  ✓  5 [t2] › e2e/plan-lifecycle-llm-mavlink-osh.spec.ts:239:2 › @t2 @mavlink-hard plan-lifecycle-llm-mavlink-osh › scenarios generated and reviewed (4.5m)
[mavlink-osh:promote-exec] Stage: (start) -> ready_for_execution (0s)
[mavlink-osh:promote-exec] Reached ready_for_execution in 0.0s
[mavlink-osh:exec-start] Stage: (start) -> implementing (0s)
[mavlink-osh:exec-start] Reached implementing in 0.0s
  ✓  6 [t2] › e2e/plan-lifecycle-llm-mavlink-osh.spec.ts:253:2 › @t2 @mavlink-hard plan-lifecycle-llm-mavlink-osh › execution triggered (1.2s)
[mavlink-osh:execution] Stage: (start) -> implementing (0s)
[mavlink-osh:execution] Stage: implementing -> rejected (6388s)
[mavlink-osh:execution] Recovery cycle 1/2: PlanDecision ery.94f4e13a status=accepted
```
