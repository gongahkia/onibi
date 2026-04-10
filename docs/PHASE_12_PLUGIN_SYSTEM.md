# Phase 12: Plugin and Extension System

**Estimated effort: 30-40 ideal hours**
**Blocked by: Phase 10 (multi-framework proves the pattern)**
**Blocks: Phase 13 (multi-language uses the plugin system)**
**Target milestone: v0.3.0**

---

## 1. Phase Overview

Piranesi's source/sink specs, regulatory rules, and framework support are currently hardcoded. For community adoption and extensibility, these need to be pluggable: third parties should be able to add their own framework support, regulatory rules, and sink/source specs without forking.

This phase builds a plugin system based on Python entry points (`pyproject.toml` `[project.entry-points]`) that allows pip-installable plugins to extend Piranesi's capabilities.

---

## 2. Plugin Architecture

### 2.1 Plugin Types

| Plugin Type | Registration Point | What It Provides |
|-------------|-------------------|------------------|
| `piranesi.frameworks` | Framework detection + specs | Source/sink specs, sanitizer patterns, tsconfig overrides |
| `piranesi.rules` | Regulatory engine | TOML rule files for new jurisdictions/frameworks |
| `piranesi.reporters` | Report generation | New output formats (SARIF is built-in, but plugins could add CSV, PDF, Jira, etc.) |
| `piranesi.languages` | Language support (Phase 13) | Transpilation/parsing pipelines for non-TS/JS languages |

### 2.2 Entry Point Convention

A Piranesi plugin is a pip-installable Python package that declares entry points:

```toml
# piranesi-plugin-nestjs/pyproject.toml
[project.entry-points."piranesi.frameworks"]
nestjs = "piranesi_plugin_nestjs:NestJSFrameworkPlugin"
```

### 2.3 Plugin Interface

```python
# src/piranesi/plugin.py
from abc import ABC, abstractmethod

class FrameworkPlugin(ABC):
    @abstractmethod
    def name(self) -> str: ...
    @abstractmethod
    def detect(self, project_root: Path) -> bool: ...
    @abstractmethod
    def source_specs(self) -> list[SourceSpec]: ...
    @abstractmethod
    def sink_specs(self) -> list[SinkSpec]: ...
    @abstractmethod
    def sanitizer_specs(self) -> list[SanitizerSpec]: ...
    def tsconfig_overrides(self) -> dict[str, object]:
        return {}

class RulePlugin(ABC):
    @abstractmethod
    def name(self) -> str: ...
    @abstractmethod
    def rule_files(self) -> list[Path]: ...

class ReporterPlugin(ABC):
    @abstractmethod
    def name(self) -> str: ...
    @abstractmethod
    def format_id(self) -> str: ...
    @abstractmethod
    def render(self, report: PiranesiReport, output_dir: Path) -> Path: ...
```

### 2.4 Plugin Discovery

```python
# src/piranesi/plugin.py
from importlib.metadata import entry_points

def discover_framework_plugins() -> list[FrameworkPlugin]:
    eps = entry_points(group="piranesi.frameworks")
    return [ep.load()() for ep in eps]

def discover_rule_plugins() -> list[RulePlugin]:
    eps = entry_points(group="piranesi.rules")
    return [ep.load()() for ep in eps]
```

---

## 3. Built-in Plugin Migration

### 3.1 Express as Default Plugin

Express support is built-in (not a plugin) but uses the same interface:

```python
class ExpressFramework(FrameworkPlugin):
    def name(self) -> str: return "express"
    def detect(self, project_root: Path) -> bool:
        # check package.json for express dependency
    def source_specs(self) -> list[SourceSpec]:
        return list(BUILTIN_SOURCE_SPECS)
    # ...
```

### 3.2 NestJS/Next.js/Fastify as Bundled Plugins

Phase 10 framework support is refactored into the plugin interface but bundled with Piranesi (not separate packages). This proves the pattern before community plugins exist.

---

## 4. Plugin Security

### 4.1 Trust Model

Plugins execute arbitrary Python code. The trust model is the same as pip packages: the user trusts what they install.

### 4.2 Sandboxing

No sandboxing for plugins. They run in the same process as Piranesi. This is consistent with every other Python tool ecosystem (pytest plugins, Flask extensions, Django apps).

### 4.3 Plugin Validation

At load time, validate:
- Plugin implements the correct ABC interface
- Plugin name is unique (no collisions)
- Rule plugin TOML files parse correctly against `RegulatoryRuleSpec` schema

---

## 5. CLI Integration

```bash
# list installed plugins
piranesi plugins list

# show plugin details
piranesi plugins info piranesi-plugin-nestjs
```

Config:
```toml
# piranesi.toml
[plugins]
disabled = ["piranesi-plugin-nestjs"]  # disable specific plugins
```

---

## 6. Acceptance Criteria

- [ ] Plugin ABC interfaces defined for frameworks, rules, reporters
- [ ] Entry point discovery works for all three plugin types
- [ ] Express, NestJS, Next.js, Fastify refactored to plugin interface
- [ ] At least one external-package plugin demonstrated (e.g., `piranesi-plugin-example`)
- [ ] `piranesi plugins list` CLI command works
- [ ] Plugin disable via config works
- [ ] Documentation: `docs/plugin-development.md`
