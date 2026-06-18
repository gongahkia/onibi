# Manual end-to-end release test

Run on a clean macOS user before a public tag.

1. Install from the Homebrew tap.

   ```sh
   brew install --cask gongahkia/onibi/onibi
   onibi version
   onibi doctor --after-upgrade
   onibi hooks --show --all
   ```

2. Pair and complete first-run setup.

   ```sh
   onibi setup --complete
   onibi doctor --mode installed
   ```

3. Verify Telegram.

   - Bot sends the paired welcome message.
   - `/help`, `/status`, `/sessions`, and `/menu` work.
   - Approval cards show agent, session, project, tool, risk, target, and TTL.
   - Encrypted mode `/secure` opens and completes the encrypted control path.

4. Verify provider hooks.

   ```sh
   onibi adapters
   onibi run claude
   ```

   Trigger a simple prompt, then approve one tool call from Telegram.
   Confirm provider-specific hook behavior:

   - Codex fresh open after hook install shows review prompt, not parse warning.
   - Claude `/hooks` shows Onibi hooks.
   - Gemini hook install has correct timeout units.
   - Goose starts with plugin present.
   - OpenCode plugin reloads.
   - Amp plugin reloads and deny path behaves as intended.
   - Pi `/reload` loads extension.
   - Copilot hook file loads with no schema warning.

5. Verify recovery.

   ```sh
   onibi doctor --fix
   onibi rotate-token
   onibi setup --rotate-owner
   ```

6. Verify uninstall.

   ```sh
   onibi hooks --show --all
   onibi uninstall --dry-run
   onibi uninstall
   onibi doctor --mode preflight --offline
   ```
