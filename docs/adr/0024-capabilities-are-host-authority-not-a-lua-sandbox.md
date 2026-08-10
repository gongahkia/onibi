# ADR 0024: separate contributions from capabilities and scope them to host services

## Status

Accepted for M8.

## Context

Registration class and editor authority are different questions. Treating a
plugin declaring a command as automatically entitled to read/edit everything
would obscure review and make a future isolated runtime harder to define.

## Decision

Manifests independently declare contributions and capabilities. Contributions
permit registration of commands, selectors, transformations, bindings, and
events; capabilities authorize data-only host services such as document read,
document edit, syntax read, selection writes, messages, command invocation,
and event subscription. The host denies absent authority with structured,
traceable errors. M7 configuration remains a separate full-authority trusted
user overlay.

## Consequences

Least authority is visible in the package manifest and testable at each host
boundary. PUC Lua standard libraries are still available, so this constrains
Zenbu operations only; filesystem/process/network/resource isolation remains
M9 work.
