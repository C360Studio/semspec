---
name: debug-trace
description: Systematically debug a semspec E2E trace using observability tools — checks service health, queries message-logger, and inspects component state
disable-model-invocation: true
---

# Debug Trace Workflow

Systematically debug semspec issues using the built-in observability tools. Do NOT grep through Docker logs or guess — use message-logger and NATS monitoring.

## Arguments

- Optional: A trace ID to investigate. If not provided, start from step 1 to find one.

## Step 1: Check Service Health

Verify infrastructure is running before investigating:

```bash
# Check E2E stack status
task e2e:status

# NATS health
curl -sf http://localhost:8222/healthz && echo "NATS OK" || echo "NATS DOWN"

# Check most recent message (verifies message-logger is working)
curl -s http://localhost:8080/message-logger/entries?limit=1 | jq '.[0].subject // "no messages"'
```

If services are down, run `task e2e:up` and wait for health.

## Step 2: Get Recent Messages & Find Trace ID

If no trace ID was provided:

```bash
# Get last 10 messages to see what's happening
curl -s "http://localhost:8080/message-logger/entries?limit=10" | jq '.[] | {subject, trace_id, timestamp: .timestamp}'
```

Look for the failing command's trace ID.

## Step 3: Query the Full Trace

With a trace ID, get all messages in that request flow:

```bash
curl -s "http://localhost:8080/message-logger/trace/<TRACE_ID>" | jq '.[] | {subject, timestamp, type: .raw_data.type}'
```

Look for:
- Missing expected messages (e.g., tool.result.* after tool.execute.*)
- Error payloads in message content
- Unexpected message ordering

## Step 4: Inspect Component State

Based on what the trace reveals:

```bash
# For workflow issues — check workflow KV state
curl -s http://localhost:8080/message-logger/kv/WORKFLOWS | jq .

# For agent loop issues — check loop KV state
curl -s http://localhost:8080/message-logger/kv/AGENT_LOOPS | jq .

# For consumer issues — check JetStream consumers
curl -s "http://localhost:8222/jsz?consumers=true" | jq '.account_details[].stream_detail[] | {name: .name, consumers: [.consumer_detail[]? | {name: .name, num_pending: .num_pending, num_ack_pending: .num_ack_pending}]}'
```

## Step 5: Check Subject-Specific Messages

Filter message-logger by subject patterns using wildcards:

```bash
# Tool execution messages
curl -s "http://localhost:8080/message-logger/entries?limit=20&subject=tool.execute.*" | jq '.[] | {subject, trace_id}'

# Workflow messages
curl -s "http://localhost:8080/message-logger/entries?limit=20&subject=workflow.>" | jq '.[] | {subject, trace_id}'

# Context build messages
curl -s "http://localhost:8080/message-logger/entries?limit=20&subject=context.build.*" | jq '.[] | {subject, trace_id}'

# Agent task messages
curl -s "http://localhost:8080/message-logger/entries?limit=20&subject=agent.task.*" | jq '.[] | {subject, trace_id}'
```

## Common Issue Patterns

### Command returns but nothing happens
1. Check message-logger for the request entry
2. Look for consumer running: `curl :8222/jsz?consumers=true`
3. Check if message was published to correct stream

### "workflow not found" errors
1. Check slug spelling in `.semspec/changes/`
2. Verify workflow state in KV: `curl .../kv/WORKFLOWS`

### Agent loop stuck
1. Get loop ID from message-logger
2. Check loop KV state
3. Look for timeout/error messages in trace

### Messages out of order
1. Check if Core NATS Publish was used instead of JetStream (common bug)
2. Verify stream exists for the subject

## Important Notes

- Message-logger returns entries **newest first** (descending timestamp)
- Subject wildcards: `*` matches one token, `>` matches multiple tokens
- JetStream subjects are durable; Core NATS subjects are ephemeral
- Buffer may fill on high-volume subjects — increase `buffer_size` if needed
