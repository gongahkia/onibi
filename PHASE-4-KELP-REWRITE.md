# PHASE 4: NanoClaw Deterministic Runtime

## Goal

Build NanoClaw as the KelpClaw runtime that compiles approved workflows into static DAGs and executes each node in its own Docker container.

Execution must be deterministic after approval. No agentic replanning is allowed mid-run.

## Runtime Principles

- Approved workflow revision is the only execution input.
- DAG order is computed deterministically with stable tie-breaking.
- Every node runs in a separate Docker container.
- Containers receive declared inputs and emit declared outputs.
- Side effects are isolated to declared adapters and mounted work directories.
- Logs, artifacts, exit codes, and result envelopes are captured per node.
- A failed node cannot mutate downstream state.

## Execution Lifecycle

1. Load approved workflow revision.
2. Validate schema, approval record, graph acyclicity, and runtime settings.
3. Compile nodes and edges into an execution plan.
4. Resolve skill images, code-gen artifacts, adapter mocks, and secrets.
5. Execute ready nodes in deterministic topological order.
6. Write node input payload to an isolated workspace.
7. Run the node container with resource limits.
8. Capture stdout, stderr, logs, artifacts, exit code, and output payload.
9. Validate outputs against node output schemas.
10. Continue to downstream nodes or mark the run failed.
11. Emit final `execution_result`.

## Docker Requirements

Each node container must define:

- Docker image or build context.
- Read-only workflow spec mount.
- Writable isolated node workspace.
- Explicit environment allowlist.
- CPU, memory, timeout, and network policy.
- Structured input path and structured output path.
- Exit code convention.

Network access is disabled by default and enabled only for nodes that declare adapter or API access.

## Failure And Retry Policy

- Retries are configured per node.
- Retry count, backoff, and timeout are part of the approved spec.
- Non-deterministic retries must be marked in runtime metadata.
- Failed schema validation is never retried.
- Downstream nodes do not run after an upstream hard failure.
- Partial run state is preserved for inspection and replay.

## Implementation Checkpoints

1. Implement execution plan compiler from approved workflow JSON.
2. Implement deterministic topological sort with stable node id tie-breaking.
3. Implement Docker runner abstraction with real Docker execution.
4. Implement per-node workspace creation and cleanup policy.
5. Implement structured input/output files and schema validation.
6. Implement retry, timeout, cancellation, and failure propagation.
7. Implement run event stream for OpenClaw.
8. Implement replay of a completed run from stored inputs and artifacts.

## Tests

- Unit tests for topological sorting and failure propagation.
- Integration tests that run two or more Dockerized nodes locally.
- Isolation tests proving node workspaces do not share undeclared files.
- Timeout and cancellation tests.
- Output schema validation tests.
- Replay tests proving the same approved spec and inputs produce the same execution path.

## Acceptance Criteria

- NanoClaw runs each node in a separate Docker container.
- Execution order is deterministic and independent of object insertion order.
- Invalid or unapproved workflows cannot run.
- Logs and artifacts are inspectable per node.
- Failure behavior is predictable and covered by tests.
