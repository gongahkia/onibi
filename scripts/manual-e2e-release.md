# Manual end-to-end release test

Run on a clean macOS user before a public tag.

1. Install from the Homebrew tap.

   ```sh
   brew install --cask gongahkia/onibi/onibi
   onibi version
   onibi doctor --after-upgrade
   onibi hooks show --all
   ```

2. Pair and complete first-run setup.

   ```sh
   onibi setup --complete
   onibi doctor --mode installed
   ```

3. Verify Telegram.

   - Bot sends the paired welcome message.
   - `/help`, `/status`, and `/sessions` work.

4. Verify one agent.

   ```sh
   onibi adapters
   onibi run claude
   ```

   Trigger a simple prompt, then approve one tool call from Telegram.

5. Verify recovery.

   ```sh
   onibi doctor --fix
   onibi rotate-token
   onibi setup --rotate-owner
   ```

6. Verify uninstall.

   ```sh
   onibi uninstall-service
   onibi doctor --mode preflight --offline
   ```
