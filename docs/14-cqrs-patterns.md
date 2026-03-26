# CQRS Patterns in Semspec

Semspec implements CQRS (Command Query Responsibility Segregation) through three mechanisms
that work together: the **payload registry**, **single-writer managers**, and the **KV Twofer**.
None of these use the word "CQRS" in the code — they emerged from practical constraints
(crash recovery, concurrent agents, event-driven rules) and happen to satisfy CQRS properties.

This document explains the pattern for developers extending semspec or building on semstreams.

## The Three Mechanisms

### 1. Payload Registry — Typed Command Schema

Every message over NATS uses the payload registry. This is the typed command bus.

```go
// Registration at init time — panics on mismatch
func init() {
    component.RegisterPayload(&component.PayloadRegistration{
        Domain:   "workflow",
        Category: "requirements-generated",
        Version:  "v1",
        Factory:  func() any { return &RequirementsGeneratedEvent{} },
    })
}

// Publish — schema validated at source
baseMsg := message.NewBaseMessage(payload.Schema(), payload, "requirement-generator")
data, _ := json.Marshal(baseMsg)
js.Publish(ctx, subject, data)

// Subscribe — consumer gets concrete type, not raw JSON
var base message.BaseMessage
json.Unmarshal(msg.Data, &base)
event := base.Payload.(*RequirementsGeneratedEvent)  // guaranteed typed
```

What this enforces:

- Every command has a declared type (`domain.category.version` envelope)
- Serialization validates at the source — invalid commands can't be published
- Deserialization is guaranteed typed — consumers get the concrete struct
- Schema mismatches panic at startup, not at runtime
- Going over the bus without registry participation degrades to `GenericPayload` — the
  system tells you the contract is broken

This is stricter than most CQRS command bus implementations. Typical command buses enforce
the interface (implement `ICommand`) but leave payload shape to convention. The registry
enforces shape, validates on marshal, and catches mismatches at init time.

### 2. Single-Writer Managers — Exclusive Write Ownership

Each entity type has exactly one component that writes to its KV bucket. Other components
publish commands (typed events); the owning manager persists them.

```
requirement-generator ──► RequirementsGeneratedEvent ──► plan-manager (sole writer)
scenario-generator   ──► ScenariosForRequirementGenerated ──► plan-manager (sole writer)
```

The manager pattern has three layers:

| Layer | Purpose | Survives Restart? |
|-------|---------|-------------------|
| `sscache.Cache[T]` (TTL) | O(1) hot reads during active execution; from semstreams `pkg/cache` | No (TTL-bounded) |
| Domain KV bucket | Durable write-through — the write IS the event (KV Twofer) | Yes |
| `graphutil.TripleWriter` → ENTITY_STATES | Cross-component facts for rules and graph queries | Yes |

The cache is `sscache.NewTTL[T]` from semstreams — a generic TTL cache, not `sync.Map`. Each
manager writes to its **own KV bucket** for domain-specific operational state, and additionally
writes key facts to `ENTITY_STATES` for cross-component visibility (rules, graph queries,
dashboard views).

**Why single-writer matters:**

- No write conflicts — one component owns the bucket, uses CAS (compare-and-swap)
- Clear error attribution — if plan state is wrong, check plan-manager
- Generators are stateless — they compute and publish, never persist
- Rules can safely react to writes without racing the writer

Current single-writer assignments:

| Entity Type | Writer | Domain KV Bucket | Also writes to ENTITY_STATES? |
|-------------|--------|------------------|-------------------------------|
| Plans, Requirements, Scenarios | `plan-manager` | `PLAN_STATES` | Yes — status, slug, key predicates for rules |
| Task + Requirement executions | `execution-manager` | `EXECUTION_STATES` | Yes — phase, verdict, metrics for rules |
| Project config | `project-manager` | (filesystem currently) | Planned |
| Agent loop state | `agentic-loop` (semstreams) | `AGENT_LOOPS` | No |

**Two write destinations, one writer:** A manager writes the full entity JSON to its domain
bucket (operational truth, fast reconcile) and writes selected predicates to ENTITY_STATES
(cross-component truth, rule triggers). Both writes happen in the same code path — not a
dual-write problem because the domain KV is the primary and ENTITY_STATES is best-effort
enrichment.

### 3. KV Twofer — Write = Event, Zero Extra Infrastructure

Every NATS KV bucket is backed by a JetStream stream. A single KV write gives three
interfaces simultaneously:

```
KV.Put(key, value)
        │
        ├── State:  kv.Get(key) → current value
        ├── Event:  kv.Watch(pattern) → fires on every change (fan-out)
        └── History: replay from any revision for audit trail
```

This is the "twofer" — the durable write IS the event. No separate event publishing step,
no dual-write consistency problem, no event store to maintain.

**How the rule processor uses this:**

1. `execution-manager` writes a phase triple: `workflow.phase = "completed"`
2. ENTITY_STATES KV fires a watch event
3. Rule processor evaluates rules in `configs/rules/semspec-task-execution/`
4. Matching rule sets terminal status: `workflow.status = "approved"`
5. That KV write fires another watch event — downstream consumers react

The component owns phase progression. Rules own terminal transitions. Neither needs to
know about the other — the KV watch is the contract.

## How It Maps to CQRS

| CQRS Concern | Semstreams Mechanism |
|---|---|
| Command bus | Named NATS subjects with typed payloads |
| Typed command schema | Payload registry — `domain.category.version` envelope |
| Command validation | `BaseMessage.MarshalJSON` — fails at source, not consumer |
| Command handler | Single-writer manager component, CAS enforcement |
| Write model | Domain KV bucket (PLAN_STATES, EXECUTION_STATES) + ENTITY_STATES triples |
| Read model | TTL cache (hot), KV watches (reactive), ENTITY_STATES queries (cross-component) |
| Event propagation | KV write = event (Twofer) — zero extra infrastructure |
| Event schema | Same payload registry — events are typed identically to commands |

## What This Means in Practice

### Adding a New Entity Type

1. **Define the payload** — use `/new-payload` skill. Register in `init()`.
2. **Pick the owning manager** — one component writes, others publish commands to it.
3. **Choose the KV bucket** — usually ENTITY_STATES unless the entity has different
   lifecycle semantics.
4. **Write rules for terminal transitions** — add JSON rules in `configs/rules/`.

### Adding a New Command

1. **Define the event struct** in `workflow/subjects.go` or the component's `payloads.go`.
2. **Register the payload** in `init()`.
3. **Define a typed subject**: `var MyEvent = natsclient.NewSubject[MyEventType]("workflow.events.my.event")`
4. **Publish from the source** using `MyEvent.Publish(ctx, client, payload)`.
5. **Subscribe in the manager** using `MyEvent.Subscribe(ctx, client, handler)`.

### Common Mistakes

| Mistake | Symptom | Fix |
|---------|---------|-----|
| Two components writing to the same KV key | Intermittent state corruption, CAS failures | Designate a single writer; other components publish events |
| Publishing without payload registration | Consumer gets `*GenericPayload` instead of typed struct | Add `init()` registration with matching Domain/Category/Version |
| Separate event publish after KV write | Dual-write inconsistency — event fires but write failed (or vice versa) | Use KV Twofer — the write IS the event |
| Rules and components both setting the same field | Race condition on terminal status | Rules own terminal transitions; components own phase steps |
| Reading KV directly instead of through the cache | Stale reads during concurrent mutations | Use the manager's `get()` which returns a copy from the TTL cache |

## Related Documentation

| Document | Description |
|----------|-------------|
| [Architecture](03-architecture.md) | Manager pattern details, component registration |
| [Components](04-components.md) | Individual manager component configurations |
| [Execution Pipeline](11-execution-pipeline.md) | NATS subjects, consumers, payload types |
