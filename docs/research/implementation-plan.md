# Initial implementation plan

Reviewed: 2026-08-10

1. Establish the Go module, config, typed domain model, SQLite migrations,
   structured logging, CLI skeleton and baseline tests.
2. Add the real SportSG data.gov.sg discovery adapter and OneMap geocode/routing
   with cache plus Haversine fallback.
3. Implement policy-aware source framework, health, source doctor and curated
   link-only registry from the source audit.
4. Build one normalized filter/ranking service: multi-sport/date/time/price,
   contiguous slots, price/person, multi-party commute and explanations.
5. Expose it through CLI JSON, loopback OpenAPI HTTP, MCP and Bubble Tea views.
6. Add persistent watches, scheduler, event fingerprints, daemon lock and
   Watches/Events TUI.
7. Add Telegram and signed webhooks with delivery history and fake transports.
8. Finish onboarding, replay tooling, completions, CI, packaging, docs and a
   clean-state verification pass.

Each live provider is a follow-on only after an approved source policy, fixtures
and contract tests exist.
