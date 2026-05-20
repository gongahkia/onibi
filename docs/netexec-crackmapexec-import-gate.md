# NetExec and CrackMapExec Import Gate

Date: 2026-05-20

Status: parked behind redacted real output fixtures.

NetExec and CrackMapExec can produce useful red-team evidence about protocol
reachability, authentication outcomes, shares, sessions, and enumerated domain
context. Piranesi may import those artifacts later, but it must not run either
tool, validate credentials, or claim support from synthetic logs.

## Accepted Evidence

An implementation issue may start only when the intake record includes:

- real JSON output, structured log output, or terminal transcript from an
  authorized lab or engagement;
- exact tool name, version, module, protocol, and command flags used to produce
  the output;
- target-scope and authorization notes;
- sanitization notes for hosts, domains, usernames, hashes, passwords, tickets, tokens, command output, and share names;
- fixture digests and secret-scan results;
- expected warnings for unsupported modules or unstructured lines.

## Mapping Expectations

The first adapter should preserve raw evidence and normalize conservative
observations only:

- host and protocol reachability as evidence context;
- authentication success or failure only when the sanitized output explicitly
  records it;
- discovered shares, sessions, groups, or policy facts as evidence when they are
  directly present;
- findings only where the source output is enough to support a reportable risk;
- parser warnings for credential-like material that should be redacted.

## Safety Boundary

The adapter must remain import-only. It must never run NetExec or CrackMapExec,
try passwords, validate hashes, open network connections, execute modules,
download files, or replay commands.

## Out of Scope

- Credential validation or spraying.
- Live SMB, LDAP, WinRM, SSH, MSSQL, RDP, or FTP interaction.
- Module execution, command execution, file retrieval, or shell access.
- Treating a transcript fixture as support for every module or protocol.
