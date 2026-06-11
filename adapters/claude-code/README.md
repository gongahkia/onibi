# Claude Code hook templates

Source-of-truth templates installed by `onibi install-hooks --agent claude`
into `~/.claude/settings.json` as a guarded block.

Events used:

- `Stop` — turn complete; emits `agent_done` event.
- `PreToolUse` — blocking approval; emits `approval_request` event and waits
  for the daemon's decision (`updatedInput` for edits, exit 2 for deny).

Hook script body invokes `onibi-notify` which talks to the daemon over the
local Unix socket. If the daemon is down the hook exits 0 silently — never
blocks the user's session.

Hashes recorded in the SQLite `hooks` table on install; `onibi doctor`
verifies hook integrity.
