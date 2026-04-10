# Phase 26: Web Vulnerability — Auth & Access Control

**Estimated effort: 70-90 ideal hours**
**Blocked by: Phase 16 (OWASP coverage), Phase 22 (advanced taint analysis)**
**Blocks: Nothing (incremental value)**
**Target milestone: v0.6.0**

---

## 1. Overview & Gap Analysis

### 1.1 Current State

Piranesi detects 12 CWE classes across its source/sink/sanitizer spec system:

| CWE | Name | OWASP Category |
|-----|------|----------------|
| CWE-22 | Path Traversal | A01 (partial) |
| CWE-77 | Command Injection | A03 |
| CWE-78 | OS Command Injection | A03 |
| CWE-79 | XSS | A03 |
| CWE-89 | SQL Injection | A03 |
| CWE-94 | Code Injection | A03 |
| CWE-434 | Unrestricted File Upload | A04 |
| CWE-444 | HTTP Request Smuggling | A05 |
| CWE-502 | Deserialization | A08 |
| CWE-601 | Open Redirect | A01 (partial) |
| CWE-611 | XXE | A05 |
| CWE-918 | SSRF | A10 |

Plus misconfiguration-class detections: CWE-942 (CORS), CWE-1021 (Clickjacking), CWE-693 (CSP), CWE-319 (HSTS), CWE-614/1004 (Cookie flags), CWE-798 (Hardcoded secrets), CWE-1321 (Prototype pollution).

### 1.2 OWASP Categories Underserved

**A01 — Broken Access Control** is the #1 OWASP category. Piranesi covers path traversal and open redirect but lacks:
- CSRF (CWE-352)
- IDOR (CWE-639)
- Privilege escalation (CWE-269)
- Missing auth on critical functions (CWE-306)

**A07 — Identification and Authentication Failures** is #7. Piranesi covers hardcoded secrets (CWE-798) but lacks:
- Broken authentication patterns (CWE-287)
- Session fixation (CWE-384)
- Mass assignment (CWE-915)

### 1.3 New CWE Targets

| CWE | Name | OWASP | Detection Approach | Expected FP Rate |
|-----|------|-------|--------------------|------------------|
| CWE-352 | Cross-Site Request Forgery | A01 | Absence-of-pattern (middleware chain) | Low |
| CWE-639 | IDOR | A01 | Heuristic (param → DB without ownership) | High |
| CWE-269 | Privilege Escalation | A01 | Absence-of-pattern (admin route without auth) | Medium |
| CWE-287 | Broken Authentication | A07 | Pattern matching (JWT, password, session) | Medium |
| CWE-384 | Session Fixation | A07 | Absence-of-pattern (no regenerate after login) | Low |
| CWE-915 | Mass Assignment | A01 | Taint flow (req.body → ORM create) | Medium |
| CWE-306 | Missing Auth for Critical Function | A01 | Heuristic (sensitive route without auth) | Medium |

### 1.4 Architectural Approach

Auth/access findings are fundamentally different from injection findings. Injection is "user data reaches dangerous sink." Auth findings are "expected security control is absent." This requires **absence-of-pattern detection** — a new detection strategy alongside the existing taint flow analysis.

New module: `src/piranesi/detect/auth_access.py` — runs after taint analysis, operates on the CPG + parsed route/middleware metadata.

All findings from this phase default to confidence range 0.4-0.6 (lower than injection findings at 0.7) to reflect inherent heuristic uncertainty.

---

## 2. CSRF Detection (CWE-352)

**Estimated effort: 10-12h**

### 2.1 Threat Model

CSRF exploits the browser's automatic cookie attachment to forge state-changing requests. Any POST/PUT/DELETE/PATCH handler that relies on cookie-based authentication and lacks a CSRF token is vulnerable.

### 2.2 Detection Strategy

**Primary:** Absence-of-middleware detection. For each state-changing route handler (POST/PUT/DELETE/PATCH), check whether a CSRF protection middleware appears in the middleware chain before the handler executes.

**Secondary:** Explicit `csrf_exempt` / `@csrf_exempt` annotations flag intentional exclusions — emit a lower-confidence informational finding.

### 2.3 Source/Sink Specs

No traditional source→sink taint flow. Instead, this uses the middleware chain analysis model from `detect/misconfigurations.py`.

Add to `scan/specs.py` — new `SinkType` member:

```python
class SinkType(StrEnum):
    # ... existing ...
    STATE_CHANGE_HANDLER = "state_change_handler"  # POST/PUT/DELETE/PATCH route
    ORM_WRITE = "orm_write"  # ORM create/update with user input
    AUTH_SENSITIVE = "auth_sensitive"  # auth-related endpoint
```

### 2.4 Framework-Specific Patterns

#### Express / csurf

**Vulnerable pattern:**
```javascript
const app = express();
// no csurf middleware
app.post('/transfer', (req, res) => {
    transferFunds(req.body.to, req.body.amount); // CWE-352
});
```

**Detection CPGQL:**
```
// find all POST/PUT/DELETE/PATCH route registrations
cpg.call.name("post|put|delete|patch").filter(c =>
    !c.inAst.isCall.name("csrf|csurf|csrfProtection|lusca").nonEmpty
)
```

**Regex fallback (for `detect/auth_access.py` file-level scan):**
```python
_EXPRESS_STATE_CHANGE_ROUTE = re.compile(
    r'\b(?:app|router)\s*\.\s*(?:post|put|delete|patch)\s*\('
)
_CSRF_MIDDLEWARE_PRESENT = re.compile(
    r'(?:csurf|csrf|csrfProtection|lusca\.csrf)\s*\('
)
_CSRF_HELMET_CSRF = re.compile(r'helmet\.csrf\s*\(')
```

**Logic:**
1. Scan file for `_EXPRESS_STATE_CHANGE_ROUTE` matches.
2. Check if `_CSRF_MIDDLEWARE_PRESENT` or `_CSRF_HELMET_CSRF` appears anywhere in the file or in imported middleware files.
3. If no CSRF middleware found → emit CWE-352 finding at each state-changing route.

**Sanitizers:**
```python
SanitizerSpec(
    name="csurf_middleware",
    pattern='cpg.call.name("csrf|csurf|csrfProtection")',
    kind=SanitizerKind.NORMALIZE,
    mitigates=("CWE-352",),
    confidence=0.9,
)
```

#### NestJS / @Guard + CSRF

**Vulnerable pattern:**
```typescript
@Controller('transfer')
export class TransferController {
    @Post()
    // no @UseGuards(CsrfGuard) or global CSRF protection
    create(@Body() dto: TransferDto) { ... }
}
```

**Detection:** Check NestJS controller methods decorated with `@Post()`, `@Put()`, `@Delete()`, `@Patch()`. Look for absence of `@UseGuards(CsrfGuard)` on method or class level, and absence of global `app.use(csurf())` in bootstrap.

**CPGQL:**
```
cpg.call.name("__decorate").filter(d =>
    d.code.contains("Post(") || d.code.contains("Put(") ||
    d.code.contains("Delete(") || d.code.contains("Patch(")
).filter(d =>
    !d.inAst.isCall.code(".*UseGuards.*Csrf.*").nonEmpty
)
```

#### Django / csrf_exempt

**Vulnerable pattern:**
```python
@csrf_exempt
def transfer_funds(request):
    # processes POST without CSRF token
    amount = request.POST['amount']
```

**Detection:** Django has CSRF enabled by default via `CsrfViewMiddleware`. Detect:
1. `@csrf_exempt` decorator on views → emit finding (intentional bypass).
2. `django.middleware.csrf.CsrfViewMiddleware` removed from `MIDDLEWARE` → emit finding (global disable).

**Regex patterns:**
```python
_DJANGO_CSRF_EXEMPT = re.compile(r'@csrf_exempt')
_DJANGO_CSRF_MIDDLEWARE_ABSENT = re.compile(
    r'MIDDLEWARE\s*=\s*\[(?:(?!CsrfViewMiddleware).)*\]', re.DOTALL
)
```

#### Flask / WTForms

**Vulnerable pattern:**
```python
@app.route('/transfer', methods=['POST'])
def transfer():
    amount = request.form['amount']  # no CSRFProtect
```

**Detection:** Check for `CSRFProtect(app)` or `csrf.init_app(app)` in the Flask app setup. If absent and POST routes exist → emit finding.

**Regex:**
```python
_FLASK_CSRF_PROTECT = re.compile(r'(?:CSRFProtect|csrf\.init_app)\s*\(')
_FLASK_POST_ROUTE = re.compile(r"@\w+\.route\(.*methods\s*=\s*\[.*'POST'.*\]")
```

#### Spring / CsrfFilter

**Vulnerable pattern:**
```java
@Configuration
public class SecurityConfig extends WebSecurityConfigurerAdapter {
    @Override
    protected void configure(HttpSecurity http) throws Exception {
        http.csrf().disable(); // CWE-352
    }
}
```

**Detection:**
```python
_SPRING_CSRF_DISABLE = re.compile(r'\.csrf\(\)\s*\.\s*disable\(\)')
```

Also detect `http.csrf(csrf -> csrf.disable())` (lambda style in Spring Security 6+):
```python
_SPRING_CSRF_DISABLE_LAMBDA = re.compile(r'\.csrf\(\s*\w+\s*->\s*\w+\.disable\(\)\s*\)')
```

### 2.5 Confidence Calibration

| Scenario | Confidence |
|----------|-----------|
| Express POST handler, no CSRF middleware in entire app | 0.6 |
| Django `@csrf_exempt` on view | 0.7 (intentional but risky) |
| Flask POST route, no CSRFProtect | 0.55 |
| Spring `csrf().disable()` | 0.7 |
| NestJS POST without guard, but global guard may exist | 0.4 |

### 2.6 Severity

Default severity: **medium**. Upgrade to **high** if the handler performs financial operations (keyword match: `transfer`, `payment`, `purchase`, `withdraw`, `deposit`).

Add to `_SEVERITY_BY_CWE` in `detect/flows.py`:
```python
"CWE-352": "medium",
```

---

## 3. IDOR Detection (CWE-639)

**Estimated effort: 12-15h**

### 3.1 Threat Model

Insecure Direct Object Reference: a user-controlled identifier (route param, query param) is used to fetch/modify a resource without verifying the requesting user owns that resource.

### 3.2 Detection Strategy

**Heuristic taint flow:** Route parameter → database query without an ownership predicate in the WHERE clause.

This is a **taint flow with negative condition** — the finding triggers when taint reaches a sink AND an expected condition (ownership check) is absent.

### 3.3 Framework-Specific Patterns

#### Express

**Vulnerable pattern:**
```javascript
app.get('/api/orders/:id', async (req, res) => {
    const order = await db.query('SELECT * FROM orders WHERE id = $1', [req.params.id]);
    // missing: WHERE user_id = req.session.userId
    res.json(order);
});
```

**Detection CPGQL:**
```
// Step 1: Find route params flowing to DB queries
cpg.call.name("query|findOne|findById|findByPk|findUnique")
    .where(_.argument.reachableBy(
        cpg.call.name("<operator>.fieldAccess").code("req[.]params.*")
    ))

// Step 2: Check if the same query also references session/user context
// If NOT → IDOR finding
.filter(c =>
    !c.argument.code(".*(?:session|user_id|userId|currentUser|req\\.user).*").nonEmpty
)
```

**Regex fallback in `detect/auth_access.py`:**
```python
_ROUTE_PARAM_TO_DB = re.compile(
    r'(?:params|param)\s*[\.\[]\s*[\'"]?(?:id|Id|ID|uuid)\b'
)
_OWNERSHIP_CHECK = re.compile(
    r'(?:session\.user|req\.user|currentUser|userId|user_id|owner_id|ownerId)'
)
```

**Logic:**
1. Find handler functions containing `_ROUTE_PARAM_TO_DB`.
2. Within the same function scope, check for `_OWNERSHIP_CHECK`.
3. If absent → emit CWE-639 finding.

#### Django

**Vulnerable pattern:**
```python
def order_detail(request, pk):
    order = Order.objects.get(pk=pk)  # no filter by request.user
    return JsonResponse(model_to_dict(order))
```

**Detection:**
```python
_DJANGO_PK_LOOKUP = re.compile(
    r'\.(?:get|filter)\(\s*(?:pk|id)\s*='
)
_DJANGO_USER_FILTER = re.compile(
    r'(?:request\.user|user=request\.user|owner=request\.user|author=request\.user)'
)
```

#### Spring

**Vulnerable pattern:**
```java
@GetMapping("/orders/{id}")
public Order getOrder(@PathVariable Long id) {
    return orderRepository.findById(id).orElseThrow(); // no ownership check
}
```

**Detection CPGQL:**
```
cpg.method.where(_.parameter.annotation.name("PathVariable"))
    .ast.isCall.name("findById|findOne|getById|getReferenceById")
    .filter(c =>
        !c.method.ast.isCall.code(".*(?:SecurityContext|getPrincipal|getAuthentication).*").nonEmpty
    )
```

### 3.4 Ownership Check Heuristics

The following patterns count as "ownership verified" and suppress the finding:

| Pattern | Framework | Confidence Reduction |
|---------|-----------|---------------------|
| `WHERE user_id = ?` with session-derived value | All | Full suppress |
| `request.user` in same queryset filter chain | Django | Full suppress |
| `@PreAuthorize("@service.isOwner()")` | Spring | Full suppress |
| `if (order.userId !== req.user.id)` comparison | Express | Full suppress |
| `req.user` referenced anywhere in handler | Express | Reduce to 0.3 |
| Custom `@Authorize` decorator present | NestJS | Reduce to 0.3 |

### 3.5 Confidence Calibration

Base confidence: **0.45** (high FP risk).

Boost to 0.6 if:
- Route matches CRUD pattern (`/api/:resource/:id`)
- Handler uses `findById`/`findOne` (single-record fetch)
- No session/user reference in entire handler

Reduce to 0.3 if:
- Handler references `req.user` or `session` anywhere (may have ownership check we missed)
- Custom middleware on the route (may enforce auth at middleware level)

### 3.6 Severity

Default: **high** (data exfiltration / unauthorized modification).

```python
"CWE-639": "high",
```

---

## 4. Broken Authentication (CWE-287)

**Estimated effort: 12-15h**

### 4.1 Threat Model

Broken authentication encompasses: weak password comparison, JWT misuse, insecure session configuration, and missing multi-factor enforcement on sensitive operations.

### 4.2 Sub-Patterns

#### 4.2.1 Password Comparison Without Timing-Safe Equality

**Vulnerable pattern:**
```javascript
if (user.password === providedPassword) { // timing oracle
    // ...
}
```

**Safe pattern:**
```javascript
const crypto = require('crypto');
if (crypto.timingSafeEqual(Buffer.from(user.password), Buffer.from(hash))) { ... }
```

**Detection regex:**
```python
_TIMING_UNSAFE_PASSWORD_COMPARE = re.compile(
    r'(?:password|passwd|secret|token|apiKey|api_key)\s*(?:===?|!==?|==)\s*'
)
_TIMING_SAFE_COMPARE = re.compile(
    r'(?:timingSafeEqual|constantTimeCompare|hmac\.compare_digest|MessageDigest\.isEqual)'
)
```

**Logic:** Find `===`/`==` comparisons involving password-like variable names. Check if `timingSafeEqual` or equivalent is used instead. If not → emit CWE-287 with confidence 0.5.

**CPGQL:**
```
cpg.call.name("<operator>.equals|<operator>.notEquals")
    .where(_.argument.code(".*(?:password|passwd|secret|hash).*"))
    .filter(c => !c.method.ast.isCall.name("timingSafeEqual|compare_digest").nonEmpty)
```

#### 4.2.2 JWT Misconfiguration

**Pattern 1 — Algorithm None Attack:**
```javascript
jwt.verify(token, secret, { algorithms: ['none'] }); // CWE-287
jwt.decode(token); // no verification at all
```

**Detection:**
```python
_JWT_ALG_NONE = re.compile(r"algorithms\s*:\s*\[.*'none'.*\]", re.IGNORECASE)
_JWT_DECODE_NO_VERIFY = re.compile(
    r'jwt\.decode\s*\([^)]*\)\s*(?!.*verify)'  # decode without subsequent verify
)
```

**CPGQL:**
```
cpg.call.name("verify|decode").code(".*jwt[.](?:verify|decode)[(].*")
    .where(_.argument.code(".*none.*"))
```

**Pattern 2 — Symmetric Key in Source Code:**
```javascript
const SECRET = 'my-hardcoded-secret'; // CWE-287 + CWE-798
jwt.sign(payload, SECRET);
```

**Detection:** This overlaps with existing CWE-798 secret detection. Cross-reference: if a `jwt.sign` or `jwt.verify` call uses a literal string or a variable assigned from a literal string → emit CWE-287 finding.

**CPGQL:**
```
cpg.call.name("sign|verify").code(".*jwt[.](?:sign|verify)[(].*")
    .argument(2).isLiteral
```

**Pattern 3 — Missing Expiry Validation:**
```javascript
jwt.verify(token, secret); // no expiresIn, no maxAge
```

**Detection:** Check `jwt.sign` calls for absence of `expiresIn` in options, and `jwt.verify` calls for absence of `maxAge` or `exp` claim validation.

```python
_JWT_SIGN_NO_EXPIRY = re.compile(
    r'jwt\.sign\s*\([^)]*\)\s*(?!.*expiresIn)'
)
_JWT_VERIFY_NO_MAXAGE = re.compile(
    r'jwt\.verify\s*\([^)]*\)\s*(?!.*maxAge)'
)
```

**Pattern 4 — Missing Audience/Issuer Check:**
```javascript
jwt.verify(token, secret); // no audience or issuer check
```

**Detection:**
```python
_JWT_VERIFY_NO_AUDIENCE = re.compile(
    r'jwt\.verify\s*\([^{]*(?:\{[^}]*)?(?!.*audience)(?!.*aud)[^)]*\)'
)
```

Confidence: 0.4 (audience checks are not always required).

#### 4.2.3 Session Cookie Misconfiguration

**Vulnerable pattern:**
```javascript
app.use(session({
    cookie: {
        secure: false,      // CWE-614
        httpOnly: false,     // CWE-1004
        // missing sameSite  // CWE-352
    }
}));
```

**Detection:** This partially overlaps with Phase 16 misconfiguration detection (`detect/misconfigurations.py`). Extend to also check for:
- `sameSite` not set or set to `'none'` without `secure: true`
- Session ID transmitted in URL query parameter

```python
_SESSION_SAMESITE_NONE = re.compile(r'sameSite\s*:\s*[\'"]none[\'"]')
_SESSION_IN_URL = re.compile(r'(?:sid|session_id|sessionId)\s*=\s*req\.(?:query|params)')
```

#### 4.2.4 Passport.js Misconfiguration

**Vulnerable pattern:**
```javascript
passport.use(new LocalStrategy((username, password, done) => {
    if (password === user.password) { // plaintext comparison
        return done(null, user);
    }
}));
```

**Detection:** Within `LocalStrategy` callback, check for direct `===` password comparison without `bcrypt.compare` / `argon2.verify` / `scrypt`.

```python
_PASSPORT_LOCAL_STRATEGY = re.compile(
    r'new\s+LocalStrategy\s*\(', re.DOTALL
)
_BCRYPT_COMPARE = re.compile(
    r'(?:bcrypt|argon2|scrypt)\.(?:compare|verify)\s*\('
)
```

**Logic:** If `_PASSPORT_LOCAL_STRATEGY` found and `_BCRYPT_COMPARE` not found in same function scope → CWE-287, confidence 0.6.

#### 4.2.5 Django Auth Misuse

**Vulnerable pattern:**
```python
def login_view(request):
    user = User.objects.get(username=request.POST['username'])
    if user.password == request.POST['password']:  # no check_password()
        login(request, user)
```

**Detection:**
```python
_DJANGO_RAW_PASSWORD_CHECK = re.compile(
    r'\.password\s*==\s*request\.(?:POST|GET|body)'
)
_DJANGO_CHECK_PASSWORD = re.compile(r'\.check_password\s*\(')
_DJANGO_AUTHENTICATE = re.compile(r'\bauthenticate\s*\(')
```

**Logic:** If `_DJANGO_RAW_PASSWORD_CHECK` found without `_DJANGO_CHECK_PASSWORD` or `_DJANGO_AUTHENTICATE` in same scope → CWE-287, confidence 0.7.

### 4.3 Severity

```python
"CWE-287": "high",
```

JWT alg=none and plaintext password comparison: **critical**.
Missing expiry/audience: **medium**.

### 4.4 Sanitizers

```python
SanitizerSpec(
    name="bcrypt_compare",
    pattern='cpg.call.name("compare|compareSync").code(".*bcrypt[.]compare.*")',
    kind=SanitizerKind.NORMALIZE,
    mitigates=("CWE-287",),
    confidence=0.95,
),
SanitizerSpec(
    name="timing_safe_equal",
    pattern='cpg.call.name("timingSafeEqual")',
    kind=SanitizerKind.NORMALIZE,
    mitigates=("CWE-287",),
    confidence=0.95,
),
SanitizerSpec(
    name="argon2_verify",
    pattern='cpg.call.name("verify").code(".*argon2[.]verify.*")',
    kind=SanitizerKind.NORMALIZE,
    mitigates=("CWE-287",),
    confidence=0.95,
),
```

---

## 5. Session Fixation (CWE-384)

**Estimated effort: 6-8h**

### 5.1 Threat Model

An attacker sets a victim's session ID before authentication. If the application does not regenerate the session after login, the attacker can hijack the authenticated session.

### 5.2 Detection Strategy

**Absence-of-pattern:** Find login/authentication handlers, check that `session.regenerate()` (or equivalent) is called after successful authentication.

### 5.3 Framework-Specific Patterns

#### Express / express-session

**Vulnerable pattern:**
```javascript
app.post('/login', (req, res) => {
    const user = authenticate(req.body.username, req.body.password);
    if (user) {
        req.session.userId = user.id; // session NOT regenerated
        res.redirect('/dashboard');
    }
});
```

**Safe pattern:**
```javascript
app.post('/login', (req, res) => {
    const user = authenticate(req.body.username, req.body.password);
    if (user) {
        req.session.regenerate((err) => {
            req.session.userId = user.id;
            res.redirect('/dashboard');
        });
    }
});
```

**Detection regex:**
```python
_LOGIN_HANDLER_PATTERN = re.compile(
    r'(?:app|router)\s*\.\s*post\s*\(\s*[\'"](?:/login|/signin|/authenticate|/auth/login)[\'"]'
)
_SESSION_ASSIGNMENT = re.compile(
    r'(?:req\.session\.\w+\s*=|session\.\w+\s*=)'
)
_SESSION_REGENERATE = re.compile(
    r'(?:req\.session\.regenerate|session\.regenerate)\s*\('
)
```

**Logic:**
1. Find login handlers via `_LOGIN_HANDLER_PATTERN` or heuristic (POST handler containing `authenticate`/`login` calls).
2. Within handler scope, check for `_SESSION_ASSIGNMENT` (session mutation after auth).
3. If session is mutated but `_SESSION_REGENERATE` is absent → CWE-384, confidence 0.55.

**CPGQL:**
```
cpg.call.name("post").filter(c =>
    c.argument(1).code.matches(".*(?:login|signin|auth).*")
).argument.isMethodRef.referencedMethod.ast.isCall
    .name("<operator>.fieldAccess").code(".*session\\..*")
    .filter(s => !s.method.ast.isCall.name("regenerate").nonEmpty)
```

#### Flask

**Vulnerable pattern:**
```python
@app.route('/login', methods=['POST'])
def login():
    user = authenticate(request.form['username'], request.form['password'])
    if user:
        session['user_id'] = user.id  # no session rotation
        return redirect('/dashboard')
```

**Detection:** Flask does not have built-in `session.regenerate()`. The recommended pattern is to clear and recreate:
```python
session.clear()
session['user_id'] = user.id
```

**Regex:**
```python
_FLASK_SESSION_CLEAR = re.compile(r'session\.clear\s*\(')
```

If `session['user_id'] = ...` appears in a login handler without prior `session.clear()` → CWE-384, confidence 0.5.

#### Django

Django's `login()` function calls `request.session.cycle_key()` internally, so direct use of `django.contrib.auth.login()` is safe. Detect manual login implementations that skip this:

```python
_DJANGO_AUTH_LOGIN = re.compile(r'\blogin\s*\(\s*request')
_DJANGO_MANUAL_SESSION_SET = re.compile(
    r'request\.session\[\s*[\'"](?:user_id|_auth_user_id)[\'"]'
)
_DJANGO_SESSION_CYCLE = re.compile(r'session\.cycle_key\s*\(')
```

If `_DJANGO_MANUAL_SESSION_SET` found without `_DJANGO_SESSION_CYCLE` and without `_DJANGO_AUTH_LOGIN` → CWE-384, confidence 0.6.

#### Spring

Spring Security handles session fixation via `sessionManagement().sessionFixation().migrateSession()` (default). Detect when explicitly disabled:

```python
_SPRING_SESSION_FIXATION_NONE = re.compile(
    r'sessionFixation\(\)\s*\.\s*none\(\)'
)
```

### 5.4 Severity & Confidence

```python
"CWE-384": "medium",
```

| Scenario | Confidence |
|----------|-----------|
| Express login handler, session assigned, no regenerate | 0.55 |
| Flask login handler, session set, no clear | 0.50 |
| Django manual session set without cycle_key | 0.60 |
| Spring sessionFixation().none() | 0.70 |

---

## 6. Mass Assignment (CWE-915)

**Estimated effort: 10-12h**

### 6.1 Threat Model

Mass assignment occurs when an application binds HTTP request data directly to a data model without filtering, allowing attackers to set fields they should not control (e.g., `isAdmin`, `role`, `price`).

### 6.2 Detection Strategy

**Taint flow with negative condition:** `req.body` (or equivalent) flows directly into an ORM create/update call without passing through an explicit field allowlist or DTO.

### 6.3 Framework-Specific Patterns

#### Express + Sequelize

**Vulnerable pattern:**
```javascript
app.post('/users', async (req, res) => {
    const user = await User.create(req.body); // CWE-915: all fields from request
    res.json(user);
});
```

**Safe pattern:**
```javascript
app.post('/users', async (req, res) => {
    const { name, email } = req.body; // explicit pick
    const user = await User.create({ name, email });
    res.json(user);
});
```

**Detection CPGQL:**
```
// Find ORM create/update calls where argument is directly req.body
cpg.call.name("create|update|build|upsert|insertMany")
    .where(_.argument.reachableBy(
        cpg.call.name("<operator>.fieldAccess").code("req[.]body")
    ))
    .filter(c =>
        // Check the argument is the raw req.body, not a destructured subset
        c.argument(1).code.matches(".*req\\.body.*") ||
        c.argument(1).code.matches(".*request\\.body.*")
    )
```

**Regex fallback:**
```python
_SEQUELIZE_MASS_ASSIGN = re.compile(
    r'(?:\.create|\.update|\.build|\.upsert|\.bulkCreate)\s*\(\s*'
    r'(?:req\.body|request\.body|ctx\.request\.body|ctx\.body)'
)
```

#### Express + Mongoose

**Vulnerable pattern:**
```javascript
const user = new User(req.body); // CWE-915
await user.save();
// or
await User.findByIdAndUpdate(id, req.body); // CWE-915
```

**Detection:**
```python
_MONGOOSE_MASS_ASSIGN = re.compile(
    r'(?:new\s+\w+\s*\(|findByIdAndUpdate\s*\([^,]*,\s*|'
    r'findOneAndUpdate\s*\([^,]*,\s*|updateOne\s*\([^,]*,\s*|'
    r'updateMany\s*\([^,]*,\s*)'
    r'(?:req\.body|request\.body)'
)
```

#### Express + Prisma

**Vulnerable pattern:**
```javascript
const user = await prisma.user.create({
    data: req.body // CWE-915
});
```

**Detection:**
```python
_PRISMA_MASS_ASSIGN = re.compile(
    r'prisma\.\w+\.(?:create|update|upsert)\s*\(\s*\{\s*data\s*:\s*'
    r'(?:req\.body|request\.body)'
)
```

**CPGQL:**
```
cpg.call.name("create|update|upsert").code(".*prisma[.].*")
    .where(_.argument.ast.isCall.name("<operator>.fieldAccess").code(".*[.]body"))
```

#### Django / ModelForm

**Vulnerable pattern:**
```python
class UserForm(ModelForm):
    class Meta:
        model = User
        fields = '__all__'  # CWE-915: includes is_staff, is_superuser
```

**Detection:**
```python
_DJANGO_FIELDS_ALL = re.compile(
    r"fields\s*=\s*['\"]__all__['\"]"
)
_DJANGO_EXCLUDE_PATTERN = re.compile(
    r"exclude\s*=\s*\["
)
```

Also detect direct `**request.POST` unpacking:
```python
_DJANGO_DIRECT_UNPACK = re.compile(
    r'(?:\.objects\.create|\.objects\.update_or_create|\.objects\.get_or_create)\s*\(\s*\*\*request\.(?:POST|data|body)'
)
```

#### Spring / @RequestBody without @JsonIgnore

**Vulnerable pattern:**
```java
@PostMapping("/users")
public User createUser(@RequestBody User user) { // CWE-915
    return userRepository.save(user);
}
```

**Safe pattern:**
```java
@PostMapping("/users")
public User createUser(@RequestBody @Valid CreateUserDto dto) {
    User user = new User();
    user.setName(dto.getName());
    // explicitly copy fields
    return userRepository.save(user);
}
```

**Detection CPGQL:**
```
// Find @RequestBody parameters whose type is the same as the entity saved
cpg.method.parameter.annotation.name("RequestBody").parameter
    .where(p =>
        p.method.ast.isCall.name("save|saveAndFlush|persist")
            .argument(1).typeFullName.matches(p.typeFullName)
    )
```

**Heuristic:** If `@RequestBody` parameter type is an `@Entity` class (not a DTO) → CWE-915, confidence 0.6.

### 6.4 Sanitizers (Allowlist Patterns)

The following suppress CWE-915 findings:

```python
SanitizerSpec(
    name="destructuring_allowlist",
    pattern='cpg.call.name("<operator>.destructuring")',
    kind=SanitizerKind.NORMALIZE,
    mitigates=("CWE-915",),
    confidence=0.8,
),
SanitizerSpec(
    name="lodash_pick",
    pattern='cpg.call.name("pick|omit").code(".*(?:_|lodash)[.](?:pick|omit)[(].*")',
    kind=SanitizerKind.NORMALIZE,
    mitigates=("CWE-915",),
    confidence=0.85,
),
SanitizerSpec(
    name="zod_parse_dto",
    pattern='cpg.call.name("parse|safeParse").code(".*(?:zod|z|schema)[.](?:parse|safeParse)[(].*")',
    kind=SanitizerKind.NORMALIZE,
    mitigates=("CWE-915",),
    confidence=0.9,
),
SanitizerSpec(
    name="class_transformer_plainToClass",
    pattern='cpg.call.name("plainToClass|plainToInstance")',
    kind=SanitizerKind.NORMALIZE,
    mitigates=("CWE-915",),
    confidence=0.85,
),
```

### 6.5 Confidence Calibration

Base confidence: **0.5**.

Boost to 0.65 if:
- The ORM model has sensitive fields (`isAdmin`, `role`, `permissions`, `is_staff`, `is_superuser`, `price`, `balance`)
- No DTO/validation layer detected between request and ORM call

Reduce to 0.3 if:
- A validation library (Joi, Zod, class-validator, Pydantic) is imported in the same file
- The handler uses destructuring before the ORM call

### 6.6 Sensitive Field Detection

To boost confidence, scan the ORM model definition for sensitive fields:

```python
_SENSITIVE_FIELDS = frozenset({
    'isAdmin', 'is_admin', 'isStaff', 'is_staff', 'isSuperuser', 'is_superuser',
    'role', 'roles', 'permission', 'permissions', 'admin',
    'price', 'balance', 'credit', 'amount',
    'verified', 'approved', 'active', 'deleted',
    'passwordHash', 'password_hash', 'password',
})
```

### 6.7 Severity

```python
"CWE-915": "high",
```

---

## 7. Privilege Escalation (CWE-269)

**Estimated effort: 8-10h**

### 7.1 Threat Model

Admin or privileged routes accessible without proper authorization checks. An authenticated but low-privilege user can access admin functionality.

### 7.2 Detection Strategy

**Absence-of-pattern:** Find routes matching admin/management URL patterns, verify that authorization middleware (role check, permission guard) exists in the middleware chain.

### 7.3 Admin Route Heuristics

```python
_ADMIN_ROUTE_PATTERNS = re.compile(
    r'[\'"](?:'
    r'/admin(?:/.*)?|'
    r'/api/v\d+/admin(?:/.*)?|'
    r'/api/v\d+/manage(?:/.*)?|'
    r'/manage(?:/.*)?|'
    r'/dashboard/admin(?:/.*)?|'
    r'/internal(?:/.*)?|'
    r'/superuser(?:/.*)?|'
    r'/settings/global(?:/.*)?'
    r')[\'"]'
)
```

### 7.4 Framework-Specific Patterns

#### Express

**Vulnerable pattern:**
```javascript
app.delete('/admin/users/:id', async (req, res) => {
    await User.destroy({ where: { id: req.params.id } }); // no role check
    res.sendStatus(204);
});
```

**Detection:** Check middleware chain for the route. Express middleware is positional:
```javascript
app.delete('/admin/users/:id', requireAdmin, handler); // safe
app.delete('/admin/users/:id', handler);                // vulnerable
```

**Regex:**
```python
_EXPRESS_ROUTE_WITH_MIDDLEWARE = re.compile(
    r'(?:app|router)\s*\.\s*(?:get|post|put|delete|patch)\s*\(\s*'
    r'(?P<route>[\'"][^\'"]+[\'"])\s*,\s*'
    r'(?P<middleware>[^(]+?)\s*,\s*'  # middleware function reference
    r'(?:async\s+)?\(?(?:req|request)'
)
_AUTH_MIDDLEWARE_NAMES = frozenset({
    'requireAdmin', 'isAdmin', 'requireRole', 'checkRole', 'authorize',
    'requireAuth', 'isAuthenticated', 'ensureAuthenticated',
    'requirePermission', 'checkPermission', 'guard', 'protect',
    'authMiddleware', 'adminMiddleware', 'roleMiddleware',
})
```

**Logic:**
1. Find routes matching `_ADMIN_ROUTE_PATTERNS`.
2. Parse middleware chain for the route.
3. If no middleware name in `_AUTH_MIDDLEWARE_NAMES` appears before the handler → CWE-269.

**CPGQL:**
```
cpg.call.name("delete|put|patch|post")
    .filter(c => c.argument(1).code.matches(".*admin.*|.*manage.*|.*internal.*"))
    .filter(c =>
        c.argument.filter(a =>
            a.order > 1 && a.order < c.argument.size &&
            a.code.matches(".*(?:auth|admin|role|permission|guard|protect).*")
        ).isEmpty
    )
```

#### NestJS / @Roles Guard

**Vulnerable pattern:**
```typescript
@Controller('admin')
export class AdminController {
    @Delete('users/:id')
    // missing @UseGuards(RolesGuard) and @Roles('admin')
    deleteUser(@Param('id') id: string) { ... }
}
```

**Detection CPGQL:**
```
cpg.call.name("__decorate").filter(d =>
    d.code.contains("Controller(") &&
    d.code.matches(".*(?:admin|manage|internal).*")
).flatMap(d =>
    cpg.typeDecl.name(d.argument(4).code.replace(".prototype", ""))
        .method.filter(m =>
            !m.ast.isCall.code(".*UseGuards.*|.*Roles.*").nonEmpty
        )
)
```

#### Django / @permission_required

**Vulnerable pattern:**
```python
# missing @login_required or @permission_required
def admin_delete_user(request, user_id):
    User.objects.get(id=user_id).delete()
```

**Detection:**
```python
_DJANGO_ADMIN_VIEW = re.compile(
    r'def\s+(?:admin_\w+|manage_\w+|delete_\w+)\s*\(\s*request'
)
_DJANGO_PERMISSION_DECORATOR = re.compile(
    r'@(?:login_required|permission_required|user_passes_test|staff_member_required)'
)
```

**Logic:** If `_DJANGO_ADMIN_VIEW` found and no `_DJANGO_PERMISSION_DECORATOR` on the preceding lines → CWE-269, confidence 0.5.

#### Spring / @PreAuthorize

**Vulnerable pattern:**
```java
@DeleteMapping("/admin/users/{id}")
// missing @PreAuthorize("hasRole('ADMIN')") or @Secured("ROLE_ADMIN")
public void deleteUser(@PathVariable Long id) {
    userRepository.deleteById(id);
}
```

**Detection CPGQL:**
```
cpg.method.annotation.name("DeleteMapping|PutMapping|PostMapping")
    .filter(m =>
        m.annotation.code.matches(".*(?:admin|manage|internal).*") &&
        !m.annotation.name("PreAuthorize|Secured|RolesAllowed").nonEmpty
    )
```

### 7.5 Confidence Calibration

Base confidence: **0.45** (high FP risk — apps may enforce auth at a higher middleware layer).

Boost to 0.6 if:
- Route explicitly matches `/admin/*`
- Handler performs destructive operation (DELETE, or calls `.destroy`/`.delete`/`.remove`)

Reduce to 0.3 if:
- A global auth middleware is detected (e.g., `app.use(requireAuth)` before route definitions)
- The controller class has a class-level guard/decorator

### 7.6 Severity

```python
"CWE-269": "high",
```

---

## 8. Missing Auth for Critical Function (CWE-306)

**Estimated effort: 6-8h**

### 8.1 Threat Model

Sensitive endpoints (user deletion, password reset, payment processing, account settings modification) that are accessible without any authentication check.

### 8.2 Detection Strategy

**Keyword + absence-of-pattern:** Identify handlers for sensitive operations by route path and handler body keywords, verify authentication middleware presence.

### 8.3 Sensitive Operation Keywords

```python
_CRITICAL_ROUTE_KEYWORDS = frozenset({
    'delete', 'remove', 'destroy',           # data deletion
    'password', 'reset-password', 'change-password', # credential change
    'payment', 'charge', 'purchase', 'checkout', 'billing', # financial
    'transfer', 'withdraw', 'deposit',        # financial
    'settings', 'profile/edit', 'account',    # account management
    'api-key', 'token', 'secret',             # credential management
    'export', 'download/all', 'backup',       # data exfiltration
    'invite', 'grant', 'revoke',              # permission management
})

_CRITICAL_HANDLER_KEYWORDS = re.compile(
    r'(?:deleteUser|removeAccount|resetPassword|changePassword|'
    r'processPayment|chargeCard|transferFunds|updateRole|'
    r'generateApiKey|revokeToken|exportData|deleteAll|'
    r'bulkDelete|purge|wipe)'
)
```

### 8.4 Framework-Specific Auth Detection

Reuse the same auth middleware detection from Section 7 (CWE-269). The difference is scope: CWE-269 targets admin routes specifically, CWE-306 targets any sensitive operation regardless of admin status.

#### Express

```python
_EXPRESS_AUTH_MIDDLEWARE = re.compile(
    r'(?:requireAuth|isAuthenticated|ensureAuthenticated|'
    r'authenticate|passport\.authenticate|'
    r'verifyToken|checkToken|authGuard|protect)'
)
```

**Logic:**
1. Find route registrations where the route path contains keywords from `_CRITICAL_ROUTE_KEYWORDS`.
2. OR handler body matches `_CRITICAL_HANDLER_KEYWORDS`.
3. Check if an auth middleware is in the middleware chain.
4. If no auth middleware → CWE-306.

#### Django

```python
_DJANGO_AUTH_DECORATORS = re.compile(
    r'@(?:login_required|permission_required|user_passes_test)'
)
_DJANGO_AUTH_MIXIN = re.compile(
    r'(?:LoginRequiredMixin|PermissionRequiredMixin|UserPassesTestMixin)'
)
```

#### Spring

```python
_SPRING_AUTH_ANNOTATIONS = re.compile(
    r'@(?:PreAuthorize|Secured|RolesAllowed|WithMockUser)'
)
_SPRING_SECURITY_CONFIG = re.compile(
    r'\.authorizeRequests\(\)|\.authorizeHttpRequests\(\)'
)
```

### 8.5 Confidence Calibration

Base confidence: **0.45**.

Boost to 0.6 if:
- Handler performs a write operation (POST/PUT/DELETE + DB mutation)
- Route contains financial keywords (`payment`, `transfer`, `charge`)

Reduce to 0.3 if:
- A global auth middleware is detected at the app level
- The endpoint is a public-by-design route (e.g., `/reset-password` initial page, `/register`)

### 8.6 Public-By-Design Exclusions

Some sensitive-sounding routes are intentionally public:

```python
_PUBLIC_BY_DESIGN = frozenset({
    '/login', '/signin', '/signup', '/register',
    '/forgot-password', '/reset-password',  # initial form, not the actual reset
    '/health', '/healthcheck', '/ping',
    '/public', '/oauth/callback',
})
```

### 8.7 Severity

```python
"CWE-306": "high",
```

---

## 9. Testing Strategy

### 9.1 Ground Truth Entries

Minimum 35 new ground truth YAML entries (5+ per CWE):

| CWE | TP Entries | FP Entries | Total |
|-----|-----------|-----------|-------|
| CWE-352 (CSRF) | 5 | 3 | 8 |
| CWE-639 (IDOR) | 5 | 4 | 9 |
| CWE-287 (Broken Auth) | 7 | 4 | 11 |
| CWE-384 (Session Fixation) | 5 | 3 | 8 |
| CWE-915 (Mass Assignment) | 6 | 4 | 10 |
| CWE-269 (Privilege Escalation) | 5 | 3 | 8 |
| CWE-306 (Missing Auth) | 5 | 3 | 8 |
| **Total** | **38** | **24** | **62** |

GT entries start at `gt-145.yaml` (continuing from existing gt-144).
FP entries start at `gt-fp-033.yaml` (continuing from existing gt-fp-032).

### 9.2 Synthetic Fixture Files

Located in `eval/synthetic/`:

#### CSRF Fixtures
- `csrf-express-no-protection.ts` — Express POST handler without csurf
- `csrf-express-with-csurf.ts` — Express POST handler with csurf (FP check)
- `csrf-django-exempt.py` — Django view with @csrf_exempt
- `csrf-flask-no-protect.py` — Flask POST without CSRFProtect
- `csrf-spring-disabled.java` — Spring Security with csrf().disable()

#### IDOR Fixtures
- `idor-express-no-ownership.ts` — params.id → db.find without user check
- `idor-express-with-ownership.ts` — params.id → db.find with user_id filter (FP check)
- `idor-django-no-filter.py` — pk lookup without request.user filter
- `idor-django-with-filter.py` — pk lookup with request.user (FP check)
- `idor-spring-no-principal.java` — @PathVariable to findById without principal

#### Broken Auth Fixtures
- `auth-timing-unsafe-compare.ts` — `===` password comparison
- `auth-timing-safe-compare.ts` — timingSafeEqual usage (FP check)
- `auth-jwt-alg-none.ts` — jwt.verify with algorithms: ['none']
- `auth-jwt-no-expiry.ts` — jwt.sign without expiresIn
- `auth-jwt-proper.ts` — jwt.sign with expiry + audience (FP check)
- `auth-passport-plaintext.ts` — LocalStrategy with === comparison
- `auth-django-raw-password.py` — user.password == request.POST

#### Session Fixation Fixtures
- `session-no-regenerate-express.ts` — login sets session without regenerate
- `session-regenerate-express.ts` — login with req.session.regenerate (FP check)
- `session-flask-no-clear.py` — Flask login without session.clear
- `session-django-manual.py` — manual session set without cycle_key
- `session-spring-fixation-none.java` — sessionFixation().none()

#### Mass Assignment Fixtures
- `mass-assign-sequelize.ts` — User.create(req.body)
- `mass-assign-sequelize-safe.ts` — destructured fields (FP check)
- `mass-assign-mongoose.ts` — new User(req.body)
- `mass-assign-prisma.ts` — prisma.create({ data: req.body })
- `mass-assign-django-all.py` — fields = '__all__'
- `mass-assign-spring-entity.java` — @RequestBody with entity type

#### Privilege Escalation Fixtures
- `privesc-express-no-guard.ts` — /admin route without auth middleware
- `privesc-express-with-guard.ts` — /admin route with requireAdmin (FP check)
- `privesc-nestjs-no-roles.ts` — @Controller('admin') without @Roles
- `privesc-django-no-perm.py` — admin view without @permission_required
- `privesc-spring-no-preauth.java` — admin endpoint without @PreAuthorize

#### Missing Auth Fixtures
- `missing-auth-payment.ts` — /payment endpoint without auth
- `missing-auth-delete-user.ts` — /users/:id DELETE without auth
- `missing-auth-with-auth.ts` — same with requireAuth middleware (FP check)
- `missing-auth-django-delete.py` — delete view without @login_required
- `missing-auth-spring-charge.java` — payment endpoint without @PreAuthorize

### 9.3 Framework-Specific Test Matrix

Each detection module must be tested across all supported frameworks:

| CWE | Express | NestJS | Fastify | Django | Flask | FastAPI | Spring | Gin | Echo |
|-----|:-------:|:------:|:-------:|:------:|:-----:|:-------:|:------:|:---:|:----:|
| 352 | Y | Y | Y | Y | Y | - | Y | - | - |
| 639 | Y | Y | - | Y | - | Y | Y | Y | - |
| 287 | Y | - | - | Y | Y | - | Y | - | - |
| 384 | Y | - | - | Y | Y | - | Y | - | - |
| 915 | Y | Y | - | Y | - | Y | Y | - | - |
| 269 | Y | Y | - | Y | - | - | Y | Y | - |
| 306 | Y | Y | - | Y | Y | - | Y | - | - |

Y = must have tests. `-` = not applicable or deferred.

### 9.4 Unit Tests

Located in `tests/test_detect/test_auth_access.py`:

```python
class TestCSRFDetection:
    def test_express_post_no_csrf(self): ...
    def test_express_post_with_csurf_no_finding(self): ...
    def test_django_csrf_exempt(self): ...
    def test_flask_no_csrf_protect(self): ...
    def test_spring_csrf_disable(self): ...

class TestIDORDetection:
    def test_express_params_to_db_no_ownership(self): ...
    def test_express_params_to_db_with_user_check_no_finding(self): ...
    def test_django_pk_no_user_filter(self): ...
    def test_spring_pathvariable_no_principal(self): ...

class TestBrokenAuth:
    def test_timing_unsafe_password_compare(self): ...
    def test_timing_safe_equal_no_finding(self): ...
    def test_jwt_alg_none(self): ...
    def test_jwt_hardcoded_secret(self): ...
    def test_jwt_missing_expiry(self): ...
    def test_passport_plaintext(self): ...

class TestSessionFixation:
    def test_express_login_no_regenerate(self): ...
    def test_express_login_with_regenerate_no_finding(self): ...
    def test_flask_login_no_clear(self): ...
    def test_django_manual_session_no_cycle(self): ...

class TestMassAssignment:
    def test_sequelize_create_req_body(self): ...
    def test_sequelize_create_destructured_no_finding(self): ...
    def test_mongoose_new_model_req_body(self): ...
    def test_prisma_create_data_req_body(self): ...
    def test_django_fields_all(self): ...
    def test_spring_entity_request_body(self): ...

class TestPrivilegeEscalation:
    def test_admin_route_no_middleware(self): ...
    def test_admin_route_with_auth_no_finding(self): ...
    def test_nestjs_controller_no_roles(self): ...
    def test_django_admin_view_no_permission(self): ...

class TestMissingAuth:
    def test_payment_endpoint_no_auth(self): ...
    def test_delete_user_no_auth(self): ...
    def test_payment_endpoint_with_auth_no_finding(self): ...
    def test_public_route_excluded(self): ...
```

---

## 10. Implementation Notes

### 10.1 New Files

| File | Purpose |
|------|---------|
| `src/piranesi/detect/auth_access.py` | Main detection module for all 7 CWEs |
| `tests/test_detect/test_auth_access.py` | Unit tests |
| `eval/synthetic/csrf-*.ts` | CSRF fixtures |
| `eval/synthetic/idor-*.ts` | IDOR fixtures |
| `eval/synthetic/auth-*.ts` | Auth fixtures |
| `eval/synthetic/session-*.ts` | Session fixtures |
| `eval/synthetic/mass-assign-*.ts` | Mass assignment fixtures |
| `eval/synthetic/privesc-*.ts` | Privilege escalation fixtures |
| `eval/synthetic/missing-auth-*.ts` | Missing auth fixtures |
| `eval/ground_truth/gt-145.yaml` through `gt-182.yaml` | TP ground truth |
| `eval/ground_truth/gt-fp-033.yaml` through `gt-fp-056.yaml` | FP ground truth |

### 10.2 Modified Files

| File | Changes |
|------|---------|
| `src/piranesi/scan/specs.py` | Add `SinkType.STATE_CHANGE_HANDLER`, `SinkType.ORM_WRITE`, `SinkType.AUTH_SENSITIVE`; add sanitizer specs for auth CWEs |
| `src/piranesi/detect/__init__.py` | Export `extract_auth_access_findings` |
| `src/piranesi/detect/flows.py` | Add CWE-352/639/269/287/384/915/306 to `_SEVERITY_BY_CWE` |
| `src/piranesi/pipeline.py` | Call `extract_auth_access_findings` in the detect stage |
| `src/piranesi/report/cwe.py` | Add CWE descriptions and remediation text for all 7 new CWEs |

### 10.3 `detect/auth_access.py` Module Structure

```python
from __future__ import annotations

import hashlib
import re
from bisect import bisect_right
from collections.abc import Sequence
from dataclasses import dataclass
from pathlib import Path

from piranesi.detect.flows import severity_for_cwe
from piranesi.models import CandidateFinding, SourceLocation, TaintSink, TaintSource

_SOURCE_FILE_EXTENSIONS = frozenset({
    ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs",
    ".py", ".java", ".go",
})

@dataclass(frozen=True, slots=True)
class AuthAccessConfig:
    enable_csrf: bool = True
    enable_idor: bool = True
    enable_broken_auth: bool = True
    enable_session_fixation: bool = True
    enable_mass_assignment: bool = True
    enable_privilege_escalation: bool = True
    enable_missing_auth: bool = True
    confidence_floor: float = 0.3  # suppress below this

def extract_auth_access_findings(
    project_root: Path,
    files: Sequence[Path],
    *,
    config: AuthAccessConfig | None = None,
) -> list[CandidateFinding]:
    """Run all auth/access control detectors. Returns CandidateFindings."""
    cfg = config or AuthAccessConfig()
    findings: list[CandidateFinding] = []
    for path in files:
        if path.suffix not in _SOURCE_FILE_EXTENSIONS:
            continue
        scanned = _ScannedFile.load(path)
        if scanned is None:
            continue
        if cfg.enable_csrf:
            findings.extend(_detect_csrf(scanned))
        if cfg.enable_idor:
            findings.extend(_detect_idor(scanned))
        if cfg.enable_broken_auth:
            findings.extend(_detect_broken_auth(scanned))
        if cfg.enable_session_fixation:
            findings.extend(_detect_session_fixation(scanned))
        if cfg.enable_mass_assignment:
            findings.extend(_detect_mass_assignment(scanned))
        if cfg.enable_privilege_escalation:
            findings.extend(_detect_privilege_escalation(scanned))
        if cfg.enable_missing_auth:
            findings.extend(_detect_missing_auth(scanned))
    return [f for f in findings if f.confidence >= cfg.confidence_floor]
```

Each `_detect_*` function follows the same pattern as `extract_misconfiguration_findings` in `detect/misconfigurations.py`: parse the file text with regex, compute source locations, build `CandidateFinding` objects with appropriate `vuln_class`, `confidence`, and `severity`.

### 10.4 Pipeline Integration

In `pipeline.py`, add to the detect stage (after `extract_misconfiguration_findings`):

```python
from piranesi.detect.auth_access import extract_auth_access_findings

# in _run_detect_stage():
auth_findings = extract_auth_access_findings(
    project_root=scan_result.project_root,
    files=[Path(f) for f in scan_result.files_scanned],
)
all_findings.extend(auth_findings)
```

### 10.5 Spec Additions to `scan/specs.py`

Add to `_SEVERITY_BY_CWE` in `detect/flows.py`:
```python
"CWE-352": "medium",
"CWE-639": "high",
"CWE-269": "high",
"CWE-287": "high",
"CWE-384": "medium",
"CWE-915": "high",
"CWE-306": "high",
```

Add new sanitizer specs to `BUILTIN_SANITIZER_SPECS`:
```python
SanitizerSpec(
    name="csurf_middleware",
    pattern='cpg.call.name("csrf|csurf|csrfProtection")',
    kind=SanitizerKind.NORMALIZE,
    mitigates=("CWE-352",),
    confidence=0.9,
),
SanitizerSpec(
    name="bcrypt_compare",
    pattern='cpg.call.name("compare|compareSync").code(".*bcrypt[.]compare.*")',
    kind=SanitizerKind.NORMALIZE,
    mitigates=("CWE-287",),
    confidence=0.95,
),
SanitizerSpec(
    name="timing_safe_equal",
    pattern='cpg.call.name("timingSafeEqual")',
    kind=SanitizerKind.NORMALIZE,
    mitigates=("CWE-287",),
    confidence=0.95,
),
SanitizerSpec(
    name="argon2_verify",
    pattern='cpg.call.name("verify").code(".*argon2[.]verify.*")',
    kind=SanitizerKind.NORMALIZE,
    mitigates=("CWE-287",),
    confidence=0.95,
),
SanitizerSpec(
    name="session_regenerate",
    pattern='cpg.call.name("regenerate").code(".*session[.]regenerate.*")',
    kind=SanitizerKind.NORMALIZE,
    mitigates=("CWE-384",),
    confidence=0.9,
),
SanitizerSpec(
    name="lodash_pick_allowlist",
    pattern='cpg.call.name("pick|omit").code(".*(?:_|lodash)[.](?:pick|omit)[(].*")',
    kind=SanitizerKind.NORMALIZE,
    mitigates=("CWE-915",),
    confidence=0.85,
),
SanitizerSpec(
    name="zod_parse_validation",
    pattern='cpg.call.name("parse|safeParse").code(".*(?:zod|z|schema)[.](?:parse|safeParse)[(].*")',
    kind=SanitizerKind.NORMALIZE,
    mitigates=("CWE-915",),
    confidence=0.9,
),
SanitizerSpec(
    name="class_transformer",
    pattern='cpg.call.name("plainToClass|plainToInstance")',
    kind=SanitizerKind.NORMALIZE,
    mitigates=("CWE-915",),
    confidence=0.85,
),
```

### 10.6 CWE Report Descriptions

Add to `report/cwe.py`:

```python
CWE_DESCRIPTIONS = {
    # ... existing ...
    "CWE-352": CWEDescription(
        id="CWE-352",
        name="Cross-Site Request Forgery (CSRF)",
        description="The application does not verify that a state-changing request originated from the application itself.",
        remediation="Add CSRF token validation middleware (csurf for Express, CSRFProtect for Flask, CsrfViewMiddleware for Django).",
        references=["https://cwe.mitre.org/data/definitions/352.html"],
        owasp_category="A01:2021",
    ),
    "CWE-639": CWEDescription(
        id="CWE-639",
        name="Authorization Bypass Through User-Controlled Key (IDOR)",
        description="The application uses a user-controlled identifier to access resources without verifying the user owns the resource.",
        remediation="Add ownership checks (WHERE user_id = session.user) to all queries using user-supplied identifiers.",
        references=["https://cwe.mitre.org/data/definitions/639.html"],
        owasp_category="A01:2021",
    ),
    "CWE-269": CWEDescription(
        id="CWE-269",
        name="Improper Privilege Management",
        description="Administrative or privileged routes are accessible without proper role verification.",
        remediation="Add role-based authorization middleware to all admin and management routes.",
        references=["https://cwe.mitre.org/data/definitions/269.html"],
        owasp_category="A01:2021",
    ),
    "CWE-287": CWEDescription(
        id="CWE-287",
        name="Improper Authentication",
        description="The application uses insecure authentication patterns such as timing-unsafe comparison, JWT misconfiguration, or plaintext password handling.",
        remediation="Use timing-safe comparison for secrets, validate JWT algorithm/expiry/audience, hash passwords with bcrypt/argon2.",
        references=["https://cwe.mitre.org/data/definitions/287.html"],
        owasp_category="A07:2021",
    ),
    "CWE-384": CWEDescription(
        id="CWE-384",
        name="Session Fixation",
        description="The application does not regenerate the session identifier after authentication, allowing session fixation attacks.",
        remediation="Call session.regenerate() (Express), session.clear() (Flask), or session.cycle_key() (Django) after successful login.",
        references=["https://cwe.mitre.org/data/definitions/384.html"],
        owasp_category="A07:2021",
    ),
    "CWE-915": CWEDescription(
        id="CWE-915",
        name="Improperly Controlled Modification of Dynamically-Determined Object Attributes (Mass Assignment)",
        description="Request body data is passed directly to ORM create/update without field allowlisting, allowing attackers to modify unintended fields.",
        remediation="Use DTOs or explicit field picking. Never pass req.body directly to ORM methods. Use validation libraries (Zod, Joi, class-validator).",
        references=["https://cwe.mitre.org/data/definitions/915.html"],
        owasp_category="A01:2021",
    ),
    "CWE-306": CWEDescription(
        id="CWE-306",
        name="Missing Authentication for Critical Function",
        description="A sensitive operation (payment, deletion, credential change) is accessible without authentication.",
        remediation="Add authentication middleware to all sensitive endpoints. Use framework-specific auth guards.",
        references=["https://cwe.mitre.org/data/definitions/306.html"],
        owasp_category="A01:2021",
    ),
}
```

### 10.7 Confidence Calibration Strategy

Auth/access findings are inherently noisier than injection findings because they rely on absence-of-pattern rather than proven taint flows. The calibration approach:

1. **Default confidence range: 0.4-0.6** for all auth findings (vs 0.7 for injection).
2. **Confidence boosters** (each adds +0.1, capped at 0.7):
   - Route matches multiple heuristic signals (both path keyword + handler keyword)
   - Destructive operation detected (DELETE, `.destroy()`, `.delete()`)
   - Financial keywords in handler
   - Sensitive model fields present
3. **Confidence reducers** (each subtracts 0.1, floor at 0.3):
   - Global auth middleware detected
   - Validation library imported in same file
   - Custom decorator/guard present (may be auth-related)
   - Handler references session/user context (may have check we missed)
4. **Findings below 0.3 confidence are suppressed** (not emitted).

### 10.8 OWASP Coverage After Phase 26

| OWASP Category | Pre-Phase 26 | Post-Phase 26 |
|----------------|:------------:|:-------------:|
| A01: Broken Access Control | Partial (CWE-22, 601) | Strong (+ CWE-352, 639, 269, 306, 915) |
| A02: Cryptographic Failures | Partial (CWE-798) | Partial (CWE-798) |
| A03: Injection | Strong (CWE-77, 78, 79, 89, 94) | Strong |
| A04: Insecure Design | Partial (CWE-434) | Partial |
| A05: Security Misconfiguration | Moderate (CWE-444, 611, 942, 1021, 693, 319) | Moderate |
| A06: Vulnerable Components | Moderate (dep scanning) | Moderate |
| A07: Auth Failures | Partial (CWE-798) | Strong (+ CWE-287, 384) |
| A08: Data Integrity | Moderate (CWE-502) | Moderate |
| A09: Logging Failures | None (design-level) | None |
| A10: SSRF | Strong (CWE-918) | Strong |

### 10.9 Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| High FP rate on IDOR (CWE-639) | User trust erosion | Conservative base confidence (0.45), require multiple signals to boost |
| Global middleware detection miss | FN for CWE-269/306 | Scan for `app.use()` calls at top of entry file, propagate globally |
| Custom auth patterns unrecognized | FN across all CWEs | Configurable auth function name list in `.piranesi.yaml` |
| Cross-file middleware chains | FN for Express middleware | Leverage existing cross-module analysis from Phase 22 |
| Django class-based views | Detection gap | Add CBV mixin detection (LoginRequiredMixin, etc.) |
| Spring Security global config | FN for CWE-352/269 | Parse `SecurityConfig` class for `.authorizeRequests()` chains |

### 10.10 Configuration Extension

Add to `.piranesi.yaml` schema:

```yaml
auth_access:
  enabled: true
  confidence_floor: 0.3
  custom_auth_middleware:
    - "myCustomAuthMiddleware"
    - "companyAuthGuard"
  custom_admin_routes:
    - "/api/v1/internal/*"
  excluded_routes:
    - "/api/v1/public/*"
    - "/health"
```

This allows users to teach Piranesi about their custom auth patterns, reducing FPs without disabling the entire detection class.
