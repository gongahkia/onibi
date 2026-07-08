# Team Setup

This walkthrough covers one owner sharing one live session with one read-only viewer. Use it for short reviews, debugging, or demos where the teammate should watch the terminal without steering it.

For the underlying models, see [Workspaces](workspaces.md) and [Viewer Sharing](sharing.md).

## Roles

| role   | device                       | purpose                                                            |
| ------ | ---------------------------- | ------------------------------------------------------------------ |
| owner  | your laptop and paired phone | starts Onibi, controls the PTY, decides approvals, revokes viewers |
| viewer | teammate phone or browser    | watches one session through a read-only cockpit                    |

## 1. Create A Shared Workspace

In the project repo, commit only portable workspace defaults:

```bash
mkdir -p .onibi
$EDITOR .onibi/workspace.toml
git add .onibi/workspace.toml
git commit -m "add Onibi workspace defaults"
onibi workspace add my-repo "$(pwd)" --use
```

Minimal `.onibi/workspace.toml`:

```toml
schema_version = 1
name = "my-repo"
default_agent = "claude"
```

Do not commit local paths, provider tokens, SSH keys, Keychain references, SQLite state, or `~/.onibi/`. Those stay in each user's private workspace binding.

## 2. Start The Owner Session

Start Onibi from the workspace checkout:

```bash
onibi up
```

Pair your owner phone from the QR if you need mobile control. Then find the session ID:

```bash
onibi sessions
```

Use the `ID` column for the share command below.

## 3. Invite One Viewer

From the owner phone cockpit, tap `SHARE`, choose a TTL and viewer limit, then send the displayed URL or have the teammate scan the QR.

Mint a one-hour, one-claim viewer URL:

```bash
onibi share <session-id> --ttl 1h --max-viewers 1
```

The command prints a read-only viewer URL and QR. The viewer scans it or opens the URL, pairs through `/pair/<token>`, and lands on `/s/<session-id>`.

Use a shorter TTL for quick checks:

```bash
onibi share <session-id> --ttl 15m --max-viewers 1
```

Use `--copy` on macOS to copy the primary URL, or `--no-qr` when pasting the URL into a secure channel manually.

## 4. Viewer Access Model

The viewer can see:

- live PTY output for the shared session
- terminal replay sent while attaching
- visible editor, prompt, command output, and shell text
- file panel read views exposed to the cockpit
- timeline, snapshot, and session details available to a paired viewer
- any secret printed in the terminal while attached

The viewer cannot:

- type into the PTY
- approve, deny, or edit approval requests
- trigger owner controls such as interrupt, kill, handover, or file writes
- become an owner through the viewer URL
- use the viewer link after its TTL expires or after its claim limit is exhausted

Viewer sharing is not redaction. Stop secret-printing commands before inviting a viewer.

## 5. Revoke Access

From the owner phone cockpit, open `SHARE` and tap `Revoke` next to the viewer.

List paired devices and roles:

```bash
onibi devices
```

Revoke one viewer:

```bash
onibi unpair --viewer <device-id>
```

Revoke every viewer without touching owner devices:

```bash
onibi unpair --all-viewers
```

Stop `onibi up` when the session should no longer be reachable.

## Audit Trail

Viewer WebSocket attach and detach events are written to audit as `viewer.attach` and `viewer.detach` with the viewer ID, source IP, and user-agent. Read them with:

```bash
onibi log --json
```

Onibi does not store a full terminal transcript in the audit log. Treat audit as metadata for viewer access, not as screen recording.

## Teammate Setup

A teammate who needs their own owner session should clone the repo and create a private binding from the committed workspace file:

```bash
git clone <repo-url>
cd <repo>
onibi workspace use ./.onibi
onibi up
```

That creates the teammate's private checkout binding without reusing your owner session or viewer link.
