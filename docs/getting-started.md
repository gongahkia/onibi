# Getting Started

This guide takes a fresh machine from no Onibi state to one working Telegram-controlled agent session.

## 1. Prerequisites

You need:

- macOS or Linux.
- Go 1.26.4 or newer when installing from source.
- A Telegram account.
- Telegram 2-Step Verification enabled on that account.
- A bot created with @BotFather.
- `git`, `make`, and a shell supported by your platform.
- Optional: Claude Code installed if you want to test the Claude adapter first.

Onibi stores local state in:

```bash
# macOS
~/Library/Application\ Support/onibi/

# Linux
~/.local/share/onibi/
```

The daemon listens only on a local Unix socket under that state path. It talks outbound to Telegram over HTTPS.

## 2. Install

The source install is the current reliable path:

```bash
git clone https://github.com/gongahkia/onibi
cd onibi
make install
```

Confirm the binaries are available:

```bash
onibi version
onibi-notify --help
```

After the Homebrew tap is published, the release path is:

```bash
brew install --cask gongahkia/onibi/onibi
onibi version
```

## 3. Create The Telegram Bot

Open Telegram and start a chat with `@BotFather`.

Create the bot:

```text
/newbot
```

BotFather asks for:

- A display name, for example `Onibi Home`.
- A username ending in `bot`, for example `onibi_home_bot`.

BotFather returns an HTTP API token. Keep it private. Onibi stores it in the OS keychain when available and falls back to a `0600` `.env` file in the state directory when no keychain backend is available.

Recommended BotFather cleanup:

```text
/setdescription
/setuserpic
```

Those are optional. The only required value is the token from `/newbot`.

## 4. Pair Onibi

Run the happy-path command:

```bash
onibi up
```

On a fresh machine, `onibi up` delegates to:

```bash
onibi setup --complete
```

During setup:

- Paste the BotFather token when prompted.
- Open the printed Telegram link.
- Press the pairing button or send the requested message.
- Confirm Telegram 2-Step Verification when asked.
- Accept service install if you want Onibi to run in the background.
- Accept hook install if you want automatic agent and shell notifications.

If you need to avoid putting the bot token in terminal scrollback, use stdin:

```bash
printf '%s\n' '123456789:replace-with-botfather-token' | onibi setup --token-stdin --complete
```

After setup, run:

```bash
onibi doctor
onibi adapters
onibi config list
onibi demo approval
```

Expected result:

- `doctor` reports the state directory, SQLite DB, owner chat id, token, and hook status.
- `adapters` shows which agent hooks are installed or available.
- `config list` prints daemon, shell, and Telegram settings.
- `demo approval` sends a labelled test approval to Telegram without executing an agent tool.

## 5. Start The First Session

Open Telegram and send:

```text
/menu
```

Use the onboarding buttons to add a project, choose an agent, start visible,
or trigger a test approval. The test approval path does not require trusted
agent hooks.

For the normal Telegram flow, add the repo as an explicit project alias:

```bash
onibi project add here
```

Or from Telegram:

```text
/project add onibi ~/Desktop/coding/projects/onibi
```

`/project list` shows alias health and buttons for starting visible or
headless sessions from saved aliases.

Start a visible tmux-backed session from Telegram:

```text
/new --visible --project onibi codex
```

Visible means Onibi opens a local terminal attached to the same tmux session.
Headless means the same session runs without an attached terminal window:

```text
/new --headless --project onibi shell
```

Onibi uses tmux as the control plane. Ghostty, iTerm2, or Terminal.app only
render the session when visible; Onibi does not inject into arbitrary terminal
tabs.

Choose a visible-session launcher:

```bash
onibi config set terminal.default auto
onibi config set terminal.default ghostty
onibi config set terminal.default iterm2
onibi config set terminal.default terminal
onibi config set terminal.default none
```

`auto` tries Ghostty, then iTerm2, then Terminal.app, then prints the manual
`tmux attach-session` command.

CLI starts still work for local testing:

```bash
onibi run claude
onibi shell
onibi wrap bash
```

In Telegram, send:

```text
/status
/sessions
```

You should see the running session id, agent name, and current daemon status.

## 6. Trigger The First Approval

To test approvals before agent hooks are trusted, run:

```bash
onibi demo approval
```

For a real agent approval, ask the agent to do something that uses a tool, for example:

```text
list the files in this repo
```

When the agent attempts the tool call, Onibi sends an approval message to Telegram.

Use Telegram to choose:

- Approve: let the original tool input run.
- Deny: block it.
- Edit: send modified JSON input back to the agent.

After the turn finishes, Onibi sends a completion notification. Use:

```text
/peek <session-id>
```

or:

```text
/render <session-id>
```

to inspect recent output from Telegram. `/render` is a PNG render of the
Onibi PTY/tmux buffer, not a Ghostty window screenshot. `/screenshot` remains a
compatibility alias for `/render`.

## 7. Common Next Steps

Install or refresh hooks:

```bash
onibi install-hooks --interactive
onibi doctor --fix
```

Run as a background service:

```bash
onibi install-service
onibi doctor --mode installed
```

Start another agent:

```bash
onibi run codex
onibi run gemini
onibi run goose
```

List recorded sessions:

```bash
onibi sessions
onibi sessions --all
```

Inspect audit logs:

```bash
onibi log -n 20
onibi log --json --n 20
```

Rotate a leaked bot token:

```bash
onibi rotate-token
onibi doctor
```

## 8. Encrypted Telegram Mode

Default mode sends approval and prompt bodies as normal Telegram bot messages.

Encrypted mode sends ciphertext through Telegram and decrypts in the Mini App:

```bash
onibi setup --enable-encrypted-mode --encrypted-mode on
onibi config get telegram.encrypted_mode
```

Setup prints a Mini App seed URL and QR code. Open it in Telegram so the Mini App stores the seed in Telegram SecureStorage.

When encrypted mode is on:

- Use `/secure` for prompt entry and approvals.
- Plain `/prompt`, `/send`, `/editprompt`, and `/rename <id> <name>` refuse plaintext.
- The Mini App host must stay static and trusted because it renders decrypted plaintext.

Read the full model before relying on encrypted mode:

```bash
less docs/encrypted-mode.md
```

## 9. Troubleshooting

No Telegram messages:

```bash
onibi doctor
onibi log -n 20
```

Service not running:

```bash
onibi install-service
onibi doctor --mode installed
```

Hook drift:

```bash
onibi adapters
onibi doctor --fix
```

Lost bot token:

```bash
onibi rotate-token
```

Lost owner account:

```bash
onibi setup --rotate-owner
```

More cases:

```bash
less docs/troubleshooting.md
```

## 10. Clean Reset

Stop the service first:

```bash
onibi uninstall-service
```

Remove all local Onibi state:

```bash
# macOS
rm -rf ~/Library/Application\ Support/onibi/

# Linux
rm -rf ~/.local/share/onibi/
```

Then pair again:

```bash
onibi up
```
