# Codex Adapter

The Codex adapter is intentionally Bash-only for blocking approvals. It installs command hooks that run `onibi _hook codex`, reads hook JSON from stdin, blocks Bash `PreToolUse` on `/v1/approval/request`, then writes a Codex-compatible permission decision to stdout.

Current limitation: `apply_patch` and non-Bash tools are not intercepted here.

Lifecycle hooks also persist Codex `session_id` metadata so stale sessions can prefer `codex resume <session_id>` when a provider session ID has been captured.
