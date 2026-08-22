# kaypoh

`kaypoh` is a local-first, badminton-only Singapore court-availability tool.
It reads approved provider data into SQLite, searches fresh slots, ranks courts,
and runs local watches and notifications. It never books, pays, cancels, enters
ballots, or bypasses CAPTCHA/OTP challenges.

## Live-reader status

The following read-only provider adapters are registered: ActiveSG, onePA, The
Kallang / OCBC Arena, KFF Badminton Arena @ Guillemard, Singapore Badminton
Hall, Smash Arena, Wyse Active Hub, and TruSmash. Each uses this configured
order:

1. Partner API.
2. Partner-issued username/password through headless Playwright Chromium, or a
   partner-provided imported session.
3. A partner-approved public availability page.

All partner readers are disabled by default until their approved access details
are configured. This repository contains no credentials, browser session, or
partner API contract. A configured source reports healthy only after a successful
read; an unconfigured source is labelled rather than treated as live.

SportSG's official facility dataset remains enabled for venue discovery. OneMap
is used for optional geocoding/routing when its credentials are configured.

## Build and start

```sh
go build -o bin/kaypoh ./cmd/kaypoh
go run github.com/mxschmitt/playwright-go/cmd/playwright@v0.6201.1 install chromium
./bin/kaypoh config init
./bin/kaypoh
```

The default daemon refresh is 30 minutes. Each partner source can override it.
`availability_max_days = 0` means that source's configured booking horizon.

```sh
kaypoh refresh onepa
kaypoh venue list --search "guillemard"
kaypoh search --date 2026-08-23 --duration 1h --rank cheap --explain
kaypoh watch add "Wednesday badminton" --date 2026-08-23 --one-shot
kaypoh watch evaluate
```

Times without an offset are interpreted in `Asia/Singapore`; slots use the
half-open interval `[start, end)`.

## Partner source configuration

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

Fixture tests cover source-payload normalization, storage-state validation,
freshness/reconciliation, query/ranking, API, MCP, and local watches. A live
provider fetch requires the partner API contract, credentials/session, and
approved selectors; it cannot be verified from this repository alone.
