# Bug: Code reviewer receives builder prompt instead of review prompt

**Severity**: High — causes misscoped rejections on valid code
**Component**: `execution-manager` (`processor/execution-manager/component.go`)
**Found during**: UI E2E T2 @easy with Gemini (2026-03-29, run 13)
**Status**: OPEN

## Summary

The code reviewer receives the builder's task prompt ("Create the HTTP handler")
instead of a review-focused prompt. The reviewer interprets this as a building
task and rejects with `misscoped: "My role is to review code, not create code."`

## Evidence

From run 13 review feedback:
```
rejection_type: misscoped
feedback: "My role is to review code against a specification and SOPs, not to
create new code. The prompt asks me to 'Create the HTTP handler', which is a
building task. Please provide an implementation to review."
```

The reviewer is correct — it's being asked to create, not review. The prompt
passed to the reviewer is the decomposer's node prompt, not a review prompt.

## Root Cause

In `dispatchReviewerLocked`, the reviewer gets `exec.Prompt` which is the original
node implementation prompt. It should get a review-specific prompt like:

"Review the following implementation against the acceptance criteria. The code
has passed structural validation (go build, go vet, go test). Evaluate code
quality, correctness, and adherence to the requirement."

The backend fix e599e4c addressed this for requirement-level reviewers but the
TDD-level code reviewer (dispatched by execution-manager) may not have been
updated.

## Files

- `processor/execution-manager/component.go` — `dispatchReviewerLocked()` (check prompt)
