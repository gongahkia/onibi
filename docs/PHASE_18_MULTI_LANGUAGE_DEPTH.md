# Phase 18: Multi-Language Joern Frontend Integration (Depth)

**Estimated effort: 40-50 ideal hours**
**Blocked by: Phase 13 (shallow specs), Phase 12 (plugin system)**
**Blocks: Nothing**
**Target milestone: v1.0**

---

## 1. Phase Overview

Phase 13 added source/sink specs for Python, Go, and Java. The specs define WHAT to look for but the pipeline still only runs `jssrc2cpg` (TypeScript/JavaScript via tsc). This phase wires the actual Joern frontends (`pysrc2cpg`, `gosrc2cpg`, `javasrc2cpg`) into the pipeline so Piranesi can analyze non-JS/TS code end-to-end.

---

## 2. Language-Aware Pipeline

**Estimated effort: 10-12h**

### 2.1 Frontend Selection

Update `src/piranesi/scan/joern.py`:

```python
def import_project(
    self,
    project_dir: Path,
    language: str = "javascript",
) -> None:
    frontend = LANGUAGE_TO_JOERN_FRONTEND[language]
    # Use appropriate Joern frontend
```

Currently `import_project` always uses `jssrc2cpg`. Wire it to select the frontend based on detected language.

### 2.2 Skip Transpilation for Non-TS

Update `src/piranesi/pipeline.py` scan stage:
- If language is Python/Go/Java, skip the `tsc` transpilation step entirely
- Pass source directory directly to Joern with the appropriate frontend
- Source maps not needed (no transpilation = 1:1 line mapping)

### 2.3 Multi-Language Projects

For projects with multiple languages (e.g., Next.js frontend + Python API):
1. Detect all languages via `scan/framework.py`
2. Run Joern with each frontend sequentially
3. Merge findings from all languages into a single `DetectArtifact`
4. Each finding tagged with its source language

---

## 3. Python Pipeline

**Estimated effort: 8-10h**

### 3.1 pysrc2cpg Integration

- Verify `pysrc2cpg` is available (bundled with Joern)
- Configure: `joern --frontend pysrc2cpg --input {project_dir}`
- Handle virtual environments: exclude `venv/`, `.venv/`, `site-packages/`
- Handle import resolution: pass `--exclude-regex` for test files

### 3.2 CPGQL Query Adaptation

Python CPGQL queries differ from JS:
- Method calls: `cpg.call.name("execute").argument(1)` (cursor.execute)
- Decorators: `cpg.method.annotation.name("route")`
- Module imports: `cpg.imports.importedEntity`

Update `scan/queries.py` with Python-specific CPGQL patterns.

### 3.3 Django ORM Awareness

- Detect `Model.objects.raw()` as SQL sink
- Detect `Model.objects.extra()` as SQL sink
- Recognize `Model.objects.filter()` as SAFE (parameterized)
- Recognize `F()` and `Q()` expressions as SAFE

---

## 4. Go Pipeline

**Estimated effort: 8-10h**

### 4.1 gosrc2cpg Integration

- No transpilation needed
- Handle Go modules: resolve imports via `go.mod`
- Exclude vendor/ if using vendored dependencies

### 4.2 Go-Specific CPGQL Patterns

- Function calls: `fmt.Sprintf` → check if used in SQL/exec context
- Error handling: Go's `if err != nil` pattern doesn't affect taint
- Goroutines: taint through `go func()` closures (limited by Joern)
- Interface dispatch: Joern may miss taint through interface method calls (soundness hole)

---

## 5. Java Pipeline

**Estimated effort: 8-10h**

### 5.1 javasrc2cpg Integration

- No transpilation needed (Joern parses `.java` directly)
- Handle Maven/Gradle: resolve classpath from `pom.xml`/`build.gradle`
- Exclude test sources (`src/test/`)

### 5.2 Spring-Specific CPGQL Patterns

- Annotation-based sources: `@RequestBody`, `@RequestParam`, `@PathVariable`
- Spring Security: detect `@PreAuthorize`, `@Secured` as access control (not a sink)
- JPA: `@Query(nativeQuery=true)` with string concatenation is a sink; `@Query` with `?1` parameters is safe

---

## 6. Cross-Language Taint Tracking

**Estimated effort: 6-8h**

### 6.1 API Boundary Detection

Detect where one language calls another:
- TypeScript `fetch('/api/endpoint')` → Python Flask route `/api/endpoint`
- Match by route pattern (URL path matching)

### 6.2 Cross-Language Finding

If TypeScript sends tainted data to a URL that matches a Python route with a known sink, create a cross-language finding:

```
Source: req.body.name (TypeScript, src/frontend/form.tsx:15)
  → fetch('/api/users', {body: JSON.stringify({name})})
  → Flask route /api/users (Python, src/backend/routes.py:23)
  → cursor.execute(f"INSERT INTO users (name) VALUES ('{name}')")
Sink: cursor.execute (Python, src/backend/routes.py:25)
```

This requires matching URL patterns between frontend fetch calls and backend route definitions.

---

## 7. Acceptance Criteria

- [ ] `pysrc2cpg` wired into pipeline — Python projects analyzed end-to-end
- [ ] `gosrc2cpg` wired into pipeline — Go projects analyzed end-to-end
- [ ] `javasrc2cpg` wired into pipeline — Java projects analyzed end-to-end
- [ ] Multi-language projects: all languages scanned in single run
- [ ] Transpilation skipped for non-TS/JS languages
- [ ] Python: Django ORM-aware (raw/extra = sink, filter/F/Q = safe)
- [ ] Go: fmt.Sprintf-in-SQL detection
- [ ] Java: Spring annotation sources, JPA native query sinks
- [ ] 5+ cross-language findings in ground truth
- [ ] No regression on TS/JS test suite
