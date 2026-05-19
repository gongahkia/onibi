# Product Interface Decision

Date: 2026-05-20

Status: accepted for the red-team workspace pivot.

## Decision

Piranesi should make the local web app the primary operator workflow surface and keep the CLI as a companion for automation, bulk import, CI, and scripted validation.

The web app must be useful on an empty workspace. It should guide an operator through engagement setup, evidence capture, timeline building, objective tracking, scanner finding review, detection handoff, report generation, and signing. The CLI remains the stable interface for repeatable runs and power users.

## Options Considered

| Interface | Strengths | Weaknesses | Decision |
| --- | --- | --- | --- |
| Local web app | Works well for evidence review, timeline scanning, report/handoff navigation, and guided empty states. Easy to run locally without deploying SaaS. | More implementation work than CLI-only; browser upload and local data handling must stay careful. | Primary product surface. |
| CLI-only | Excellent for automation, reproducibility, CI, and fast operator commands. Simple to test and script. | Poor fit for visual evidence review, timelines, screenshots, and narrative/handoff assembly. | Keep as companion, not primary. |
| Desktop app | Strong local-first posture and native file handling. | Higher packaging/support cost and less portable than a browser UI. | Defer until the workspace workflow proves useful. |
| Chat or bot | Good for notifications and lightweight summaries. | Weak for browsing evidence, editing timelines, reviewing screenshots, and managing structured handoff data. | Do not use as the primary interface. |

## Rationale

Red-team and assessment users can tolerate CLI workflows, but engagement management is visual and iterative. Piranesi needs to help users inspect artifacts, link events, manage objectives, and prepare handoff packages without forcing them to remember every command. A local web app gives that workflow surface while preserving the project’s local-first evidence posture.

The CLI remains important because scanner imports, report generation, signing, retest comparison, CI validation, and bulk data movement need deterministic commands.

## Implementation Requirements

- `piranesi serve` binds to loopback by default.
- Non-loopback access requires explicit acknowledgement.
- The app can create an empty workspace and initialize engagement metadata.
- The app exposes guided empty states for evidence, timeline, objectives, findings, detection handoff, reports, and signing.
- Operator note evidence can be added from the UI.
- Scanner imports remain available from the CLI and are represented in the UI through findings and source counts.
- The service boundary stays JSON-based so future desktop, bot, or collaboration interfaces can reuse the workspace model.

## Current Limits

- File upload UI is not yet implemented; the first browser evidence path captures typed operator notes.
- Red-team PDF output is not implemented yet; red-team handoff currently supports Markdown and JSON.
- The web app is still local single-user software and does not provide hosted auth, RBAC, or multi-operator synchronization.
