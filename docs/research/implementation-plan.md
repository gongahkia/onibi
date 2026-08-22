# Badminton prototype implementation record

Updated: 2026-08-22

The prototype is badminton-only. Generic sport selection and sport registry
types were removed from the public domain, CLI, TUI, HTTP API, MCP input, query
pipeline, and cached availability migration.

The current live-reader path is:

```text
partner API -> approved browser credentials/imported session -> approved public page
             -> normalized venue/court/slot snapshot -> SQLite -> search/watches
```

Eight partner source profiles are registered: ActiveSG, onePA, The Kallang,
KFF Badminton Arena, Singapore Badminton Hall, Smash Arena, Wyse Active Hub,
and TruSmash. SBA, Singapore Badminton Hall, Smash Arena, and Wyse Active Hub
have verified anonymous public readers; the other four require source-specific
API contract configuration or approved browser/public JSON selectors. See the
[source audit](source-audit.md) and the root [README](../../README.md).

Each successful refresh records a minimal slot-identity digest, upserts the
current snapshot, and marks absent previously available slots unavailable within
the same source/date range. Refresh defaults to 30 minutes and supports
per-source interval and booking-horizon overrides.

The remaining operator hand-off is deliberately external to the repository:
provide each partner's API mapping/fixture or browser selector contract, plus
environment-backed credentials or base64 imported session state. No approval
emails, cookies, credentials, or personal data belong in source control.
