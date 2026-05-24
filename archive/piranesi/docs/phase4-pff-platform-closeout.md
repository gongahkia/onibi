# Phase 4 PFF Platform Closeout

Date: 2026-05-20

Status: implemented as alpha public interchange and SDK foundation.

Phase 4 is complete at the v0 platform level. The implementation now includes:

- Piranesi Finding Format v0 documentation and JSON Schema;
- PFF validation helpers and CLI validation;
- workspace findings export to PFF;
- PFF import into a workspace with upsert, source-reference preservation, and
  import provenance;
- versioning and migration rules for the current schema version;
- committed PFF compatibility fixture validation;
- Python adapter SDK v0 for generating valid PFF documents;
- plugin API boundaries and security model;
- CI validation commands for PFF artifacts and report bundles.

The current boundary is intentionally conservative. PFF is a data interchange
format and adapter output contract, not an in-process plugin runtime. Third-party
adapter output remains untrusted until it passes validation, and plugins must not
bypass provenance, redaction, source references, or workspace import controls.

Issue #42 can close once child issues #72 through #80 are closed on GitHub. Future
platform work should be tracked as narrower follow-up issues, especially any
Go/TypeScript SDKs, registry mechanics, or executable plugin runtime.
