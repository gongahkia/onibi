# ADR 0001: local-first Go application with SQLite and compliant adapters

Date: 2026-08-10

## Context

The repository is empty. The target is a terminal-first Singapore facility
discovery, ranking and watch application with a TUI, CLI, local API and MCP.
Source policy is heterogeneous and many booking systems are not safe to automate.
The application must remain useful while optional services are unavailable.

## Decision

Implement one Go binary with SQLite local persistence. Use Bubble Tea for the
interactive client, Cobra for commands, the official Go MCP SDK, and
`modernc.org/sqlite` to avoid a mandatory C compiler. Source adapters are
capability/policy-driven. The app calls only approved sources; links/manual
metadata are modeled as capabilities, not failed scrapers.

Use `Asia/Singapore` in the domain, RFC 3339 UTC storage, `[start,end)` slots,
shared application services and a routing interface with Haversine fallback.

## Consequences

The binary is portable and local by default. SQLite makes watches, event
deduplication, health and cache state durable without infrastructure. Some
booking sites have no automated availability initially; that status is explicit.
Future live adapters need a fresh policy audit, fixtures and contract tests. The
project intentionally excludes booking, payment, account creation, CAPTCHA
handling, browser evasion, proxy rotation and credential harvesting.
