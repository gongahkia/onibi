> Legacy note: This document describes pre-pivot or roadmap behavior. It is retained for historical context and is not current Phase 1 guidance. Current guidance is in README.md, docs/ARCHITECTURE.md, and the pentest workspace/report/retest/sign/serve docs.

# Plugin API Stability

Piranesi plugin extension points are split into **stable**, **experimental**, and **internal** surfaces.
Use this page to decide what external plugin code can safely depend on.

For non-plugin host posture integrations, use the public library and schema
contracts in [`docs/api.md`](api.md). Plugin hooks and the embeddable host API are
separate compatibility surfaces.

## Stability Levels

- `stable`: backward compatibility is expected across patch/minor releases; breaking changes require a documented major-version transition.
- `experimental`: may change in minor releases; use with caution and pin tightly.
- `internal`: implementation detail; do not depend on this surface from external plugins.

## Stable Surfaces (`v1.0`)

Host posture library surface:

- `piranesi.host.api.assess_host_bundle`
- `piranesi.host.api.load_host_report`
- `piranesi.host.api.collect_host_evidence`
- `piranesi.host.models.HostPostureReport`
- `piranesi.host.models.HostSnapshot`
- `piranesi.host.models.FleetReport`
- `piranesi.schema.build_schema`
- `piranesi.schema.write_schema`

Entry point groups:

- `piranesi.frameworks`
- `piranesi.rules`
- `piranesi.reporters`

Framework plugin interface:

- `name(self) -> str`
- `detect(self, project_root: Path) -> bool`
- `source_specs(self) -> list[SourceSpec]`
- `sink_specs(self) -> list[SinkSpec]`
- `sanitizer_specs(self) -> list[SanitizerSpec]`

Rule plugin interface:

- `name(self) -> str`
- `rule_files(self) -> list[Path]`

Reporter plugin interface:

- `name(self) -> str`
- `format_id(self) -> str`
- `render(self, report: object, output_dir: Path) -> Path`

Stable helper functions:

- `discover_framework_plugins`
- `discover_rule_plugins`
- `discover_reporter_plugins`
- `get_framework_plugins_by_name`
- `collect_source_specs`
- `collect_sink_specs`
- `collect_sanitizer_specs`
- `plugin_api_manifest`

## Experimental Surfaces

Framework plugin hooks:

- `tsconfig_overrides(self) -> dict[str, object]`

Runtime guardrail:

- Piranesi logs a warning when a framework plugin overrides `tsconfig_overrides`.

## Internal Surfaces (Not Stable)

- Built-in framework plugin class implementations (`ExpressFramework`, `NestJSFramework`, etc.).
- `_BUILTIN_FRAMEWORK_PLUGINS` registry constant.
- Module-private registry/discovery implementation details.

These are importable for tests and internal wiring but are not compatibility promises for third-party plugins.

## Versioning Guidance For Plugin Authors

- Treat `v1.0` stable APIs as the compatibility contract.
- Pin Piranesi in your plugin project and run CI against new versions before upgrading.
- If you use experimental APIs, pin exact versions and expect migration work on minor upgrades.
- Prefer capability detection over strict type assumptions for report payload internals.
- Read release notes for plugin API changes before bumping your dependency.
