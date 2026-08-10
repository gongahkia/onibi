# Threat and compliance model

## Assets and trust boundaries

SQLite state, config references, Telegram tokens, webhook secrets and OneMap
credentials are sensitive. Upstream content, booking URLs, venue names and all
MCP/HTTP inputs are untrusted. The application is a discovery/notification tool,
not a booking agent.

## Controls

- Source policies include allowed hosts, capabilities, credentials, poll floor,
  concurrency and review time. Unknown policy disables network fetching.
- HTTP validates scheme/host, bounds bodies/redirects/retries, honors context and
  redacts credentials from logs/errors.
- SQLite uses parameterized queries, foreign keys, and transactions. Secrets
  never appear in HTTP/MCP/doctor responses.
- Telegram is sent as plain text; webhook event data is JSON encoded and HMAC
  signed when a webhook secret is configured.
- The API binds to loopback by default. Non-loopback requires explicit bearer
  authentication. MCP stdio writes are disabled by default.
- Telegram mutation is limited to configured chat IDs. Webhooks are HTTPS-only
  except an explicit development override.
- No telemetry. The app never stores Singpass/booking credentials or automates
  login, booking, payment, ballots or CAPTCHAs.

## Residual risks

Upstream pages and policies can drift. Approved adapters have fixtures and source
health distinguishes parser/transport/policy failure from no slots. Users remain
responsible for the provider terms when following an official link. Local-disk and
environment-secret security remain the host user's responsibility.
