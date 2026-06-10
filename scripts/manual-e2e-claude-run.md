# Manual end-to-end test: Claude session → Telegram (Phase 2 exit criterion)

This requires Phase 1 already verified (`scripts/manual-e2e-pair.md`).
Allow ~5 minutes.

## Prereqs

1. Phase 1 setup complete (owner paired).
2. Claude Code CLI installed and authenticated (`claude` in PATH).
3. `make install` puts both binaries on PATH:

   ```sh
   make install   # installs to ~/.local/bin/onibi{,-notify}
   export PATH="$HOME/.local/bin:$PATH"
   which onibi onibi-notify
   ```

## Install the Claude hook

```sh
onibi install-hooks --agent claude
# expected:
#   Installed Claude Stop hook into /Users/<you>/.claude/settings.json
#   Hook calls: /Users/<you>/.local/bin/onibi-notify --type agent_done

# verify hook landed
cat ~/.claude/settings.json | jq '.hooks.Stop'
# should show an entry with "onibi-managed": true and your notify path
```

## Run an agent session

```sh
onibi run claude
```

This:

- Loads bot token from Keychain, validates `getMe`.
- Loads owner chat id from SQLite.
- Spawns Claude under a PTY with `ONIBI_SOCK` + `ONIBI_SESSION_ID` env vars
  set, raw-mode stdin pass-through, SIGWINCH resize.
- Listens on the intake socket for hook events.
- Runs the idle-detector as a fallback turn-complete signal.

In the spawned Claude session, type a short prompt like `"reverse the word 'hello'"`. When Claude finishes its turn, two things happen:

1. **Stop hook fires** (primary path): `onibi-notify --type agent_done` runs
   inside the Claude process; the daemon's intake handler receives the event
   and sends a Telegram message to your owner chat.
2. If for some reason the hook doesn't fire, **idle detector fires** (fallback
   path) after 3 seconds of PTY silence; same Telegram message goes out.

The dedup map prevents both from firing for the same active period.

## Verify

In Telegram you should see something like:

```
[claude] turn complete (agent_done)
```text
...
(last ~40 lines of Claude's output, ANSI-stripped, in a fenced code block)
```

Exit Claude (Ctrl-D or `/exit`). The `onibi run` process should exit cleanly
when the child does (no need to Ctrl-C the wrapper).

## Verify the socket cleaned up

```sh
ls -l ~/Library/Application\ Support/onibi/onibi.sock 2>&1
# expected: No such file or directory
```

The intake server removes its socket on graceful shutdown.

## Verify the hook hash registry

```sh
onibi install-hooks --agent claude   # re-run is idempotent — no duplicate entries
sqlite3 ~/Library/Application\ Support/onibi/onibi.sqlite \
  "SELECT agent, sha256 FROM hooks;"
# expected: one row, agent=claude, sha256=<64-char hex>
```

## Tamper detection (proves T9 mitigation works)

```sh
# manually edit ~/.claude/settings.json — change the command in the
# onibi-managed Stop entry to something else (e.g. add "; echo tampered")

# then in code:
#   internal/adapters/claude.VerifyHash(ctx, db) should return an error.
#
# `onibi doctor` will surface this in Phase 10. For now, the SQL store
# already has the recorded hash and the package-level test
# TestVerifyHashDetectsTamper proves the mechanism works.
```

## Negative tests

**No agent in PATH:**

```sh
onibi run definitely-not-installed
# expected: agent "definitely-not-installed" not found in PATH
```

**No owner paired:**

```sh
# wipe owner_id and try
sqlite3 ~/Library/Application\ Support/onibi/onibi.sqlite \
  "DELETE FROM kv WHERE key='owner_id';"
onibi run
# expected: owner not paired — run `onibi setup`
```

**Daemon-only mode (no agent spawn):**

```sh
onibi run
# expected: "daemon-only mode (no agent spawned); waiting for hook events on ..."
# stays listening; you can drive it manually:
ONIBI_SOCK=~/Library/Application\ Support/onibi/onibi.sock \
ONIBI_SESSION_ID=test123 \
  onibi-notify --type agent_done
# the daemon receives the event but session "test123" isn't registered,
# so it silently no-ops (correct — see daemon.notifyTurnComplete).
# Press Ctrl-C to stop the daemon.
```

When Telegram messages arrive cleanly for a real Claude turn-complete,
Phase 2's exit criterion is met.
