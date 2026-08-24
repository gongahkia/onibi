# Architecture

`kaypoh` is a local-first Go application. All user surfaces use one application
service layer:

```text
sources -> observations -> normalized SQLite state -> query/ranking -> watches -> events -> notifiers
                                             ^             ^              ^
                               CLI / TUI / HTTP API / MCP adapters --------+
```

The primary executable is `cmd/kaypoh`. Without a subcommand it opens a Bubble
Tea TUI; it also provides a JSON-safe CLI, local HTTP API and MCP server. No
frontend owns business rules and no source adapter knows about watches.

## Data and time

Domain times use `Asia/Singapore`; database timestamps are RFC 3339 UTC. Slots
are half-open `[start,end)` intervals. Normalized IDs are deterministic where
possible, while stable local IDs survive refreshes. Every normalized record holds
source provenance, observed/fetched time, freshness/staleness and a bounded
evidence hash.

SQLite is the local source of truth. Migrations run transactionally with foreign
keys. Credentials live in config
references such as `env:ONEMAP_EMAIL`, never database fields, APIs or MCP data.

## Source boundary

Each source declares immutable identity, policy, allowed hosts, capabilities,
auth requirement, poll floor, concurrency and review metadata. A central HTTP
client applies source policy before network I/O. It implements request coalescing,
cache validators, per-host limits, bounded response bodies/redirects/retries,
backoff and circuit degradation.

The source audit is authoritative: an adapter without approved policy cannot
fetch. SportSG data.gov.sg discovery and credentialed OneMap routing/geocoding
remain available. Eight partner-authorized badminton readers are registered but
disabled until their API credentials, approved browser credentials/session, or
approved public reader configuration is supplied. Their state is visible in
source health.

Partner readers prefer an API, then an authenticated Playwright Chromium page,
then an approved public page. Imported browser state is base64 storage-state JSON
decoded only in memory. Browser code can submit a configured login form but has
no booking, payment, cancellation, or CAPTCHA/OTP path. A successful refresh
persists a bounded snapshot and marks previously available slots absent from that
source/date scope unavailable. ActiveSG can additionally enable its dedicated
imported-session date-card reader after those generic modes; source health
persists each failed access mode in attempt order even when a later mode
succeeds.

## Query, ranking and watches

Queries operate only on normalized badminton slots. Filtering handles source/venue,
date/time, duration, attributes, price and radius. Dedupe requires a high
confidence shared identity. Ranking exposes fit, cost, participant cost,
origin/destination commute, maximum commute, fairness and freshness rather than
an opaque score.

The route provider is an interface. OneMap is preferred with credentials; missing
network/credentials uses labelled deterministic Haversine distance.

The scheduler refreshes each enabled permitted source at most once per cycle,
honours its poll floor, then evaluates all watches locally.
Deterministic fingerprints plus persisted watch state prevent resend after a
restart. A single-instance lock and graceful context cancellation support daemon
operation.

## Interfaces and security

Notifiers implement a small interface. Telegram uses the Bot API with chat
allowlists. HTTPS webhooks use versioned events, HMAC, idempotency and delivery
history.

The HTTP API binds to `127.0.0.1` by default. MCP uses the official Go SDK over
stdio; MCP mutation tools are disabled by default. Neither interface exposes
arbitrary SQL, HTTP, shell commands, booking or credential reads.
