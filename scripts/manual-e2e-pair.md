# Manual end-to-end test: pair flow (Phase 1 exit criterion)

This test cannot be automated — it requires a live Telegram bot. Allow ~3
minutes.

## Setup

1. Build: `make build`
2. (Optional, but recommended) point at a throwaway state dir for testing:

   ```sh
   export XDG_DATA_HOME=/tmp/onibi-test
   rm -rf /tmp/onibi-test
   ```

   On macOS the daemon ignores `XDG_DATA_HOME` and writes to
   `~/Library/Application Support/onibi/`. To isolate, instead run the
   manual test inside an isolated user (e.g. a test macOS account).

## Procedure

1. Run `./bin/onibi setup`.
2. Open Telegram on your phone.
3. Message @BotFather → `/newbot`.
4. Set a display name. When asked for the username, paste the suggestion the
   wizard printed (e.g. `onibi_a8f93b04_bot`). BotFather rejects taken
   usernames — bump the random suffix and retry.
5. Copy the token BotFather returns. Paste into the terminal prompt.
6. The terminal prints a deeplink `https://t.me/<bot>?start=pair_<token>`
   plus a QR code. Tap the link on your phone (or scan the QR).
7. Telegram opens the bot chat. Tap **Start**.
8. The bot replies: `Paired. This chat is now the owner channel for Onibi.`
9. Terminal prints: `Paired — owner chat id <N> recorded permanently.`
10. Type `enabled` or `skip` at the 2FA acknowledgment prompt.

## Verify

```sh
# State files exist with correct perms
ls -ld ~/Library/Application\ Support/onibi/        # drwx------ (0700)
ls -l  ~/Library/Application\ Support/onibi/onibi.sqlite  # -rw------- (0600)

# Owner stored
sqlite3 ~/Library/Application\ Support/onibi/onibi.sqlite \
  "SELECT key, value FROM kv WHERE key IN ('owner_id', 'bot_id', 'tg_2fa_ack');"

# Token is in Keychain (macOS)
security find-generic-password -s sh.onibi.daemon -a bot_token -w 2>/dev/null \
  | head -c 20; echo "..."
```

## Negative tests

**Re-running setup must fail without --rotate-owner:**

```sh
./bin/onibi setup
# expect: "owner already paired — re-run with --rotate-owner ..."
```

**Re-pair with --rotate-owner succeeds:**

```sh
./bin/onibi setup --rotate-owner
# wizard runs again; previous owner is overwritten
```

**Token rotation refuses different-bot token:**

```sh
# get a token from a DIFFERENT BotFather bot, try to rotate
./bin/onibi rotate-token
# paste a token from bot-B (different from the one used for setup)
# expect: "token belongs to a different bot — refusing to rotate."
```

**Expired pair token (race the timer manually):**

```sh
# In one shell: ./bin/onibi setup --rotate-owner
# Wait 6 minutes without tapping the link
# Then tap the link
# Bot replies: "Pairing link expired or already used."
```

## Cleanup

```sh
rm -rf ~/Library/Application\ Support/onibi/
security delete-generic-password -s sh.onibi.daemon -a bot_token 2>/dev/null
```

When all of the above pass, Phase 1's exit criterion is met.
