# Prompt: Document Plugin APIs as Stable or Experimental

You are working in Piranesi, an AppSec CLI with plugin capabilities. Plugin authors need to know which APIs are stable and which are experimental.

Goal: document plugin API stability and add guardrails that prevent accidental breakage.

Implementation requirements:

- Inspect `src/piranesi/plugin/` if present and plugin-related docs.
- Identify public plugin extension points, hooks, models, and config formats.
- Create documentation that marks APIs as stable, experimental, or internal.
- Add versioning guidance for plugin authors.
- If feasible, add tests or snapshots that lock stable plugin interfaces.
- Add warnings when users rely on experimental plugin APIs if that is practical.
- Update examples to use stable APIs where possible.

Acceptance criteria:

- Plugin authors can tell what is safe to depend on.
- Stable API surfaces have at least lightweight regression coverage.
- Internal APIs are not advertised as stable.
