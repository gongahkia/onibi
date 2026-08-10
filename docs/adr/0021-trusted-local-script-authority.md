# ADR 0021: treat M7 configuration as trusted local code while constraining editor authority

## Status

Accepted for M7.

## Context

Lua standard libraries are useful for a user's own configuration and local
modules, but they make language-level sandbox claims misleading. Zenbu still
must preserve its architectural thesis: no model or extension gets a privileged
direct document/history mutation API.

## Decision

M7 loads only a user-selected/default local file and its local Lua modules;
Zenbu does not discover project scripts or fetch remote code. Lua standard
libraries stay available, so users must treat configuration as trusted. The
Zenbu API supplies copied document/selection/syntax summaries and accepts only
declarative actions, selection results, and edit proposals. The normal runtime
performs conversion, validation, transaction commit, history/provenance, and
syntax refresh. Host save, quit, and reload keys are outside script binding
resolution.

## Consequences

M7 is useful configuration, not an isolation boundary. It does not mitigate
malicious local Lua modules, resource exhaustion, filesystem/process/network
access, or Lua package attacks. A future stable plugin contract must add
explicit capabilities and a threat model before considering isolated execution.
