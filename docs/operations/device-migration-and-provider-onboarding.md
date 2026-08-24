# Device migration and provider onboarding

This runbook is the handoff for moving Kaypoh to another device and continuing
provider work without relying on a local scraper prototype. It is deliberately
limited to read-only availability discovery: do not add booking, ballot entry,
checkout, payment, CAPTCHA, OTP, account-management, or credential-extraction
automation.

## Handoff status

Completed on 2026-08-24:

- The Docker image builds with its matching Playwright driver and Chromium.
- The enabled anonymous providers passed a live Docker refresh: SBA, Singapore
  Badminton Hall, Smash Arena, Wyse Active, and SportSG facilities.
- The local `daemon` service was started with the documented Compose command.
- A dedicated ActiveSG reader exists for the verified **badminton** venue-list
  URL. It requires an imported browser session and is disabled by default.
- A dedicated The Kallang reader exists for the PerfectGym **Badminton Courts**
  calendar. It requires an imported browser session and is disabled by default.
- A dedicated onePA reader exists for configured public facility IDs. It uses
  the anonymous availability endpoint, maps named courts, and is disabled by
  default.

Not yet complete:

- ActiveSG pickleball or other activities. The present reader deliberately
  accepts only the badminton activity URL; it is not a generic all-sport
  ActiveSG reader.
- A live ActiveSG or The Kallang scan with a transferred session state.
- A live onePA refresh with an enabled configured facility ID, and TruSmash.
  TruSmash remains disabled until a provider-specific approved API, imported
  session, or public payload contract is captured and implemented.
- OneMap routing. It remains `credentials_required` until local credentials are
  supplied.

## 1. Preserve the Git handoff before deleting this device

The working tree contains the provider-reader implementations and documentation.
Deleting the local repositories before committing and pushing would lose that
work. Review and publish the tracked changes first:

```sh
git status --short
git diff --check
git add README.md .env.example docker/config.toml.example docs internal
git diff --cached --stat
git diff --cached
git commit -m "Add provider availability readers and handoff"
git push origin main
git status --short
```

Do not add `docker/config.toml`, `.env`, a Playwright storage-state file, a
SQLite database, or Docker-volume archives. They are intentionally ignored and
may contain secrets or personal local history.

## 2. Decide what moves outside Git

The repository contains code and documentation only. Copy the following through
an approved encrypted transfer or recreate them on the new device:

- `docker/config.toml`: local source choices, never the secret values
- `.env`: API tokens, browser storage-state values, and optional notification
  secrets
- optional `kaypoh-data` Docker volume: normalized availability cache and watch
  history; it is not needed for a clean start

Before exporting the optional volume, stop Compose without the `-v` flag:

```sh
docker compose down
```

To create a portable volume archive outside the repository, use a private
directory and transfer the resulting archive securely:

```sh
KAYPOH_MIGRATION_DIR="$HOME/kaypoh-migration"
mkdir -p "$KAYPOH_MIGRATION_DIR"
chmod 700 "$KAYPOH_MIGRATION_DIR"
docker run --rm \
  -v kaypoh_kaypoh-data:/data:ro \
  -v "$KAYPOH_MIGRATION_DIR:/backup" \
  ubuntu:24.04 \
  tar -C /data -czf /backup/kaypoh-data.tgz .
```

The volume name above is the default for a clone in a directory named
`kaypoh`. Confirm it with `docker volume ls` before exporting. Do not place the
archive in the repository or upload it to a public service.

## 3. Bootstrap the new device

Clone the pushed repository, restore only the local configuration/secrets you
intend to keep, and run the same bounded validation sequence:

```sh
git clone https://github.com/gongahkia/kaypoh.git
cd kaypoh
cp docker/config.toml.example docker/config.toml
cp .env.example .env
# Restore approved values in docker/config.toml and .env through a private path.

docker compose build
docker compose run --rm daemon config validate
docker compose run --rm daemon sources list
docker compose run --rm daemon refresh --json
docker compose run --rm daemon sources doctor
docker compose run --rm daemon search --date 2026-08-23 --duration 1h
docker compose up -d daemon
docker compose ps
```

The refresh command is the live provider-contract test. `sources doctor` then
shows its stored health; it is not a replacement for `refresh`. A result with
`"state": "degraded"`, an error exit, an unexpected `records_parsed` count,
or a changed payload contract starts a new provider investigation.

To restore the optional volume on the new device, do this only before Kaypoh
has written new data:

```sh
docker compose down
docker volume create kaypoh_kaypoh-data
docker run --rm \
  -v kaypoh_kaypoh-data:/data \
  -v "$KAYPOH_MIGRATION_DIR:/backup:ro" \
  ubuntu:24.04 \
  tar -C /data -xzf /backup/kaypoh-data.tgz
```

## 4. Capture a provider contract before writing an adapter

Repeat this process separately for each provider and activity. Do not assume a
badminton page, a pickleball page, or a different venue uses the same contract.

1. Confirm the allowed read scope and source policy. Record the activity URL,
   permitted hosts, date horizon, poll floor, and the explicit rule that no
   booking action may occur.
2. In an authenticated browser only when necessary, manually navigate to the
   provider's availability page. Open DevTools **Network**, clear the log, and
   filter for `fetch`/`XHR`.
3. Manually choose one date or other non-committing availability control. Do
   not select a slot, submit a ballot, proceed to checkout, or trigger payment.
4. Record the request method, stable URL/path, non-secret request fields,
   response content type, date/time zone fields, venue/court identifiers,
   availability status, pagination, and the smallest stable DOM selector when
   the response does not contain the data directly.
5. Save a sanitized fixture and a short contract note. Remove cookies,
   authorization headers, user IDs, emails, phone numbers, booking references,
   and any other personal data. Never paste storage-state JSON, cookies, or
   credentials into Git or chat.

Use this contract-note template in the provider's fixture or documentation:

```text
provider and activity:
read scope / approval:
availability page URL:
request method and stable path:
request date and timezone fields:
response type and stable selector or JSON path:
venue ID/name fields:
court ID/name fields:
slot start/end/status fields:
pagination and horizon:
poll floor and timeout:
empty-result behavior:
failure behavior:
no-side-effect interaction boundary:
sanitized fixture path:
```

If a page needs a session, export it using an approved Playwright storage-state
workflow on the new device, encode it as base64, and put it only in `.env`.
Configure the corresponding `session_state_base64 = "env:..."` value in
`docker/config.toml`. The state is decoded in memory; do not store it as a
tracked file.

## 5. ActiveSG-specific next work

The current ActiveSG reader has a narrow, tested contract:

- The only accepted activity URL is badminton:
  `https://activesg.gov.sg/facility-bookings/activities/YLONatwvqJfikKOmB5N9U/venues`.
- It reads venue links, clicks date cards labelled `View timeslots for …`, and
  normalizes visible instant hourly starts as one-hour slots.
- Ballot-only, already-balloted, and empty dates are retained as non-bookable
  observations; no ballot or booking action is attempted.
- It is venue-level because the observed date-card surface has no stable court
  identity.

For pickleball or another sport, first complete the contract capture in step 4
for that activity. Then refactor the reader so the allowed activity is an
explicit, validated activity specification rather than weakening the current
badminton URL check. Add fixtures and tests for that sport before enabling it.

To enable a scoped badminton scan after a session has been transferred:

```toml
[sources.myactivesg]
enabled = true
refresh_minutes = 60
availability_max_days = 15

[sources.myactivesg.activesg]
enabled = true
venue_list_url = "https://activesg.gov.sg/facility-bookings/activities/YLONatwvqJfikKOmB5N9U/venues"
venue_names = ["Jurong East Sport Hall"]
scan_all = false
session_state_base64 = "env:KAYPOH_ACTIVESG_SESSION_STATE_B64"
```

Use `scan_all = true` only after deliberately accepting a sequential scan of
the full current venue list. Keep the one-hour poll floor. Validate with
`docker compose run --rm daemon refresh --json myactivesg`; a real scan is
required before treating the reader as healthy.

## 6. Implement and verify the next provider

For every new provider or activity:

1. Add or update the source policy and documentation.
2. Keep provider parsing in a dedicated adapter or a documented generic mapper;
   do not make generic selectors silently absorb provider-specific DOM changes.
3. Add sanitized fixtures covering available, empty, and malformed/changed
   responses.
4. Add contract tests that assert normalized venue, court, time, status, and
   source IDs.
5. Run the narrow source tests, then the full checks:

   ```sh
   go test ./...
   go vet ./...
   go build ./cmd/kaypoh
   docker compose build
   docker compose run --rm daemon config validate
   docker compose run --rm daemon refresh --json <source-id>
   docker compose run --rm daemon sources show <source-id>
   ```

6. Commit and push the code, fixtures, tests, policy update, and contract note
   together. Keep secrets and live browser state out of the commit.

## 7. Retire the old device only after verification

Before removing the old repositories, confirm all of the following:

- `git status --short` is clean after the commit and push.
- The new-device clone contains the commit with this runbook and the ActiveSG
  implementation.
- The new device can build, validate, refresh, search, and start the daemon.
- Required local secrets/session state are present on the new device through a
  private transfer, or intentionally recreated.
- Any wanted Docker volume archive has been restored or deliberately discarded.

Only then remove the old checkout and its local Docker resources. Use
`docker compose down` to stop Kaypoh while preserving its data. Removing the
`kaypoh-data` volume is irreversible for local cache and watch history.
