# ADR 0017: structured provenance and bounded observation

## Context

Zenbu needs to explain edits without coupling its observer to any editing model
or backend, and must not let diagnostics alter semantic replay.

## Decision

Store optional deterministic provenance chains in transaction metadata. Own
bounded trace and profiler services explicitly in each runtime/session. Trace
events are typed values with deterministic insertion/eviction order. CPU-time
profile samples are observational and never become transaction or replay data.

## Consequences

History changes can name their semantic source without reproducing model logic.
Tracing and profiling stay local and bounded; failed inputs can be diagnosed.
The runtime has small observational mutable services, but no global logger or
network telemetry.
