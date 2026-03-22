# Behavioral Controls for Autonomous Agents

Autonomous agents fail in predictable ways. They describe work instead of doing it. They explore
indefinitely without producing output. They submit clarifying questions as deliverables. They inflate
review scores so underperforming peers get trusted with harder work. Left unchecked, these patterns
compound — each uncorrected failure trains the next agent to fail in the same way.

Semspec addresses this through a layered behavioral control system built into the `prompt/` package.
Controls are injected as prompt fragments, ordered by urgency, and composed per-role and per-provider
at runtime. This document explains each technique, why it works, and where to find the implementation.

## Overview

Behavioral controls are prompt fragments that constrain, redirect, or calibrate agent behavior at
specific points in the reasoning cycle. They differ from task instructions (what to do) and context
(what is known) — behavioral controls address *how the agent reasons and acts*.

The primary failure modes they target:

| Failure Mode | Symptom | Control Used |
|---|---|---|
| Description instead of execution | Agent writes code in its response body but never calls `bash` to write the file | Tool directive + anti-description gate |
| Infinite exploration | Agent keeps reading files but never produces output | Tool budget injection |
| Questions as deliverables | Agent submits "How should I handle X?" as its result | Question detection (`LooksLikeQuestion`) |
| Score inflation | Reviewer rates everything 4-5 so agents game the trust system | Rating calibration |
| Ignoring peer feedback | Agent repeats same errors across tasks | Error trend injection |
| Starting from scratch on retry | Agent discards previous work and begins a new implementation | Retry continuity directive |

Behavioral controls are implemented in two files:

- `prompt/fragment.go` — Category constants that control injection ordering
- `prompt/domain/software.go` — Fragment definitions for the software engineering domain

## Identity and Consequence Framing

The first thing an agent reads sets the stakes. Fragment `software.developer.system-base`
(`CategorySystemBase`, position 0) establishes identity and optimization target before any other
instruction reaches the model:

```
You are a developer implementing code changes for a software project.

Your Objective: Complete the assigned task according to acceptance criteria. You optimize for COMPLETION.
```

The phrase "You optimize for COMPLETION" is deliberate. It establishes a single success criterion
before the model processes any other instruction. Later fragments that say "you MUST use bash to
write files" reinforce the same criterion rather than introducing a new one.

The code reviewer's base identity uses adversarial framing for the opposite effect:

```
You are a code reviewer checking implementation quality for production readiness.

Your Objective: Determine: "Would I trust this code in production?"
You optimize for TRUSTWORTHINESS, not completion. Your job is adversarial to the developer.
```

"Your job is adversarial to the developer" directly counters the cooperative bias that causes
reviewers to approve work they would personally reject. The framing is intentional: reviewers and
developers have opposing optimization targets, and stating this explicitly reduces score inflation.

Fragment location: `prompt/domain/software.go`, `software.developer.system-base` and
`software.reviewer.system-base`.

## Behavioral Gates

`CategoryBehavioralGate` (value 275) sits between provider hints (200) and role context (300).
Fragments in this category run before the agent reads task details, which means behavioral rules
are established before the model encounters anything that might override them.

The category is defined in `prompt/fragment.go`:

```go
// CategoryBehavioralGate contains mandatory behavioral rules injected early
// (exploration gates, anti-description directives, structural checklist, tool budget).
CategoryBehavioralGate Category = 275
```

### Mandatory Workspace Exploration Gate

Small and mid-size models skip file reading and generate code from memory. The results look
plausible but contain wrong import paths, misnamed types, and fabricated function signatures.
An exploration gate makes workspace inspection non-optional:

```
BEFORE WRITING ANY CODE:
1. Use bash to list the relevant directories and understand project structure
2. Use bash to read the files you intend to modify
3. Only after reading — begin implementation

Skipping this step and writing code from memory is a TASK FAILURE.
```

The phrase "TASK FAILURE" is load-bearing. Agents trained on RLHF are strongly averse to explicit
failure labels. Framing skipped exploration as a failure condition — not a preference — reliably
changes the behavior of models that would otherwise skip it.

### Anti-Description Directive

The most common agent failure mode is producing a code block in the response body without calling
`bash` to actually write the file. Fragment `software.developer.tool-directive` addresses this at
position 100 (before behavioral gates) and again in the behavioral gate:

```
CRITICAL: You MUST Use Tools to Make Changes

You MUST actually call bash to create or modify files. Do NOT just describe what you would do
— you must EXECUTE the changes using tool calls.

- To create or modify a file: Call bash with the appropriate shell command
- NEVER output code blocks as your response without also executing them via bash

If you complete a task without using bash to write files, the task has FAILED.
```

The explicit failure condition and the uppercase "MUST" are both intentional emphasis patterns.
Many models respond to ALLCAPS and "CRITICAL" prefix as urgency markers. The negative statement
("Do NOT just describe") and the positive restatement ("you must EXECUTE") cover both direction
types because some models respond better to one form than the other.

Fragment location: `prompt/domain/software.go`, `software.developer.tool-directive`.

## Tool-Use Budget

Agents without a sense of iteration cost explore indefinitely. Injecting the current iteration
count and maximum creates a scarcity frame that triggers planning behavior:

```
Tool-Use Budget

You have used N of M tool-call rounds. Plan your remaining work to finish within the budget.
- Rounds remaining: R
- Prioritize: bash calls that write files (required for completion)
- Avoid: redundant reads of files you have already examined
```

This is generated dynamically from `TaskContext` fields. The budget injection converts an
open-ended loop into a constrained planning problem. Agents that would otherwise read the same
file three times instead complete a single pass and move to implementation.

`TaskContext` carries the iteration data from `prompt/context.go`:

```go
// TaskContext carries data for developer task prompts.
type TaskContext struct {
    Task          workflow.Task
    Context       *workflow.ContextPayload
    PlanTitle     string
    PlanGoal      string
    Feedback      string
    Iteration     int          // current attempt number (1-based)
    MaxIterations int          // total tool-use budget
    ErrorTrends   []ErrorTrend
    AgentID       string
}
```

The assembler reads this at runtime through `ContentFunc` closures, so iteration data is injected
fresh on each assembly call without requiring fragment re-registration.

## Structural Checklist

The structural checklist appears in both developer and reviewer prompts. Dual injection creates
a closed loop: the developer sees the rules before starting, and the reviewer enforces the same
rules when evaluating the result.

Developer-side injection (fragment `software.developer.behavioral-gates`, `CategoryBehavioralGate`):

```
STRUCTURAL CHECKLIST — You will be auto-rejected if ANY item fails:
- All code changes must include corresponding tests. No untested code.
- No hardcoded API keys, passwords, or secrets in source code.
- All errors must be handled or explicitly propagated. No silently swallowed errors.
- No debug prints, TODO hacks, or commented-out code left in the submission.
```

Reviewer-side enforcement (fragment `software.reviewer.structural-checklist`, same category):

```
STRUCTURAL CHECKLIST — Any failure is an automatic rejection:
- All code changes must include corresponding tests. No untested code.
- No hardcoded API keys, passwords, or secrets in source code.
- All errors must be handled or explicitly propagated. No silently swallowed errors.
- No debug prints, TODO hacks, or commented-out code left in the submission.

Check each item. If ANY item fails, the verdict MUST be "rejected" with rejection_type "fixable".
```

The identical checklist in both prompts creates a closed loop. The developer knows exactly what
the reviewer will check, and the reviewer enforces the same rules the developer was told to follow.
Items that appear in both prompts cannot be missed by accident.

Fragment location: `prompt/domain/software.go`. Developer checklist in
`software.developer.behavioral-gates`. Reviewer checklist in `software.reviewer.structural-checklist`.

## Review Scoring and Rating Calibration

Reviewers default to optimism. Without explicit calibration, agent reviewers assign high scores to
work that merely functions, conflating "it runs" with "it is production-ready." Two techniques
address this.

### Pre-task Criteria Injection

The reviewer's role context includes the scoring criteria before the reviewer reads any code. This
anchors judgment before the model forms an initial impression:

```
Review Criteria

Your verdict must reflect honest quality assessment:
- approved  — Code is production-ready, all SOPs satisfied, tests complete
- rejected  — Any SOP violated, missing tests, or quality below production threshold

You cannot approve if any SOP has status "violated".
```

The unconditional constraint ("You cannot approve if any SOP has status 'violated'") removes
reviewer discretion for the most common approval mistake. Discretion is preserved for edge cases
but removed where rules are clear.

### Rating Calibration

Fragment `software.reviewer.rating-calibration` (`CategoryRoleContext`, Priority 1) injects
an explicit rating scale anchored at "meets expectations":

```
RATING CALIBRATION:
Rate honestly. These ratings determine the agent's future assignments.
If you inflate scores, underperforming agents get trusted with harder work — and when
they fail, it costs everyone.

  1 = Unacceptable — fundamentally wrong, missing, or unusable
  2 = Below expectations — significant gaps, errors, or missing requirements
  3 = Meets expectations — correct, complete, does what was asked (baseline for competent work)
  4 = Exceeds expectations — well-structured, thorough, handles edge cases
  5 = Exceptional — production-quality, elegant, rare

Most good work is a 3 or 4, not a 5. A 3 for solid work is correct — not a 5.
```

The game-theoretic statement ("If you inflate scores, underperforming agents get trusted with
harder work") appeals to the reviewer's role as a system component, not just an evaluator of a
single task. This framing is more effective than abstract requests for honesty because it ties
inflated scores to a concrete negative consequence.

The explicit "3 = baseline for competent work" combats the LLM tendency to rate everything 5/5.

Fragment location: `prompt/domain/software.go`, `software.reviewer.rating-calibration`.

## Peer Feedback as Mandatory Directives

Error trends from previous reviews are injected at `CategoryPeerFeedback` (position 350) — after
role context but before domain knowledge. This placement means a developer reads their historical
failure patterns immediately before reading the current task description, maximizing relevance.

Fragment `software.developer.error-trends` is conditionally injected when `ErrorTrends` is
non-empty:

```go
{
    ID:       "software.developer.error-trends",
    Category: prompt.CategoryPeerFeedback,
    Roles:    []prompt.Role{prompt.RoleDeveloper},
    Condition: func(ctx *prompt.AssemblyContext) bool {
        return ctx.TaskContext != nil && len(ctx.TaskContext.ErrorTrends) > 0
    },
    ContentFunc: func(ctx *prompt.AssemblyContext) string {
        var sb strings.Builder
        sb.WriteString("RECURRING ISSUES — Your recent reviews flagged these patterns. " +
            "You MUST address ALL of the following:\n\n")
        for _, trend := range ctx.TaskContext.ErrorTrends {
            fmt.Fprintf(&sb, "- %s (%d occurrences): %s\n",
                trend.Label, trend.Count, trend.Guidance)
        }
        return sb.String()
    },
},
```

The occurrence count is load-bearing. An agent seeing "Missing Tests (3 occurrences)" treats the
issue as a persistent pattern — not a one-time observation. The word "RECURRING" in the header
reinforces this. The guidance field from `ErrorTrend` provides concrete remediation, not just
a label:

```go
// ErrorTrend carries a resolved error category with its occurrence count.
type ErrorTrend struct {
    CategoryID string // e.g. "missing_tests"
    Label      string // e.g. "Missing Tests"
    Guidance   string // actionable remediation from the category def
    Count      int
}
```

Fragment location: `prompt/domain/software.go`. Context struct: `prompt/context.go`.

## Failure Recovery and Anti-Pattern Injection

When a reviewer rejects an implementation, the retry prompt must accomplish three things:

1. Carry forward all previous work (the agent should not start from scratch)
2. Direct the agent to address every specific issue
3. Block the agent from repeating patterns that caused the rejection

Fragment `software.developer.retry-directive` is conditionally injected when `Feedback` is
non-empty:

```go
{
    ID:       "software.developer.retry-directive",
    Category: prompt.CategoryToolDirective,
    Priority: 1,
    Roles:    []prompt.Role{prompt.RoleDeveloper},
    Condition: func(ctx *prompt.AssemblyContext) bool {
        return ctx.TaskContext != nil && ctx.TaskContext.Feedback != ""
    },
    ContentFunc: func(ctx *prompt.AssemblyContext) string {
        return fmt.Sprintf(`CRITICAL: You MUST Use Tools to Fix Issues
...
DO NOT repeat these mistakes. Build on your previous work — do not start from scratch.

Previous Feedback:
The reviewer rejected your implementation with this feedback:

%s

Address ALL issues mentioned in the feedback. Do not ignore any points.
...
- Fix EVERY issue mentioned in feedback
- Use bash to check current state, then bash to apply fixes
- Do not introduce new issues
- Maintain existing functionality
- Update tests if needed`, ctx.TaskContext.Feedback)
    },
},
```

Key phrases:

- "Build on your previous work — do not start from scratch" — Prevents the agent from discarding
  previous work and beginning a new implementation
- "Address ALL issues" — Counters selective compliance (agents fix easy items, skip hard ones)
- "Do not introduce new issues" — Prevents fix attempts that break unrelated functionality
- "Maintain existing functionality" — Explicit continuity instruction reduces wholesale rewrites

The retry directive uses `Priority: 1` within `CategoryToolDirective`, placing it immediately
after the primary tool directive. This ensures the rejection feedback is processed before any
role context shifts the agent's frame.

Fragment location: `prompt/domain/software.go`, `software.developer.retry-directive`.

## Question Detection

Some agents, particularly smaller models or models encountering ambiguous requirements, submit a
question as their deliverable. Without detection, the question enters the review cycle as a failed
implementation rather than being redirected to the knowledge gap system.

The `LooksLikeQuestion` function (`prompt/validation.go`) detects this pattern before
the response is processed:

- Code fences present → not a question (the agent produced real work)
- Short response with question phrases (`"how should"`, `"what is"`, `"can you"`) → likely a question
- More than 50% of lines end with `?` → likely a question

When a question is detected, the error message redirects the agent to the correct mechanism:

```
Your response appears to be a question rather than an implementation.

If you need clarification before proceeding, use a <gap> block to signal the knowledge gap.
The workflow will route your question to the appropriate answerer and resume when resolved.

Do not submit questions as deliverables.
```

This redirect is specific: it names the correct tool (`<gap>` block) and explains the routing
consequence. Vague redirects ("please implement") produce another question. Specific redirects
("use this mechanism") produce a gap block that the question-answerer component can process.

See [Question Routing](06-question-routing.md) for the full gap detection and SLA escalation
lifecycle.

## Provider-Specific Workarounds

Different LLM providers require different prompt engineering. The `prompt` package handles this
through provider-gated fragments and provider-aware formatting.

### Provider Constants

```go
const (
    ProviderAnthropic Provider = "anthropic"
    ProviderOpenAI    Provider = "openai"
    ProviderOllama    Provider = "ollama"
)
```

### Known Provider Behaviors

| Provider | Known Issue | Workaround |
|---|---|---|
| Gemini (OpenAI-compat) | Ignores instructions placed mid-prompt | Place JSON format and tool directives at END of prompt (`CategoryOutputFormat = 600`) |
| OpenAI | Needs explicit "call tool first" instruction before reasoning | `CategoryToolDirective = 100` appears before role context |
| Small models (Ollama) | Skip exploration and write from memory | Mandatory exploration gate in `CategoryBehavioralGate = 275` |
| Anthropic | Responds well to XML-wrapped sections | `ProviderStyle.PreferXML = true` wraps each category in `<tag>...</tag>` |

### Provider-Gated Fragments

A fragment can target a specific provider using the `Providers` field:

```go
{
    ID:        "openai.tool-call-first",
    Category:  prompt.CategoryProviderHints,
    Providers: []prompt.Provider{prompt.ProviderOpenAI},
    Content:   "Always call a tool before generating any text response.",
},
```

When `Providers` is empty, the fragment applies to all providers. Provider gating lets you inject
provider-specific workarounds without polluting the shared fragment space.

### Format Conventions

The assembler wraps each category group using the provider's preferred format:

```go
func FormatSection(label, content string, style ProviderStyle) string {
    if style.PreferXML {
        tag := strings.ReplaceAll(strings.ToLower(label), " ", "_")
        return fmt.Sprintf("<%s>\n%s\n</%s>", tag, content, tag)
    }
    if style.PreferMarkdown {
        return fmt.Sprintf("## %s\n%s", label, content)
    }
    return fmt.Sprintf("%s:\n%s", label, content)
}
```

Anthropic output:

```xml
<behavioral_gates>
BEFORE WRITING ANY CODE: ...
</behavioral_gates>
```

OpenAI/Ollama output:

```markdown
## Behavioral Gates
BEFORE WRITING ANY CODE: ...
```

Fragment location: `prompt/fragment.go` (provider types and styles), `prompt/assembler.go`
(format application).

## Fragment Assembly Order

All behavioral control techniques compose through a single ordered pipeline. Categories are sorted
numerically; fragments within a category are sorted by `Priority` (lower = first).

```
Category 0   (SystemBase)      — Identity and stakes framing
Category 100 (ToolDirective)   — "You MUST use bash to write files" — anti-description
Category 200 (ProviderHints)   — Provider-specific format instructions
Category 275 (BehavioralGate)  — Exploration gate, checklist, tool budget
Category 300 (RoleContext)     — Role-specific process and criteria
Category 350 (PeerFeedback)    — Error trends from previous reviews
Category 400 (DomainContext)   — Task details, plan goal, acceptance criteria
Category 500 (ToolGuidance)    — When to use which tool (advisory)
Category 600 (OutputFormat)    — JSON structure requirements
Category 700 (GapDetection)    — <gap> block instructions
```

The placement of `BehavioralGate` (275) after `ProviderHints` (200) is deliberate. Provider
formatting hints must reach the model before behavioral rules so the model knows how to interpret
section delimiters. Behavioral gates must arrive before role context (300) so rules are established
before the model reads task-specific information that might override them.

The placement of `PeerFeedback` (350) after `RoleContext` (300) is also deliberate. The role
context establishes what the agent is trying to do; peer feedback then adds historical failure
patterns as a filter on that goal.

Files:

- `prompt/fragment.go` — Category constants and `Fragment` type
- `prompt/assembler.go` — Sort, group, and format pipeline
- `prompt/domain/software.go` — Software domain fragment definitions
- `prompt/domain/research.go` — Research domain fragment definitions (alternate domain example)

## Extending Behavioral Controls

### Adding a New Fragment

1. Open `prompt/domain/software.go`
2. Add a new `Fragment` struct to the `Software()` return slice
3. Choose the appropriate `Category` based on when the rule should be applied
4. Use `Condition` for context-dependent fragments (e.g., only on retry)
5. Use `ContentFunc` for fragments with dynamic content from `AssemblyContext`

Example — adding a scope reminder that fires only when the task has explicit file scope:

```go
{
    ID:       "software.developer.scope-reminder",
    Category: prompt.CategoryBehavioralGate,
    Priority: 10, // After primary behavioral gates (Priority 0)
    Roles:    []prompt.Role{prompt.RoleDeveloper},
    Condition: func(ctx *prompt.AssemblyContext) bool {
        return ctx.TaskContext != nil && len(ctx.TaskContext.Task.Files) > 0
    },
    ContentFunc: func(ctx *prompt.AssemblyContext) string {
        return fmt.Sprintf(
            "Scope Constraint: Only modify files within the task scope.\n"+
                "In-scope files: %s\n"+
                "Modifying files outside this list is a task failure.",
            strings.Join(ctx.TaskContext.Task.Files, ", "),
        )
    },
},
```

### Adding a New Category

Add the constant to `prompt/fragment.go` with a value that places it correctly in the sequence:

```go
const (
    // CategoryCustomGate sits between BehavioralGate and RoleContext.
    CategoryCustomGate Category = 280
)
```

Add the label to `categoryLabel` in `prompt/assembler.go`:

```go
case CategoryCustomGate:
    return "Custom Gate"
```

### Adding Provider-Specific Fragments

Set the `Providers` field to target a single provider:

```go
{
    ID:        "software.gemini.json-reminder",
    Category:  prompt.CategoryOutputFormat,
    Priority:  99, // Last in output format — Gemini reads end-of-prompt instructions
    Roles:     []prompt.Role{prompt.RoleDeveloper},
    Providers: []prompt.Provider{prompt.ProviderOpenAI}, // OpenAI-compat (includes Gemini)
    Content:   "IMPORTANT: Your response MUST be valid JSON. No text before or after.",
},
```

### Adding a New Domain

Create a new file in `prompt/domain/` following the pattern in `software.go` and `research.go`.
Return a `[]*prompt.Fragment` slice from a named constructor. Register the fragments in the
processor that builds the `prompt.Registry`:

```go
registry := prompt.NewRegistry()
registry.RegisterAll(domain.Software()...)
registry.RegisterAll(domain.YourDomain()...)
registry.Register(prompt.ToolGuidanceFragment(prompt.DefaultToolGuidance()))
```

The `software.go` file is the canonical reference implementation. All fragment IDs, category
choices, and conditional patterns demonstrated there apply directly to new domains.

## Related Documentation

| Document | Description |
|---|---|
| [How It Works](01-how-it-works.md) | End-to-end overview including agentic loop execution |
| [Question Routing](06-question-routing.md) | Gap detection, SLA, and escalation when agents need clarification |
| [SOP System](09-sop-system.md) | SOP authoring and enforcement — the rule source that reviewers check |
| [Model Configuration](07-model-configuration.md) | Capability-to-model mapping and provider configuration |
