# kaypoh

`kaypoh` is a local-first terminal application for discovering Singapore sports
facilities, searching normalized availability that you are authorized to use,
ranking options, and monitoring persistent watches.

It does not automate booking, account login, payment, ballots, CAPTCHA solving,
or any attempt to bypass source restrictions.

## What is live today

- SportSG's official data.gov.sg GeoJSON dataset discovers 45 facilities in the
  current live smoke test. It is venue discovery, not a claim of court-slot
  availability.
- OneMap routing/geocoding is used when registered credentials are supplied;
  otherwise rank results label their deterministic Haversine fallback.
- Local manual availability is available for information the user is authorized
  to enter. It powers the complete search, ranking, watch, event, notification,
  API, MCP, and TUI flow without a prohibited scraper.
- All other reviewed operators are visible in `kaypoh sources list` with an
  explicit policy/health state. They are not silently queried.

See [the source audit](docs/research/source-audit.md) for the evidence and
policy decision behind each source.

## Build and start

```sh
go build -o bin/kaypoh ./cmd/kaypoh
./bin/kaypoh config init
./bin/kaypoh
```

Running `kaypoh` opens the terminal UI. It has Discover, Watches, Sources,
Events, and Settings tabs. Use `?` for its key guide; `r` refreshes permitted
sources, `/` edits the sport query, and `w` creates a local watch from Discover.

The CLI is equally useful in scripts. `--json` writes only JSON to stdout;
diagnostics go to stderr.

```sh
kaypoh refresh sportsg-facilities
kaypoh venue list --search "ang mo kio"

# Add availability that you are permitted to supply, then search it.
kaypoh availability add sportsg-facilities:venue:322 badminton \
  --start 2026-08-12T19:00 --end 2026-08-12T20:00 --price 12.50
kaypoh search shuttle --date 2026-08-12 --duration 1h --rank cheap --explain

kaypoh watch add "Wednesday badminton" badminton --date 2026-08-12 --one-shot
kaypoh watch evaluate
kaypoh watch events --json
```

Times without an offset are interpreted in `Asia/Singapore`. Slots use the
half-open interval `[start, end)`.

## Configuration and secrets

The example config is written with mode `0600`. Use `env:NAME` references for
secrets instead of literal tokens.

```toml
[routing]
access_token = "env:ONEMAP_ACCESS_TOKEN"

[telegram]
enabled = true
bot_token = "env:KAYPOH_TELEGRAM_BOT_TOKEN"
allowed_chat_ids = [123456789]

[webhooks.ops]
enabled = true
url = "https://hooks.example.test/kaypoh"
secret = "env:KAYPOH_WEBHOOK_SECRET"
```

Use `--telegram-chat` or `--webhook` when creating a watch target. Telegram
chat IDs must be allowlisted. Webhooks send a versioned JSON envelope with
`X-Kaypoh-Event-ID`, `X-Kaypoh-Event-Type`, and an HMAC SHA-256 signature when
a secret is configured. HTTPS is required except for loopback development URLs.

## Daemon and API

One daemon cycle refreshes each permitted source at most once, evaluates every
watch from local state, and retries pending/failed deliveries. Source poll floors
are enforced, including SportSG's 24-hour floor.

```sh
kaypoh daemon --once --json
kaypoh daemon
kaypoh api serve
curl http://127.0.0.1:8373/v1/health
```

The HTTP API is loopback-only by default and has bounded JSON input. Remote
binding requires `api.allow_remote = true` and an `api.auth_token`; see
[API documentation](docs/api.md).

## MCP

`kaypoh mcp serve` exposes a stdio MCP server using the official Go SDK. It has
read-only availability, venue, source, watch, and event tools by default. Set
`mcp.allow_writes = true` only when the agent should be able to add manual slots,
create watches, or evaluate watches. See [MCP setup](docs/mcp.md) for Hermes and
OpenClaw instructions.

## Verification

```sh
go test ./...
go vet ./...
go build ./cmd/kaypoh
```

The source adapter has fixture tests, the query/ranking/event/delivery layers
have unit tests, API and MCP have transport tests, and the live SportSG smoke is
performed separately because it depends on the public dataset being available.
