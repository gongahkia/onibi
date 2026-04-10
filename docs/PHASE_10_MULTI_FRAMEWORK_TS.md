# Phase 10: Multi-Framework TypeScript/JavaScript Support

**Estimated effort: 40-60 ideal hours (shallow pass across 3 frameworks)**
**Blocked by: Phase 8 (clean Express baseline)**
**Blocks: Phase 14 (deep framework support)**
**Target milestone: v0.3.0**

---

## 1. Phase Overview

Piranesi currently supports Express-pattern taint analysis. The TypeScript ecosystem has diversified significantly: NestJS dominates enterprise backends, Next.js API routes are the most common serverless pattern, and Fastify is the performance-focused alternative.

This phase adds shallow support for NestJS, Next.js, and Fastify. "Shallow" means: detect framework-specific sources and sinks, map them to existing CWE categories, and extract taint flows. Deep framework-specific analysis (decorators, middleware chains, DI containers) is deferred to Phase 14.

---

## 2. Framework Detection

**Estimated effort: 3-4h**

Implement `src/piranesi/scan/framework.py`:

### 2.1 Detection Heuristics

```python
def detect_framework(project_root: Path) -> list[str]:
    """Return list of detected frameworks in priority order."""
```

| Signal | Framework |
|--------|-----------|
| `@nestjs/core` in `package.json` dependencies | NestJS |
| `next` in dependencies + `next.config.*` exists | Next.js |
| `fastify` in dependencies | Fastify |
| `express` in dependencies | Express |
| `koa` in dependencies | Koa |
| None detected | Generic Node.js |

Multiple frameworks can coexist (e.g., NestJS uses Express under the hood).

### 2.2 Framework-Specific Spec Selection

The detected framework(s) determine which source/sink specs are loaded. Each framework adds its own specs ON TOP of the base Express specs (since NestJS/Fastify can wrap Express).

---

## 3. NestJS Support

**Estimated effort: 12-15h**

### 3.1 NestJS Source Patterns

NestJS uses decorators for request data:

| Decorator | Express Equivalent | Source Type |
|-----------|-------------------|-------------|
| `@Body()` | `req.body` | request_body |
| `@Param('id')` | `req.params.id` | request_param |
| `@Query('q')` | `req.query.q` | url_param |
| `@Headers('auth')` | `req.headers.auth` | header |
| `@Req()` | `req` (entire request) | request_body |
| `@UploadedFile()` | `req.file` | request_body |

### 3.2 NestJS Sink Patterns

NestJS uses TypeORM/Prisma/Sequelize under the hood. Sinks are the same as Express but accessed through:

| Pattern | Sink Type |
|---------|-----------|
| `repository.query(raw)` | sql_query |
| `entityManager.query(raw)` | sql_query |
| `@Injectable() service` method passing to `exec()` | shell_exec |
| `res.send()` via `@Res()` decorator | html_output |

### 3.3 NestJS Transpilation

NestJS uses decorators extensively. Ensure `tsc` transpiles with `experimentalDecorators: true` and `emitDecoratorMetadata: true` in the Piranesi-generated tsconfig.

### 3.4 CPGQL Queries

Add NestJS-specific CPGQL query patterns to `scan/queries.py`:
- Match decorator parameters as sources: `cpg.method.parameter.annotation.name("Body").l`
- Track taint through service injection: controller method → injected service → sink

---

## 4. Next.js API Routes Support

**Estimated effort: 10-12h**

### 4.1 Next.js Source Patterns

Next.js API routes have two patterns:

**Pages Router** (`pages/api/*.ts`):
```typescript
export default function handler(req: NextApiRequest, res: NextApiResponse) {
  const data = req.body;  // source
}
```

**App Router** (`app/api/*/route.ts`):
```typescript
export async function POST(request: Request) {
  const body = await request.json();  // source
}
```

| Pattern | Source Type |
|---------|-------------|
| `req.body` (Pages Router) | request_body |
| `req.query` (Pages Router) | url_param |
| `request.json()` (App Router) | request_body |
| `request.text()` (App Router) | request_body |
| `request.formData()` (App Router) | request_body |
| `request.headers` (App Router) | header |
| `NextRequest.nextUrl.searchParams` | url_param |

### 4.2 Next.js Route Discovery

Detect API routes by file path convention:
- `pages/api/**/*.{ts,js}` — Pages Router
- `app/**/route.{ts,js}` — App Router
- `app/**/actions.{ts,js}` — Server Actions

### 4.3 Server Actions

Next.js 14+ Server Actions are form handlers that receive `FormData` directly:
```typescript
'use server';
export async function submitForm(formData: FormData) {
  const name = formData.get('name');  // source
  await db.query(`INSERT INTO users (name) VALUES ('${name}')`);  // sink!
}
```

These are sources. Add `FormData.get()` as a source pattern.

---

## 5. Fastify Support

**Estimated effort: 10-12h**

### 5.1 Fastify Source Patterns

| Pattern | Source Type |
|---------|-------------|
| `request.body` | request_body |
| `request.params` | request_param |
| `request.query` | url_param |
| `request.headers` | header |

### 5.2 Fastify Sink Patterns

Fastify uses `reply.send()` instead of `res.send()`:

| Pattern | Sink Type |
|---------|-----------|
| `reply.send(userInput)` | html_output |
| `reply.header('Location', userInput)` | header_injection |

### 5.3 Fastify Plugins

Fastify's plugin system (`fastify.register()`) scopes request/reply decorators. Taint through plugins requires tracking `decorate()` calls. This is deferred to Phase 14 (deep support).

### 5.4 Fastify Schema Validation

Fastify has built-in JSON Schema validation. If a route defines a schema, inputs are validated before the handler runs. This is a sanitizer:
```typescript
fastify.post('/user', {
  schema: { body: { type: 'object', properties: { name: { type: 'string', maxLength: 50 } } } }
}, handler);
```

Detect schema presence and reduce confidence for flows from schema-validated inputs.

---

## 6. Integration

### 6.1 Spec Registry

Refactor `scan/specs.py` to support framework-specific spec sets:

```python
def get_source_specs(frameworks: list[str] | None = None) -> tuple[SourceSpec, ...]:
    specs = list(BUILTIN_SOURCE_SPECS)  # Express base
    if frameworks:
        if "nestjs" in frameworks:
            specs.extend(NESTJS_SOURCE_SPECS)
        if "nextjs" in frameworks:
            specs.extend(NEXTJS_SOURCE_SPECS)
        if "fastify" in frameworks:
            specs.extend(FASTIFY_SOURCE_SPECS)
    return tuple(specs)
```

### 6.2 Config

Add `piranesi.toml` option:
```toml
[scan]
frameworks = ["auto"]  # auto-detect, or explicit: ["nestjs", "express"]
```

---

## 7. Ground Truth

Add 5+ ground truth entries per framework (15+ total):
- NestJS: TypeORM raw query injection, decorator-sourced XSS, service-layer CMDi
- Next.js: Server Action SQLi, API route path traversal, SSR XSS
- Fastify: Schema bypass injection, reply.send XSS, plugin-scoped taint

---

## 8. Acceptance Criteria

- [ ] Framework auto-detection works for NestJS, Next.js, Fastify
- [ ] NestJS decorator sources detected (Body, Param, Query, Headers)
- [ ] Next.js Pages Router and App Router sources detected
- [ ] Next.js Server Actions detected as sources
- [ ] Fastify request/reply patterns detected
- [ ] Fastify schema validation recognized as sanitizer
- [ ] 15+ framework-specific ground truth entries
- [ ] No regression on Express test suite
