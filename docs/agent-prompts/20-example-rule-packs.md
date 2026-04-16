# Prompt: Ship Example Rule Packs

You are working in Piranesi, an AppSec CLI with customizable rules. Users need realistic example rule packs to learn from and adapt.

Goal: add first-party example rule packs for common technologies and vulnerability classes.

Implementation requirements:

- Inspect existing `rules/`, `src/piranesi/rules/`, and rule authoring docs.
- Add example packs for at least Node/Express, Python/Flask or Django, Go net/http, PHP/Laravel or Symfony, and Ruby/Rails if the current rule format supports them.
- Each pack should include source specs, sink specs, sanitizers, metadata, and short explanations.
- Add validation tests to ensure example packs parse successfully.
- Add docs showing how to enable, copy, and customize a pack.
- Avoid overclaiming coverage. Mark packs as examples unless they are production-ready.

Acceptance criteria:

- Users can inspect concrete rule packs rather than only abstract docs.
- Example packs are syntactically validated in CI.
- The packs demonstrate receiver constraints and metadata where available.
