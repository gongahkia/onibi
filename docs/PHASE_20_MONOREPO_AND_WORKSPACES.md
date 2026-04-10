# Phase 20: Monorepo + Workspace Scanning

**Estimated effort: 35-45 ideal hours**
**Blocked by: Phase 14 (incremental scanning)**
**Blocks: Nothing (independent feature)**

## 1. Motivation

Modern codebases are overwhelmingly monorepos. npm workspaces, Yarn workspaces, pnpm, Turborepo, Nx, Lerna, Go multi-module, Maven multi-module, and Gradle multi-project are all common. A scanner that forces users to point at individual packages misses cross-package taint flows and creates friction in CI.

Piranesi must detect monorepo structure, scan packages in dependency order, merge findings, and surface cross-package vulnerabilities — all while keeping scan time proportional to what changed.

## 2. Monorepo Detection

### 2.1 Supported Tools

| Tool | Detection signal | Package list source |
|------|-----------------|---------------------|
| npm workspaces | `package.json` → `workspaces` array/object | glob resolution |
| Yarn workspaces | `package.json` → `workspaces` | glob resolution |
| pnpm | `pnpm-workspace.yaml` → `packages` array | glob resolution |
| Turborepo | `turbo.json` exists | delegates to npm/yarn/pnpm |
| Nx | `nx.json` exists | `workspace.json` or `project.json` per package |
| Lerna | `lerna.json` → `packages` array | glob resolution |
| Go multi-module | `go.work` → `use` directives | directory list |
| Maven multi-module | parent `pom.xml` → `<modules>` | module directories |
| Gradle multi-project | `settings.gradle(.kts)` → `include` statements | project paths |

### 2.2 Package Dependency Graph

For each workspace package:
1. Parse `package.json` `dependencies` + `devDependencies` for npm/Yarn/pnpm.
2. Match dependency names against other workspace package names.
3. Build a DAG (directed acyclic graph) of internal dependencies.
4. Topological sort for scan ordering.

For Go: parse `go.work` `use` directives + `go.mod` `require` for internal modules.
For Maven/Gradle: parse `<dependency>` / `implementation` references to sibling module artifactIds.

### 2.3 Data Model

```python
@dataclass
class WorkspacePackage:
    name: str
    path: Path
    language: str # detected via framework.py
    internal_deps: list[str] # names of other workspace packages
    frameworks: tuple[str, ...]

@dataclass
class MonorepoManifest:
    root_path: Path
    tool: str # "npm-workspaces", "pnpm", "go-work", etc.
    packages: list[WorkspacePackage]
    dependency_edges: list[tuple[str, str]] # (from_name, to_name)
```

Implement in `src/piranesi/scan/monorepo.py`.

## 3. Per-Package Parallel Scanning

### 3.1 Scan Strategy

1. Detect monorepo → build `MonorepoManifest`.
2. Topological sort packages by dependency graph.
3. For independent packages (no internal deps on each other), scan in parallel using `ProcessPoolExecutor`.
4. For dependent packages, respect ordering: scan dependencies before dependents.
5. Share Joern server across packages in the same language (one CPG import per package, not one Joern instance per package).

### 3.2 Finding Merge

After scanning all packages:
1. Deduplicate findings with identical fingerprints (can happen if shared-lib code is analyzed via multiple dependents).
2. Create cross-package findings: if package A exports a function with a sink, and package B calls it with tainted input, create a finding spanning both.
3. Group findings by package in the report output.

### 3.3 CLI Flags

```
piranesi run <monorepo-root>                  # scan all packages
piranesi run <monorepo-root> --package api    # scan single package
piranesi run <monorepo-root> --changed-packages  # git-diff-based
piranesi run <monorepo-root> --max-parallel 4 # limit parallelism
```

### 3.4 Incremental + Monorepo

When `--incremental --changed-packages`:
1. Run `git diff --name-only <baseline>..HEAD` to get changed files.
2. Map changed files to packages (by path prefix).
3. Also include packages that depend on changed packages (transitive).
4. Scan only affected packages.

## 4. Cross-Package Taint Flows

### 4.1 Detection Strategy

1. For each package, compute exported taint summaries: which exported functions have paths from parameters to sinks.
2. When scanning a dependent package, load the dependency's taint summaries.
3. If the dependent calls an exported function with tainted input, and the summary shows a path to a sink, create a cross-package finding.

### 4.2 Finding Format

Cross-package findings have a `cross_package: true` flag and a taint path that spans multiple package directories. The path includes an "internal dependency call" step.

## 5. Tests

1. Fixture: 3-package npm workspace (`@test/shared-lib`, `@test/api`, `@test/frontend`).
   - `shared-lib` exports a `runQuery(input)` function with a SQL sink.
   - `api` imports `shared-lib` and passes `req.body.id` to `runQuery`.
   - `frontend` is independent.
2. Verify: per-package scan finds sink in shared-lib, cross-package finding links api→shared-lib.
3. Verify: `--package frontend` scans only frontend.
4. Verify: `--changed-packages` with only frontend changed skips api and shared-lib.
5. Verify: parallel scan produces same findings as sequential.

## 6. Risks

- **Joern memory**: large monorepos may exceed JVM heap. Mitigation: scan packages independently, each with its own CPG.
- **Cross-package false positives**: taint summaries are conservative (over-approximate). May need LLM triage to filter.
- **Mixed-language monorepos**: e.g., TS frontend + Go backend in one repo. Must handle multiple Joern frontends.
