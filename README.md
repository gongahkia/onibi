# kaypoh

`kaypoh` is a local-first, badminton-only Singapore court-availability tool.
It reads approved provider data into SQLite, searches fresh slots, ranks courts,
and runs local watches and notifications. It never books, pays, cancels, enters
ballots, or bypasses CAPTCHA/OTP challenges.

## Live-reader status

Four anonymous, read-only availability readers are enabled by default and need
no API key, account, or browser configuration:

| Source | Coverage | Default window |
| --- | --- | --- |
| KFF Badminton Arena @ Guillemard (SBA) | Premium Courts 1–9 | 7 days |
| Singapore Badminton Hall | SBH @ Sims, SBH East Coast @ EXPO, TSA @ EXPO | 7 days |
| Smash Arena | public court calendar | 1 day |
| Wyse Active Hub | Ace and Premier Courts | 7 days |

They use the provider's public booking availability requests, but never submit
a booking, payment, confirmation, login, CAPTCHA, or OTP action. The readers
have a one-hour poll floor. Smash Arena is intentionally one day by default:
its public surface requires a separate read for every available hour; increase
`availability_max_days` only if that extra upstream load is appropriate.

ActiveSG and The Kallang / OCBC Arena remain disabled by default because they
require operator-imported Playwright storage state. onePA remains disabled until
its public facility IDs are configured. ActiveSG has a typed JSON reader
authenticated by operator-imported Playwright storage state. The Kallang has a
dedicated browser reader that selects only the **Badminton Courts**
facility type, advances the weekly calendar, and reads its JSON response. It
does not automate login or select a facility, slot, ballot, cart, checkout, or
payment flow. The remaining partners can be configured with a partner API,
approved browser session, or public JSON mapper, in that order.
A configured API/browser/public reader takes precedence over a built-in reader,
so a provider can migrate to an official API without code changes. For
ActiveSG and The Kallang, enabled generic API, browser, and public modes run in
that order before the dedicated reader; source health records every failed mode
in order.

SportSG's official facility dataset remains enabled for venue discovery. OneMap
is used for optional geocoding/routing when its credentials are configured.

## Build and start

```sh
go build -o bin/kaypoh ./cmd/kaypoh
npm ci --ignore-scripts
go run github.com/mxschmitt/playwright-go/cmd/playwright@v0.6201.1 install chromium
./bin/kaypoh config init
./bin/kaypoh
```

The default daemon refresh is 30 minutes; the public readers enforce their
one-hour source poll floor. Each source can request a longer interval.
`availability_max_days = 0` means that source's configured booking horizon.

```sh
kaypoh refresh sba-stadium singapore-badminton-hall smash-arena wyse-active
kaypoh venue list --search "guillemard"
kaypoh search --date 2026-08-23 --duration 1h --rank cheap --explain
kaypoh watch add "Wednesday badminton" --date 2026-08-23 --one-shot
kaypoh watch evaluate
```

Times without an offset are interpreted in `Asia/Singapore`; slots use the
half-open interval `[start, end)`.

## Docker

The Docker image includes Node.js, the pinned ActiveSG request transport,
the matching Go Playwright driver, headless Chromium, and its Linux dependencies.
Runtime data lives in the named `kaypoh-data`
volume; the local configuration and all secrets remain outside the image.

```sh
cp docker/config.toml.example docker/config.toml
cp .env.example .env
# No source secret is needed for the built-in public readers. Edit the files
# only to configure optional sources, OneMap, notifications, or the API.

docker compose build
docker compose run --rm daemon config validate
docker compose up -d daemon
```

The default Compose service runs the refresh daemon. Run ad-hoc commands
against the same persisted SQLite database with:

```sh
docker compose run --rm daemon sources list
docker compose run --rm daemon refresh sba-stadium singapore-badminton-hall smash-arena wyse-active
docker compose run --rm daemon sources doctor
docker compose run --rm daemon search --date 2026-08-23 --duration 1h
```

To start the optional HTTP API, set a strong `KAYPOH_API_TOKEN` in `.env` and
then start its Compose profile. The published port is restricted to the Docker
host's loopback interface (`127.0.0.1:8373`).

```sh
docker compose --profile api up -d api
curl -sS http://127.0.0.1:8373/v1/health \
  -H "Authorization: Bearer $KAYPOH_API_TOKEN"
```

Kaypoh runs as an unprivileged `kaypoh` user; its entrypoint first gives that
user access to the dedicated data volume. Do not mount the generated `.env` or
`docker/config.toml` into another image, and keep the latter out of version
control. To stop services without deleting saved availability data, use
`docker compose down`. Removing `kaypoh-data` deletes the local SQLite database
and watch history.

For a device change, repeatable provider audit, or the next provider onboarding
pass, follow the [device migration and provider onboarding runbook](docs/operations/device-migration-and-provider-onboarding.md).

## Optional partner source configuration

The config file is written with mode `0600`. Use `env:NAME` references for
every secret. Browser state is base64-encoded Playwright storage-state JSON;
it is decoded only in memory and is never written by kaypoh.

```toml
[sources.onepa]
enabled = true
refresh_minutes = 30
availability_max_days = 0

# Use this first when the partner supplies an API contract.
[sources.onepa.api]
enabled = true
base_url = "https://partner-api.example"
availability_path = "/v1/badminton/availability"
bearer_token = "env:KAYPOH_ONEPA_API_TOKEN"

# Use this if no API is available. Login selectors are required only when no
# imported session is supplied. The reader submits only the login form and then
# opens the availability URL; it contains no booking interaction.
[sources.onepa.browser]
enabled = false
availability_url = "https://partner.example/availability?from={start_date}&to={end_date}"
login_url = "https://partner.example/login"
username = "env:KAYPOH_ONEPA_USERNAME"
password = "env:KAYPOH_ONEPA_PASSWORD"
username_selector = "input[name=email]"
password_selector = "input[name=password]"
submit_selector = "button[type=submit]"
ready_selector = "[data-availability-ready]"
slot_json_selector = "script#kaypoh-slots"
# session_state_base64 = "env:KAYPOH_ONEPA_SESSION_STATE_B64"

# Use this final path only for an approved public reader.
[sources.onepa.public]
enabled = false
availability_url = "https://partner.example/availability?from={start_date}&to={end_date}"
slot_json_selector = "script#kaypoh-slots"

# Final fallback: the built-in anonymous onePA reader. Use the exact public
# facilityId selected in onePA's availability page. It reads named courts and
# available slots only; it never starts a booking.
[sources.onepa.onepa]
enabled = true
facility_ids = ["WoodlandsCC_BADMINTONCOURTS"]
```

The configured API response, or the JSON text selected from a browser/public
page, must contain `slots` (or `availability`) and may contain `venues`:

```json
{
  "slots": [{
    "id": "provider-slot-id",
    "venue_id": "provider-venue-id",
    "venue_name": "KFF Badminton Arena",
    "court_id": "court-3",
    "court_name": "Court 3",
    "start_at": "2026-08-23T19:00:00+08:00",
    "end_at": "2026-08-23T20:00:00+08:00",
    "status": "available",
    "price_cents": 1400,
    "currency": "SGD",
    "booking_url": "https://partner.example/book"
  }]
}
```

`venue_id`, start, and end are required. `status` defaults to `available`.
Partner API contracts that differ from this shape need a small provider payload
mapper before they can be enabled.

If a source requires CAPTCHA, OTP, or another interactive challenge, provide a
partner-generated session state or leave the source disabled. Kaypoh does not
attempt to solve or bypass interactive challenges.

### ActiveSG badminton reader

The ActiveSG reader is a separate, dedicated path because its typed tRPC JSON
contract differs from the generic contract above. It accepts only the verified
badminton venue-list URL and an imported session. It does not accept credentials
or login selectors and does not launch Chromium during a refresh.

```toml
[sources.myactivesg]
enabled = true
refresh_minutes = 60
availability_max_days = 15

[sources.myactivesg.activesg]
enabled = true
venue_list_url = "https://activesg.gov.sg/facility-bookings/activities/YLONatwvqJfikKOmB5N9U/venues"
venue_names = ["Jurong East Sport Hall", "Bukit Gombak Sport Hall"]
scan_all = false
session_state_base64 = "env:KAYPOH_ACTIVESG_SESSION_STATE_B64"
```

Set `scan_all = true` and omit `venue_names` only when a sequential scan of
every venue on the current list is intended. Name matching is case-insensitive
substring matching; `Sports` is normalized to ActiveSG's official singular
`Sport`. The reader maps typed **instant** start/end ranges to bookable slots.
Ballot-only and empty dates are read as non-bookable results, but are not published as bookable slots:
Kaypoh has no ballot-entry feature and its search results mean a concrete,
bookable time. The schedule response exposes available subvenue IDs but not
human-readable court names, so these records remain venue-level availability.

### The Kallang / OCBC Arena reader

The Kallang reader is restricted to
`https://thekallang.perfectgym.com/clientportal2/`, an imported Playwright
storage-state value, and the **Badminton Courts** facility type. It reads the
`GetWeeklySchedule` calendar response after loading the page, selecting that
facility type when needed, and clicking `Next week`. It never selects a named
facility, an individual slot, or any booking-related control.

```toml
[sources.the-kallang]
enabled = true
refresh_minutes = 60
availability_max_days = 30

[sources.the-kallang.perfectgym]
enabled = true
availability_url = "https://thekallang.perfectgym.com/clientportal2/"
facility_type_name = "Badminton Courts"
session_state_base64 = "env:KAYPOH_THE_KALLANG_SESSION_STATE_B64"
```

The observed calendar payload exposes `StartTime`, `EndTime`, and `Status`,
but no stable per-court identity when using “Any facility”. Kaypoh therefore
deduplicates identical bookable times and publishes them as venue-level slots;
it does not claim a particular badminton court is free.

## HTTP API and MCP

The local HTTP API and MCP server query already-normalized badminton slots; they
never refresh an upstream source or expose credentials. See [API documentation](docs/api.md)
and [MCP setup](docs/mcp.md).

## Verification

```sh
go test ./...
go vet ./...
go build ./cmd/kaypoh
```

Fixture tests cover source-payload normalization, public-response parsing,
cookie-session isolation, freshness/reconciliation, query/ranking, API, MCP,
and local watches. The public readers depend on external booking surfaces, so
run `kaypoh refresh …` to exercise their current contracts. `sources doctor`
reports the most recently persisted source health; it does not make another
upstream request.
