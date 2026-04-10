# Phase 27: Web Vulnerability Injection Variants

**Estimated effort: 65-85 ideal hours**
**Blocked by: Phase 22 (advanced taint analysis), Phase 18 (multi-language depth)**
**Blocks: Nothing (incremental value, but improves OWASP A03 coverage)**
**Target milestone: v0.5.0**

---

## 1. Overview

### 1.1 Gap Analysis

Piranesi currently detects five injection-class CWEs:

| CWE | Class | Status |
|-----|-------|--------|
| CWE-79 | Cross-Site Scripting (XSS) | Implemented |
| CWE-89 | SQL Injection | Implemented |
| CWE-78 | OS Command Injection | Implemented |
| CWE-94 | Code Injection (eval) | Implemented |
| CWE-1321 | Prototype Pollution | Implemented |

Modern web applications — especially those using MongoDB, template engines, LDAP directories, XPath queries, and expression languages — expose additional injection surfaces that the current spec set does not cover. These are not exotic edge cases: NoSQL injection and SSTI routinely appear in HackerOne and Snyk vulnerability databases, and ReDoS has been the root cause of multiple high-profile Node.js CVEs (e.g., `ua-parser-js`, `color-string`, `trim-newlines`).

### 1.2 New CWE Targets

| CWE | Class | Detection Type | Estimated Effort |
|-----|-------|---------------|-----------------|
| CWE-943 | NoSQL Injection | Taint analysis | 12-15h |
| CWE-1336 | Server-Side Template Injection (SSTI) | Taint analysis | 10-14h |
| CWE-1333 | ReDoS (Regular Expression DoS) | Static pattern + taint | 12-16h |
| CWE-90 | LDAP Injection | Taint analysis | 6-8h |
| CWE-113 | HTTP Header Injection / Response Splitting | Taint analysis | 6-8h |
| CWE-917 | Expression Language Injection | Taint analysis | 8-10h |
| CWE-643 | XPath Injection | Taint analysis | 5-7h |

### 1.3 Architecture

All new CWEs except CWE-1333 (ReDoS) follow the existing taint analysis pattern: add `SourceSpec`/`SinkSpec`/`SanitizerSpec` entries to `scan/specs.py`, extend `SinkType` enum, and let the existing `detect/flows.py` pipeline produce `CandidateFinding` artifacts. ReDoS requires a dedicated static pattern analyzer (`detect/redos.py`) because catastrophic backtracking is a structural property of the regex, not a taint flow.

New files:

| File | Purpose |
|------|---------|
| `src/piranesi/detect/injection_variants.py` | Shared helpers for the new injection CWEs (operator-position detection, template-source-vs-context discrimination) |
| `src/piranesi/detect/redos.py` | NFA-based ReDoS detection (star height analysis, exponential state detection) |

Modified files:

| File | Change |
|------|--------|
| `src/piranesi/scan/specs.py` | New `SinkType` enum members, new sink/sanitizer specs per CWE |
| `src/piranesi/detect/sanitizer_validation.py` | Extend `SANITIZER_EFFECTIVENESS` matrix with new CWE columns |
| `src/piranesi/detect/flows.py` | Wire `_SEVERITY_BY_CWE` for new CWEs |
| `src/piranesi/detect/categories.py` | Data category classification for new CWE classes |
| `src/piranesi/pipeline.py` | Integrate `redos.py` as a sub-stage |

---

## 2. NoSQL Injection (CWE-943)

### 2.1 Threat Model

NoSQL injection occurs when user-controlled input reaches a MongoDB (or similar) query in a position where it can inject query operators (`$gt`, `$ne`, `$regex`, `$where`, etc.) or entire query objects. Unlike SQL injection, the attack surface is the JSON query structure itself, not a string concatenation.

**Critical distinction:** `db.collection.find({ email: req.body.email })` where `req.body.email` is a string is safe. But if `req.body` is parsed as JSON and `req.body.email` is `{"$ne": ""}`, the query becomes `find({ email: {"$ne": ""} })` — returning all documents.

### 2.2 Sources

Identical to existing SQLi sources (already defined in `BUILTIN_SOURCE_SPECS`):
- `req.body.*`, `req.query.*`, `req.params.*` (Express)
- `request.body.*`, `request.query.*` (Fastify)
- `request.form.*`, `request.args.*`, `request.json.*` (Flask)
- `request.POST.*`, `request.GET.*` (Django)
- `c.Query()`, `c.PostForm()`, `c.BindJSON()` (Gin)

### 2.3 Sinks

#### 2.3.1 JavaScript/TypeScript — MongoDB Native Driver

```javascript
// VULNERABLE: user object flows to query operator position
const db = client.db("app");
const users = db.collection("users");
users.find({ username: req.body.username });         // CWE-943 if req.body.username is object
users.findOne({ email: req.body.email });             // CWE-943
users.updateOne({ _id: req.body.id }, { $set: {} });  // CWE-943 in filter position
users.deleteMany(req.body.filter);                     // CWE-943: entire filter from user
```

```javascript
// VULNERABLE: $where with user string (server-side JS execution)
users.find({ $where: "this.name === '" + req.body.name + "'" }); // CWE-943 + CWE-94
```

```javascript
// VULNERABLE: $regex with user input
users.find({ username: { $regex: req.body.search } }); // CWE-943 (also ReDoS risk)
```

**CPGQL sink patterns:**

```python
# mongodb native driver — collection methods with user-controlled filter argument
SinkSpec(
    name="mongodb_find",
    pattern='cpg.call.name("find|findOne|findOneAndUpdate|findOneAndDelete|findOneAndReplace")',
    sink_type=SinkType.NOSQL_QUERY,
    cwe_id="CWE-943",
    severity="high",
)
SinkSpec(
    name="mongodb_update",
    pattern='cpg.call.name("updateOne|updateMany|replaceOne")',
    sink_type=SinkType.NOSQL_QUERY,
    cwe_id="CWE-943",
    severity="high",
)
SinkSpec(
    name="mongodb_delete",
    pattern='cpg.call.name("deleteOne|deleteMany")',
    sink_type=SinkType.NOSQL_QUERY,
    cwe_id="CWE-943",
    severity="high",
)
SinkSpec(
    name="mongodb_aggregate",
    pattern='cpg.call.name("aggregate")',
    sink_type=SinkType.NOSQL_QUERY,
    cwe_id="CWE-943",
    severity="high",
)
SinkSpec(
    name="mongodb_where",
    pattern='cpg.call.name("find|findOne").where(_.argument.code(".*[$]where.*"))',
    sink_type=SinkType.NOSQL_QUERY,
    cwe_id="CWE-943",
    severity="critical", # $where enables arbitrary JS execution
)
```

#### 2.3.2 JavaScript/TypeScript — Mongoose ODM

```javascript
// VULNERABLE: Mongoose model methods
const User = mongoose.model("User", userSchema);
User.find(req.body);                                   // CWE-943: entire query from user
User.findOne({ email: req.body.email });               // CWE-943
User.where("age").gt(req.body.age);                    // CWE-943 if age is operator object
User.countDocuments(req.body.filter);                   // CWE-943
```

**CPGQL:**

```python
SinkSpec(
    name="mongoose_find",
    pattern=(
        'cpg.call.name("find|findOne|findById|findOneAndUpdate|findOneAndDelete'
        '|countDocuments|distinct|where")'
    ),
    sink_type=SinkType.NOSQL_QUERY,
    cwe_id="CWE-943",
    severity="high",
)
```

#### 2.3.3 Python — PyMongo

```python
# VULNERABLE
from pymongo import MongoClient
client = MongoClient()
db = client["app"]
users = db.users
users.find({"username": request.form["username"]})        # CWE-943
users.find_one({"email": request.json["email"]})          # CWE-943
users.update_one(request.json["filter"], {"$set": data})  # CWE-943
```

**CPGQL:**

```python
SinkSpec(
    name="pymongo_find",
    pattern=_PY_CALL_CODE_PATTERN.format(
        name="find|find_one|find_one_and_update|find_one_and_delete|find_one_and_replace",
        code=".*(?:collection|db)[.].*",
    ),
    sink_type=SinkType.NOSQL_QUERY,
    cwe_id="CWE-943",
    severity="high",
)
SinkSpec(
    name="pymongo_update",
    pattern=_PY_CALL_CODE_PATTERN.format(
        name="update_one|update_many|replace_one|delete_one|delete_many",
        code=".*(?:collection|db)[.].*",
    ),
    sink_type=SinkType.NOSQL_QUERY,
    cwe_id="CWE-943",
    severity="high",
)
SinkSpec(
    name="pymongo_aggregate",
    pattern=_PY_CALL_CODE_PATTERN.format(
        name="aggregate",
        code=".*(?:collection|db)[.]aggregate[(].*",
    ),
    sink_type=SinkType.NOSQL_QUERY,
    cwe_id="CWE-943",
    severity="high",
)
```

#### 2.3.4 Go — mgo / mongo-driver

```go
// VULNERABLE
collection.Find(bson.M{"username": r.FormValue("username")}) // mgo
collection.FindOne(ctx, bson.M{"email": r.FormValue("email")}) // mongo-driver
```

**CPGQL:**

```python
SinkSpec(
    name="go_mongo_find",
    pattern=_GO_CALL_CODE_PATTERN.format(
        method="Find|FindOne|FindOneAndUpdate|FindOneAndDelete|FindOneAndReplace",
        code=".*collection[.].*",
    ),
    sink_type=SinkType.NOSQL_QUERY,
    cwe_id="CWE-943",
    severity="high",
)
SinkSpec(
    name="go_mongo_aggregate",
    pattern=_GO_CALL_CODE_PATTERN.format(
        method="Aggregate",
        code=".*collection[.]Aggregate[(].*",
    ),
    sink_type=SinkType.NOSQL_QUERY,
    cwe_id="CWE-943",
    severity="high",
)
```

#### 2.3.5 Java — Spring Data MongoDB

```java
// VULNERABLE
@Autowired MongoTemplate mongoTemplate;
Query query = new Query(Criteria.where("username").is(request.getParameter("user")));
mongoTemplate.find(query, User.class);

// VULNERABLE: BasicQuery with raw JSON
BasicQuery basicQuery = new BasicQuery(request.getParameter("filter"));
mongoTemplate.find(basicQuery, User.class);
```

**CPGQL:**

```python
SinkSpec(
    name="spring_mongo_template_find",
    pattern=_JAVA_METHOD_FULL_NAME_PATTERN.format(
        class_pattern="MongoTemplate",
        method="find|findOne|findAndModify|findAndRemove|findAll",
    ),
    sink_type=SinkType.NOSQL_QUERY,
    cwe_id="CWE-943",
    severity="high",
)
SinkSpec(
    name="spring_basic_query",
    pattern='cpg.call.name("<operator>.new").code("new BasicQuery.*")',
    sink_type=SinkType.NOSQL_QUERY,
    cwe_id="CWE-943",
    severity="critical", # raw JSON query from user
)
```

### 2.4 Sanitizers

```python
# mongo-sanitize (npm) — strips keys starting with $
SanitizerSpec(
    name="mongo_sanitize",
    pattern='cpg.call.name("sanitize").code(".*mongo[-_]?sanitize.*")',
    kind=SanitizerKind.ESCAPE,
    mitigates=("CWE-943",),
    confidence=0.85,
)

# explicit field picking — only extracts scalar values
SanitizerSpec(
    name="nosql_field_pick",
    pattern='cpg.call.name("toString|String|Number|parseInt|parseFloat")',
    kind=SanitizerKind.NORMALIZE,
    mitigates=("CWE-943",),
    confidence=0.7,
)

# schema validation (Joi, Zod, Ajv) — validates shape before query
# already exists as json_schema_validate, extend mitigates tuple
```

Update existing `json_schema_validate` sanitizer:

```python
SanitizerSpec(
    name="json_schema_validate",
    pattern='cpg.call.name("validate|ajv|Joi|yup|zod").code(".*(?:validate|parse|safeParse)[(].*")',
    kind=SanitizerKind.NORMALIZE,
    mitigates=("CWE-502", "CWE-943"), # add CWE-943
    confidence=0.9,
)
```

### 2.5 Detection Pattern (injection_variants.py)

The core challenge is distinguishing safe scalar flow from dangerous object flow. When user input flows as a string to a query field value, it is typically safe. When user input flows as an object (parsed from JSON body) to a query filter position, it is dangerous.

```python
def is_nosql_operator_position(flow_path: list[TaintStep]) -> bool:
    """Check if taint reaches a MongoDB query in operator position.
    
    Heuristic: if the source is req.body (parsed JSON, can be object)
    and it flows directly to the first argument of find/findOne/etc.
    without explicit field extraction (e.g., req.body.email vs req.body),
    it's operator-position.
    """
    # ... implementation
```

### 2.6 Z3 Constraint (Exploit Verification)

For the Z3-based exploit verification (Phase 2 infrastructure):

```python
# payload must contain a MongoDB operator prefix or query object structure
nosql_constraint = z3.Or(
    z3.Contains(payload, z3.StringVal("$gt")),
    z3.Contains(payload, z3.StringVal("$ne")),
    z3.Contains(payload, z3.StringVal("$lt")),
    z3.Contains(payload, z3.StringVal("$regex")),
    z3.Contains(payload, z3.StringVal("$where")),
    z3.Contains(payload, z3.StringVal("$exists")),
    z3.Contains(payload, z3.StringVal("$in")),
    z3.And(
        z3.Contains(payload, z3.StringVal("{")),
        z3.Contains(payload, z3.StringVal("$")),
    ),
)
```

### 2.7 Ground Truth

| ID | File | Label | Pattern |
|----|------|-------|---------|
| gt-145 | `eval/synthetic/nosqli-mongoose-find.ts` | TP | `User.find(req.body)` — entire body as filter |
| gt-146 | `eval/synthetic/nosqli-mongodb-findone.ts` | TP | `collection.findOne({ user: req.body.user })` — object in field |
| gt-147 | `eval/synthetic/nosqli-where-concat.ts` | TP | `find({ $where: "..." + input })` — JS execution |
| gt-148 | `eval/synthetic/nosqli-regex-user.ts` | TP | `find({ name: { $regex: req.query.q } })` |
| gt-fp-033 | `eval/synthetic/nosqli-sanitized.ts` | FP | `mongoSanitize(req.body)` before find |
| gt-fp-034 | `eval/synthetic/nosqli-schema-validated.ts` | FP | Joi validates shape, then passes to find |
| gt-149 | `eval/synthetic/nosqli-pymongo-find.py` | TP | `db.users.find(request.json)` |
| gt-150 | `eval/synthetic/nosqli-spring-basicquery.java` | TP | `new BasicQuery(request.getParameter("q"))` |

---

## 3. Server-Side Template Injection (CWE-1336)

### 3.1 Threat Model

SSTI occurs when user input is incorporated into a template string that is then compiled/rendered by a server-side template engine. The critical distinction is:

- **VULNERABLE:** `Template(user_input).render()` — user controls the template source
- **SAFE:** `template.render(context={"name": user_input})` — user controls a template variable

SSTI can escalate to Remote Code Execution (RCE) in most template engines (Jinja2: `{{ config.__class__.__init__.__globals__['os'].popen('id').read() }}`; Pug: `#{process.mainModule.require('child_process').execSync('id')}`).

### 3.2 Sources

All existing HTTP input sources apply. No new source specs needed.

### 3.3 Sinks

#### 3.3.1 JavaScript/TypeScript

```javascript
// VULNERABLE — user input as template source
const Handlebars = require("handlebars");
const template = Handlebars.compile(req.body.template); // CWE-1336
template({ name: "test" });

const ejs = require("ejs");
ejs.render(req.body.template, { name: "test" });        // CWE-1336

const pug = require("pug");
pug.render(req.body.template);                           // CWE-1336

const nunjucks = require("nunjucks");
nunjucks.renderString(req.body.template, {});            // CWE-1336
```

```javascript
// SAFE — user input as context variable
ejs.render("<h1><%= name %></h1>", { name: req.body.name }); // not SSTI
pug.renderFile("template.pug", { name: req.body.name });      // not SSTI
```

**CPGQL:**

```python
SinkSpec(
    name="handlebars_compile_user_template",
    pattern='cpg.call.name("compile").code(".*[Hh]andlebars[.]compile[(].*")',
    sink_type=SinkType.TEMPLATE_INJECTION,
    cwe_id="CWE-1336",
    severity="critical",
)
SinkSpec(
    name="ejs_render_string",
    pattern='cpg.call.name("render").code(".*ejs[.]render[(].*")',
    sink_type=SinkType.TEMPLATE_INJECTION,
    cwe_id="CWE-1336",
    severity="critical",
)
SinkSpec(
    name="pug_render_string",
    pattern='cpg.call.name("render|compile").code(".*pug[.](?:render|compile)[(].*")',
    sink_type=SinkType.TEMPLATE_INJECTION,
    cwe_id="CWE-1336",
    severity="critical",
)
SinkSpec(
    name="nunjucks_render_string",
    pattern='cpg.call.name("renderString").code(".*nunjucks[.]renderString[(].*")',
    sink_type=SinkType.TEMPLATE_INJECTION,
    cwe_id="CWE-1336",
    severity="critical",
)
```

**Argument position matters.** For `ejs.render(template, data)`, the first argument is the template source (dangerous), the second is context data (safe). The flow must reach argument position 1, not position 2.

```python
SinkSpec(
    name="ejs_render_template_arg",
    pattern='cpg.call.name("render").code(".*ejs[.]render[(].*").argument(1)',
    sink_type=SinkType.TEMPLATE_INJECTION,
    cwe_id="CWE-1336",
    severity="critical",
    flow_pattern='cpg.call.name("render").code(".*ejs[.]render[(].*").argument(1)',
    flow_to_parent_call=True,
)
```

#### 3.3.2 Python — Jinja2, Mako, Django

```python
# VULNERABLE — Jinja2
from jinja2 import Template
Template(request.form["template"]).render()                   # CWE-1336

from jinja2 import Environment
env = Environment()
tmpl = env.from_string(request.form["template"])              # CWE-1336
tmpl.render()

# VULNERABLE — Mako
from mako.template import Template as MakoTemplate
MakoTemplate(request.form["template"]).render()               # CWE-1336

# VULNERABLE — Django
from django.template import Template as DjangoTemplate
DjangoTemplate(request.POST["template"]).render(Context({}))  # CWE-1336
```

```python
# SAFE — Jinja2 with template file + context
from jinja2 import Environment, FileSystemLoader
env = Environment(loader=FileSystemLoader("templates"))
tmpl = env.get_template("index.html")
tmpl.render(name=request.form["name"])  # safe: user data in context, not template
```

**CPGQL:**

```python
SinkSpec(
    name="python_jinja2_template_constructor",
    pattern=_PY_CALL_CODE_PATTERN.format(
        name="Template",
        code=".*(?:jinja2[.])?Template[(].*",
    ),
    sink_type=SinkType.TEMPLATE_INJECTION,
    cwe_id="CWE-1336",
    severity="critical",
)
SinkSpec(
    name="python_jinja2_from_string",
    pattern=_PY_CALL_CODE_PATTERN.format(
        name="from_string",
        code=".*env[.]from_string[(].*",
    ),
    sink_type=SinkType.TEMPLATE_INJECTION,
    cwe_id="CWE-1336",
    severity="critical",
)
SinkSpec(
    name="python_mako_template_constructor",
    pattern=_PY_CALL_CODE_PATTERN.format(
        name="Template",
        code=".*[Mm]ako.*Template[(].*",
    ),
    sink_type=SinkType.TEMPLATE_INJECTION,
    cwe_id="CWE-1336",
    severity="critical",
)
SinkSpec(
    name="python_django_template_constructor",
    pattern=_PY_CALL_CODE_PATTERN.format(
        name="Template",
        code=".*django[.]template.*Template[(].*",
    ),
    sink_type=SinkType.TEMPLATE_INJECTION,
    cwe_id="CWE-1336",
    severity="critical",
)
```

#### 3.3.3 Java — Thymeleaf, Freemarker, Velocity

```java
// VULNERABLE — Thymeleaf
SpringTemplateEngine engine = new SpringTemplateEngine();
Context ctx = new Context();
engine.process(request.getParameter("template"), ctx); // CWE-1336

// VULNERABLE — Freemarker
freemarker.template.Template t = new freemarker.template.Template(
    "name", new StringReader(request.getParameter("template")), cfg
); // CWE-1336

// VULNERABLE — Velocity
VelocityEngine ve = new VelocityEngine();
ve.evaluate(context, writer, "tag", request.getParameter("template")); // CWE-1336
```

**CPGQL:**

```python
SinkSpec(
    name="java_thymeleaf_process",
    pattern=_JAVA_METHOD_FULL_NAME_PATTERN.format(
        class_pattern=".*TemplateEngine",
        method="process",
    ),
    sink_type=SinkType.TEMPLATE_INJECTION,
    cwe_id="CWE-1336",
    severity="critical",
)
SinkSpec(
    name="java_freemarker_template_new",
    pattern='cpg.call.name("<operator>.new").code("new.*freemarker.*Template.*")',
    sink_type=SinkType.TEMPLATE_INJECTION,
    cwe_id="CWE-1336",
    severity="critical",
)
SinkSpec(
    name="java_velocity_evaluate",
    pattern=_JAVA_METHOD_FULL_NAME_PATTERN.format(
        class_pattern="VelocityEngine",
        method="evaluate",
    ),
    sink_type=SinkType.TEMPLATE_INJECTION,
    cwe_id="CWE-1336",
    severity="critical",
)
```

### 3.4 Key Detection Logic (injection_variants.py)

The core discrimination function:

```python
def is_template_source_position(
    sink_spec: SinkSpec,
    flow_path: list[TaintStep],
) -> bool:
    """Determine if taint reaches the template SOURCE (vulnerable)
    vs. the template CONTEXT (safe).
    
    Template source = first arg of Template(), compile(), render(string),
                      from_string(), renderString(), evaluate()
    Template context = second arg of render(), named kwargs, Context object
    """
    terminal_step = flow_path[-1]
    # check if the terminal node is argument(1) of a template constructor/render call
    # vs argument(2+) or a context dict key
    # ...
```

### 3.5 Sanitizers

```python
SanitizerSpec(
    name="template_sandbox",
    pattern='cpg.call.name("SandboxedEnvironment|ImmutableSandboxedEnvironment")',
    kind=SanitizerKind.NORMALIZE,
    mitigates=("CWE-1336",),
    confidence=0.8, # sandboxes have known bypasses
)
SanitizerSpec(
    name="template_file_loader",
    pattern='cpg.call.name("get_template|renderFile|render").code(".*(?:get_template|renderFile)[(].*")',
    kind=SanitizerKind.NORMALIZE,
    mitigates=("CWE-1336",),
    confidence=0.95, # file-based templates are safe from SSTI
)
```

### 3.6 Z3 Constraints

```python
# engine-specific payload delimiters
ssti_jinja2 = z3.And(z3.Contains(payload, z3.StringVal("{{")), z3.Contains(payload, z3.StringVal("}}")))
ssti_ejs = z3.And(z3.Contains(payload, z3.StringVal("<%")), z3.Contains(payload, z3.StringVal("%>")))
ssti_pug = z3.Contains(payload, z3.StringVal("#{"))
ssti_velocity = z3.Or(
    z3.Contains(payload, z3.StringVal("${")),
    z3.Contains(payload, z3.StringVal("#set")),
)
ssti_freemarker = z3.And(z3.Contains(payload, z3.StringVal("<#")), z3.Contains(payload, z3.StringVal(">")))
ssti_constraint = z3.Or(ssti_jinja2, ssti_ejs, ssti_pug, ssti_velocity, ssti_freemarker)
```

### 3.7 Ground Truth

| ID | File | Label | Pattern |
|----|------|-------|---------|
| gt-151 | `eval/synthetic/ssti-ejs-render.ts` | TP | `ejs.render(req.body.template)` |
| gt-152 | `eval/synthetic/ssti-handlebars-compile.ts` | TP | `Handlebars.compile(req.body.tpl)` |
| gt-153 | `eval/synthetic/ssti-nunjucks-renderstring.ts` | TP | `nunjucks.renderString(req.body.tpl)` |
| gt-154 | `eval/synthetic/ssti-jinja2-template.py` | TP | `Template(request.form["tpl"]).render()` |
| gt-155 | `eval/synthetic/ssti-mako-template.py` | TP | `MakoTemplate(request.json["tpl"]).render()` |
| gt-fp-035 | `eval/synthetic/ssti-ejs-context-safe.ts` | FP | `ejs.render(staticTemplate, { name: req.body.name })` |
| gt-fp-036 | `eval/synthetic/ssti-jinja2-file-safe.py` | FP | `env.get_template("page.html").render(name=input)` |
| gt-fp-037 | `eval/synthetic/ssti-jinja2-sandbox.py` | FP | `SandboxedEnvironment().from_string(input)` |

---

## 4. ReDoS / Regular Expression Denial of Service (CWE-1333)

### 4.1 Threat Model

ReDoS is a denial-of-service attack that exploits regex engines using backtracking (NFA-based). A specially crafted input causes the regex to take exponential time. This affects all languages using backtracking engines (JavaScript, Python, Java, Go's `regexp/syntax` is safe — Go uses Thompson NFA — but Go code calling C libraries or using `regexp2` is vulnerable).

**Two distinct detection modes:**

1. **Static regex analysis** (no taint needed): scan all regex literals and `new RegExp(...)` with constant arguments for catastrophic backtracking patterns.
2. **Regex injection** (taint analysis): user input flows to `new RegExp(input)` — attacker controls the regex itself.

### 4.2 Vulnerable Regex Patterns

#### 4.2.1 Nested Quantifiers (Star Height >= 2)

```javascript
// catastrophic backtracking
const re1 = /^(a+)+$/;                    // nested quantifier
const re2 = /^([a-zA-Z0-9]+)*$/;          // nested quantifier
const re3 = /^(a|aa)+$/;                   // overlapping alternation + quantifier
const re4 = /^(.*a){x}$/;                  // .* followed by literal inside quantified group
```

#### 4.2.2 Overlapping Character Classes in Alternation

```javascript
const re5 = /^(\w+|\d+)+$/;               // overlapping \w and \d
const re6 = /^([a-z]|[a-c])+$/;           // overlapping ranges
```

#### 4.2.3 Backreference with Quantifier

```javascript
const re7 = /^(a+)\1+$/;                  // backreference + quantifier
```

### 4.3 Detection Algorithm (`detect/redos.py`)

Implement a variant of the `safe-regex` star height analysis:

```python
@dataclass(frozen=True, slots=True)
class RegexFinding:
    pattern: str
    file_path: str
    line_number: int
    vulnerability_type: str  # "nested_quantifier" | "overlapping_alternation" | "regex_injection"
    confidence: float
    is_taint: bool  # true if user controls regex, false if static bad regex

class RedosAnalyzer:
    """Detect ReDoS-vulnerable regex patterns via NFA state analysis."""
    
    def analyze_regex(self, pattern: str) -> RegexFinding | None:
        """Parse regex into AST, compute star height, detect exponential states."""
        # 1. parse regex into AST (use re._parse or custom parser)
        # 2. compute star height: max nesting depth of quantifiers
        # 3. if star_height >= 2, check if quantified groups overlap
        # 4. simulate NFA on adversarial input prefix to confirm
        ...
    
    def scan_file_for_regex(self, cpg_nodes: list[QueryNode]) -> list[RegexFinding]:
        """Extract all regex from CPG nodes: regex literals + new RegExp() calls."""
        # CPGQL to extract regex:
        # cpg.literal.typeFullName("__ecma.RegExp").code  (regex literals)
        # cpg.call.name("<operator>.new").code("new RegExp.*").argument(1).code  (constructor)
        ...
    
    def _star_height(self, node: RegexAST) -> int:
        """Compute star height of regex AST node."""
        ...
    
    def _has_overlapping_branches(self, alt_node: RegexAlternation) -> bool:
        """Check if alternation branches accept overlapping inputs."""
        ...
```

**CPGQL patterns for extracting regex from CPG:**

```
# JS regex literals
cpg.literal.typeFullName("__ecma.RegExp").l

# new RegExp() calls — extract first argument
cpg.call.name("<operator>.new").code("new RegExp.*").argument(1)

# Python re.compile() — extract first argument
cpg.call.name("compile").code(".*re[.]compile[(].*").argument(1)

# Java Pattern.compile() — extract first argument
cpg.call.methodFullName(".*Pattern[.]compile.*").argument(1)
```

### 4.4 Regex Injection (Taint Mode)

When user input flows to `new RegExp()` or `re.compile()`:

```javascript
// VULNERABLE: user controls regex
const search = new RegExp(req.query.pattern); // CWE-1333 (+ CWE-943 if used in MongoDB $regex)
users.filter(u => search.test(u.name));

// VULNERABLE: Python
import re
pattern = re.compile(request.args["regex"]) # CWE-1333
```

**Sink specs for regex injection:**

```python
SinkSpec(
    name="js_regexp_constructor",
    pattern='cpg.call.name("<operator>.new").code("new RegExp.*")',
    sink_type=SinkType.REGEX_INJECTION,
    cwe_id="CWE-1333",
    severity="medium",
)
SinkSpec(
    name="python_re_compile",
    pattern=_PY_CALL_CODE_PATTERN.format(name="compile", code=".*re[.]compile[(].*"),
    sink_type=SinkType.REGEX_INJECTION,
    cwe_id="CWE-1333",
    severity="medium",
)
SinkSpec(
    name="java_pattern_compile",
    pattern=_JAVA_METHOD_FULL_NAME_PATTERN.format(
        class_pattern="Pattern",
        method="compile",
    ),
    sink_type=SinkType.REGEX_INJECTION,
    cwe_id="CWE-1333",
    severity="medium",
)
```

### 4.5 Sanitizers

```python
SanitizerSpec(
    name="regex_escape",
    pattern='cpg.call.name("escapeRegExp|escape").code(".*(?:escapeRegExp|lodash[.]escape|escapeStringRegexp)[(].*")',
    kind=SanitizerKind.ESCAPE,
    mitigates=("CWE-1333",),
    confidence=0.9,
)
SanitizerSpec(
    name="python_re_escape",
    pattern=_PY_CALL_CODE_PATTERN.format(name="escape", code=".*re[.]escape[(].*"),
    kind=SanitizerKind.ESCAPE,
    mitigates=("CWE-1333",),
    confidence=0.95,
)
SanitizerSpec(
    name="regex_timeout",
    pattern='cpg.call.code(".*(?:timeout|RE2|re2).*")',
    kind=SanitizerKind.NORMALIZE,
    mitigates=("CWE-1333",),
    confidence=0.85, # timeout prevents DoS but regex may still be vulnerable
)
```

### 4.6 Ground Truth

| ID | File | Label | Pattern |
|----|------|-------|---------|
| gt-156 | `eval/synthetic/redos-nested-quantifier.ts` | TP | `/^(a+)+$/` — classic nested quantifier |
| gt-157 | `eval/synthetic/redos-overlapping-alt.ts` | TP | `/^(\w+\s?)*$/` — overlapping with optional |
| gt-158 | `eval/synthetic/redos-email-regex.ts` | TP | `/^([a-zA-Z0-9._%-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,6})*$/` — real-world bad email regex |
| gt-159 | `eval/synthetic/redos-regexp-injection.ts` | TP | `new RegExp(req.query.q)` — user controls regex |
| gt-160 | `eval/synthetic/redos-python-compile.py` | TP | `re.compile(request.args["pattern"])` |
| gt-fp-038 | `eval/synthetic/redos-safe-regex.ts` | FP | `/^[a-z]+$/` — linear-time regex |
| gt-fp-039 | `eval/synthetic/redos-escaped-input.ts` | FP | `new RegExp(escapeRegExp(req.query.q))` |
| gt-161 | `eval/synthetic/redos-ua-parser-cve.ts` | TP | Real CVE pattern from ua-parser-js |

---

## 5. LDAP Injection (CWE-90)

### 5.1 Threat Model

LDAP injection occurs when user input is concatenated into an LDAP filter string without escaping. Attackers can modify filter logic to bypass authentication or extract directory data.

**Attack example:**
```
Input:  username = "admin)(|(password=*)"
Filter: (&(uid=admin)(|(password=*))(userPassword=anything))
```

### 5.2 Sinks

#### 5.2.1 JavaScript/TypeScript — ldapjs

```javascript
// VULNERABLE
const ldap = require("ldapjs");
const client = ldap.createClient({ url: "ldap://localhost:389" });
const filter = `(&(uid=${req.body.username})(objectClass=person))`;
client.search("dc=example,dc=com", { filter: filter }, (err, res) => { ... });
```

```javascript
// SAFE
const { escape } = require("ldapjs");
const filter = `(&(uid=${escape(req.body.username)})(objectClass=person))`;
```

**CPGQL:**

```python
SinkSpec(
    name="ldapjs_search",
    pattern='cpg.call.name("search").code(".*(?:client|ldap)[.]search[(].*")',
    sink_type=SinkType.LDAP_QUERY,
    cwe_id="CWE-90",
    severity="high",
)
SinkSpec(
    name="ldapjs_filter_string_concat",
    pattern=(
        'cpg.call.name("<operator>.addition|<operator>.formatString")'
        '.where(_.code(".*(?:uid=|cn=|ou=|dc=|objectClass=|sAMAccountName=).*"))'
    ),
    sink_type=SinkType.LDAP_QUERY,
    cwe_id="CWE-90",
    severity="high",
)
```

#### 5.2.2 Python — python-ldap

```python
# VULNERABLE
import ldap
conn = ldap.initialize("ldap://localhost")
conn.search_s(
    "dc=example,dc=com",
    ldap.SCOPE_SUBTREE,
    f"(uid={request.form['username']})",  # CWE-90
)
```

**CPGQL:**

```python
SinkSpec(
    name="python_ldap_search",
    pattern=_PY_CALL_CODE_PATTERN.format(
        name="search_s|search_st|search_ext_s",
        code=".*(?:ldap|conn)[.]search.*",
    ),
    sink_type=SinkType.LDAP_QUERY,
    cwe_id="CWE-90",
    severity="high",
)
```

#### 5.2.3 Go — go-ldap

```go
// VULNERABLE
import "github.com/go-ldap/ldap/v3"
filter := fmt.Sprintf("(&(uid=%s)(objectClass=person))", r.FormValue("username"))
searchReq := ldap.NewSearchRequest("dc=example,dc=com", ldap.ScopeWholeSubtree,
    ldap.NeverDerefAliases, 0, 0, false, filter, []string{"dn"}, nil)
conn.Search(searchReq)
```

**CPGQL:**

```python
SinkSpec(
    name="go_ldap_search",
    pattern=_GO_CALL_CODE_PATTERN.format(
        method="NewSearchRequest",
        code="ldap[.]NewSearchRequest[(].*",
    ),
    sink_type=SinkType.LDAP_QUERY,
    cwe_id="CWE-90",
    severity="high",
)
```

#### 5.2.4 Java — Spring LDAP

```java
// VULNERABLE
@Autowired LdapTemplate ldapTemplate;
String filter = "(&(uid=" + request.getParameter("username") + ")(objectClass=person))";
ldapTemplate.search("", filter, new PersonAttributesMapper()); // CWE-90

// VULNERABLE — javax.naming
DirContext ctx = new InitialDirContext(env);
ctx.search("dc=example,dc=com",
    "(uid=" + request.getParameter("user") + ")", controls); // CWE-90
```

**CPGQL:**

```python
SinkSpec(
    name="spring_ldap_search",
    pattern=_JAVA_METHOD_FULL_NAME_PATTERN.format(
        class_pattern="LdapTemplate|DirContext|InitialDirContext",
        method="search|lookup",
    ),
    sink_type=SinkType.LDAP_QUERY,
    cwe_id="CWE-90",
    severity="high",
)
```

### 5.3 Sanitizers

```python
SanitizerSpec(
    name="ldap_escape",
    pattern='cpg.call.name("escape|escapeFilter|escapeDN|dn_escape").code(".*(?:ldap|ldapjs)[.]escape.*")',
    kind=SanitizerKind.ESCAPE,
    mitigates=("CWE-90",),
    confidence=0.95,
)
SanitizerSpec(
    name="python_ldap_filter_escape",
    pattern=_PY_CALL_CODE_PATTERN.format(
        name="escape_filter_chars",
        code=".*ldap[.]filter[.]escape_filter_chars[(].*",
    ),
    kind=SanitizerKind.ESCAPE,
    mitigates=("CWE-90",),
    confidence=0.95,
)
SanitizerSpec(
    name="spring_ldap_filter_encode",
    pattern=_JAVA_METHOD_FULL_NAME_PATTERN.format(
        class_pattern="LdapEncoder|LdapNameBuilder",
        method="filterEncode|nameEncode",
    ),
    kind=SanitizerKind.ESCAPE,
    mitigates=("CWE-90",),
    confidence=0.95,
)
```

### 5.4 Ground Truth

| ID | File | Label | Pattern |
|----|------|-------|---------|
| gt-162 | `eval/synthetic/ldapi-ldapjs-search.ts` | TP | `client.search(dn, { filter: "(&(uid=" + input + "))" })` |
| gt-163 | `eval/synthetic/ldapi-python-ldap.py` | TP | `conn.search_s(dn, scope, f"(uid={input})")` |
| gt-164 | `eval/synthetic/ldapi-spring-template.java` | TP | `ldapTemplate.search("", filter_concat, mapper)` |
| gt-fp-040 | `eval/synthetic/ldapi-escaped.ts` | FP | `ldap.escape(req.body.user)` before filter concat |
| gt-165 | `eval/synthetic/ldapi-go-ldap.go` | TP | `ldap.NewSearchRequest(... Sprintf("(uid=%s)", input) ...)` |

---

## 6. HTTP Header Injection / Response Splitting (CWE-113)

### 6.1 Threat Model

HTTP header injection occurs when user input is placed into an HTTP response header value without stripping CRLF characters (`\r\n`). An attacker can inject additional headers (including `Set-Cookie`) or split the response entirely.

**Attack example:**
```
Input:  value = "foo\r\nSet-Cookie: admin=true"
Header: X-Custom: foo
        Set-Cookie: admin=true
```

Modern frameworks (Express 4.x+, Go net/http) largely prevent raw CRLF in headers, but older versions and custom servers remain vulnerable. The redirect case (`res.redirect(userInput)`) is still a viable attack vector in many setups.

### 6.2 Sinks

#### 6.2.1 JavaScript/TypeScript

```javascript
// VULNERABLE — raw header set
res.setHeader("X-Custom", req.query.value);   // CWE-113
res.set("X-Forwarded-For", req.body.xff);     // CWE-113
res.writeHead(200, { "X-Custom": req.query.value }); // CWE-113

// VULNERABLE — redirect with CRLF potential
res.redirect(req.query.url);                   // CWE-113 (also CWE-601)
```

**CPGQL:**

```python
SinkSpec(
    name="express_set_header_value",
    pattern='cpg.call.name("setHeader|set|writeHead|header")',
    sink_type=SinkType.HEADER_INJECTION,
    cwe_id="CWE-113",
    severity="medium",
    flow_pattern='cpg.call.name("setHeader|set|writeHead|header").argument(2)',
    flow_to_parent_call=True,
)
```

Note: The existing `fastify_reply_header` sink in `FASTIFY_SINK_SPECS` already covers CWE-113 for Fastify. The new spec covers Express and generic Node.js HTTP.

#### 6.2.2 Python — Django, Flask

```python
# VULNERABLE — Django
from django.http import HttpResponse
response = HttpResponse()
response["X-Custom"] = request.GET["value"]     # CWE-113

# VULNERABLE — Flask
from flask import make_response
resp = make_response("ok")
resp.headers["X-Custom"] = request.args["value"] # CWE-113
```

**CPGQL:**

```python
SinkSpec(
    name="python_response_header_set",
    pattern=_PY_CALL_CODE_PATTERN.format(
        name="__setitem__",
        code=".*(?:response|headers)\\[.*\\].*",
    ),
    sink_type=SinkType.HEADER_INJECTION,
    cwe_id="CWE-113",
    severity="medium",
)
```

#### 6.2.3 Go

```go
// VULNERABLE
w.Header().Set("X-Custom", r.URL.Query().Get("value")) // CWE-113
```

**CPGQL:**

```python
SinkSpec(
    name="go_header_set",
    pattern=_GO_CALL_CODE_PATTERN.format(
        method="Set|Add",
        code=".*Header[(][)][.](?:Set|Add)[(].*",
    ),
    sink_type=SinkType.HEADER_INJECTION,
    cwe_id="CWE-113",
    severity="medium",
)
```

#### 6.2.4 Java

```java
// VULNERABLE
response.setHeader("X-Custom", request.getParameter("value")); // CWE-113
response.addHeader("X-Custom", request.getParameter("value")); // CWE-113
```

**CPGQL:**

```python
SinkSpec(
    name="java_response_set_header",
    pattern=_JAVA_METHOD_FULL_NAME_PATTERN.format(
        class_pattern="HttpServletResponse",
        method="setHeader|addHeader|setIntHeader|setDateHeader",
    ),
    sink_type=SinkType.HEADER_INJECTION,
    cwe_id="CWE-113",
    severity="medium",
)
```

### 6.3 Sanitizers

```python
SanitizerSpec(
    name="crlf_strip",
    pattern='cpg.call.name("replace").code(".*replace[(].*(?:\\\\r|\\\\n|%0[dD]|%0[aA]).*")',
    kind=SanitizerKind.ESCAPE,
    mitigates=("CWE-113",),
    confidence=0.85,
)
SanitizerSpec(
    name="header_value_encode",
    pattern='cpg.call.name("encodeURIComponent|encodeURI|quote")',
    kind=SanitizerKind.ESCAPE,
    mitigates=("CWE-113",),
    confidence=0.7,
)
```

### 6.4 Ground Truth

| ID | File | Label | Pattern |
|----|------|-------|---------|
| gt-166 | `eval/synthetic/header-inj-setHeader.ts` | TP | `res.setHeader("X-Custom", req.query.val)` |
| gt-167 | `eval/synthetic/header-inj-redirect-crlf.ts` | TP | `res.redirect(req.query.url)` without CRLF strip |
| gt-168 | `eval/synthetic/header-inj-django.py` | TP | `response["X-Custom"] = request.GET["val"]` |
| gt-fp-041 | `eval/synthetic/header-inj-crlf-stripped.ts` | FP | `val.replace(/[\r\n]/g, "")` before setHeader |
| gt-169 | `eval/synthetic/header-inj-go-set.go` | TP | `w.Header().Set("X-Custom", r.URL.Query().Get("v"))` |

---

## 7. Expression Language Injection (CWE-917)

### 7.1 Threat Model

Expression Language (EL) injection occurs in Java-based web applications when user input is evaluated as an expression by Spring Expression Language (SpEL), Object-Graph Navigation Language (OGNL — Struts), MVEL, or JBoss EL. Successful exploitation typically leads to RCE.

**Notable CVEs:** CVE-2022-22963 (Spring Cloud Function SpEL), CVE-2017-5638 (Struts2 OGNL), CVE-2018-1273 (Spring Data Commons SpEL).

### 7.2 Sinks

#### 7.2.1 Spring Expression Language (SpEL)

```java
// VULNERABLE — dynamic SpEL evaluation
SpelExpressionParser parser = new SpelExpressionParser();
Expression exp = parser.parseExpression(request.getParameter("expr")); // CWE-917
Object result = exp.getValue();

// VULNERABLE — SpEL in @Value (less common, requires config injection)
// @Value("#{systemProperties['" + input + "']}")
// Usually static, but dynamic template construction is dangerous

// VULNERABLE — Spring Data @Query with SpEL
@Query("SELECT u FROM User u WHERE u.name = ?#{#name}")
// safe if #name is from param, dangerous if from concatenation
```

**CPGQL:**

```python
SinkSpec(
    name="spring_spel_parse_expression",
    pattern=_JAVA_METHOD_FULL_NAME_PATTERN.format(
        class_pattern=".*ExpressionParser|SpelExpressionParser",
        method="parseExpression",
    ),
    sink_type=SinkType.EXPRESSION_INJECTION,
    cwe_id="CWE-917",
    severity="critical",
)
SinkSpec(
    name="spring_spel_evaluation_context",
    pattern=_JAVA_METHOD_FULL_NAME_PATTERN.format(
        class_pattern="Expression",
        method="getValue|setValue",
    ),
    sink_type=SinkType.EXPRESSION_INJECTION,
    cwe_id="CWE-917",
    severity="critical",
)
```

#### 7.2.2 OGNL (Struts2)

```java
// VULNERABLE
OgnlContext ctx = new OgnlContext();
Object result = Ognl.getValue(request.getParameter("expr"), ctx, root); // CWE-917
```

**CPGQL:**

```python
SinkSpec(
    name="ognl_get_value",
    pattern=_JAVA_METHOD_FULL_NAME_PATTERN.format(
        class_pattern="Ognl",
        method="getValue|setValue|parseExpression",
    ),
    sink_type=SinkType.EXPRESSION_INJECTION,
    cwe_id="CWE-917",
    severity="critical",
)
```

#### 7.2.3 MVEL

```java
// VULNERABLE
Object result = MVEL.eval(request.getParameter("expr")); // CWE-917
```

**CPGQL:**

```python
SinkSpec(
    name="mvel_eval",
    pattern=_JAVA_METHOD_FULL_NAME_PATTERN.format(
        class_pattern="MVEL",
        method="eval|compileExpression|executeExpression",
    ),
    sink_type=SinkType.EXPRESSION_INJECTION,
    cwe_id="CWE-917",
    severity="critical",
)
```

#### 7.2.4 JBoss / Jakarta EL

```java
// VULNERABLE
ExpressionFactory factory = ExpressionFactory.newInstance();
ValueExpression ve = factory.createValueExpression(
    context, "${" + request.getParameter("expr") + "}", Object.class
); // CWE-917
```

**CPGQL:**

```python
SinkSpec(
    name="jakarta_el_create_expression",
    pattern=_JAVA_METHOD_FULL_NAME_PATTERN.format(
        class_pattern="ExpressionFactory",
        method="createValueExpression|createMethodExpression",
    ),
    sink_type=SinkType.EXPRESSION_INJECTION,
    cwe_id="CWE-917",
    severity="critical",
)
```

#### 7.2.5 Angular SSR Expressions (edge case)

When using Angular Universal (SSR), expressions in templates rendered server-side with user data can be dangerous, though Angular's strict template compilation mitigates most cases. Include as low-confidence finding only.

### 7.3 Sanitizers

```python
SanitizerSpec(
    name="spel_simple_evaluation_context",
    pattern='cpg.call.name("<operator>.new").code("new SimpleEvaluationContext.*")',
    kind=SanitizerKind.NORMALIZE,
    mitigates=("CWE-917",),
    confidence=0.8, # SimpleEvaluationContext restricts available types
)
SanitizerSpec(
    name="spel_allowlist",
    pattern='cpg.call.name("setRootObject|setVariable").code(".*(?:setRootObject|setVariable).*")',
    kind=SanitizerKind.NORMALIZE,
    mitigates=("CWE-917",),
    confidence=0.5, # only effective if root object is controlled
)
```

### 7.4 Z3 Constraints

```python
el_spel = z3.Or(
    z3.Contains(payload, z3.StringVal("T(")),           # SpEL type reference
    z3.Contains(payload, z3.StringVal("Runtime")),       # java.lang.Runtime
    z3.Contains(payload, z3.StringVal("ProcessBuilder")),
    z3.Contains(payload, z3.StringVal("#{")),            # EL expression delimiter
)
el_ognl = z3.Or(
    z3.Contains(payload, z3.StringVal("@java.lang")),
    z3.Contains(payload, z3.StringVal("#_memberAccess")),
    z3.Contains(payload, z3.StringVal("#context")),
)
el_constraint = z3.Or(el_spel, el_ognl)
```

### 7.5 Ground Truth

| ID | File | Label | Pattern |
|----|------|-------|---------|
| gt-170 | `eval/synthetic/eli-spel-parse.java` | TP | `parser.parseExpression(request.getParameter("expr"))` |
| gt-171 | `eval/synthetic/eli-ognl-getvalue.java` | TP | `Ognl.getValue(request.getParameter("expr"), ctx, root)` |
| gt-172 | `eval/synthetic/eli-mvel-eval.java` | TP | `MVEL.eval(request.getParameter("expr"))` |
| gt-173 | `eval/synthetic/eli-jakarta-el.java` | TP | `factory.createValueExpression(ctx, "${" + input + "}", ...)` |
| gt-fp-042 | `eval/synthetic/eli-spel-simple-ctx.java` | FP | `SimpleEvaluationContext` restricts available types |

---

## 8. XPath Injection (CWE-643)

### 8.1 Threat Model

XPath injection occurs when user input is concatenated into an XPath query string. Attackers can modify the query to extract additional XML data or bypass authentication.

**Attack example:**
```
Input:  username = "' or '1'='1"
Query:  //user[name/text()='' or '1'='1' and password/text()='anything']
```

### 8.2 Sinks

#### 8.2.1 JavaScript/TypeScript

```javascript
// VULNERABLE
const xpath = require("xpath");
const dom = require("xmldom").DOMParser;
const doc = new dom().parseFromString(xmlString);
const query = `//user[name/text()='${req.body.username}']`;
const nodes = xpath.select(query, doc); // CWE-643
```

```javascript
// VULNERABLE — browser/JSDOM
const result = document.evaluate(
    `//user[name='${input}']`, doc, null, XPathResult.ANY_TYPE, null
); // CWE-643
```

**CPGQL:**

```python
SinkSpec(
    name="xpath_select",
    pattern='cpg.call.name("select|select1|evaluate").code(".*xpath[.]select.*|.*document[.]evaluate.*")',
    sink_type=SinkType.XPATH_QUERY,
    cwe_id="CWE-643",
    severity="high",
)
SinkSpec(
    name="xpath_string_concat",
    pattern=(
        'cpg.call.name("<operator>.addition|<operator>.formatString")'
        '.where(_.code(".*(?://|/child::|/descendant::|\\[.*=).*"))'
    ),
    sink_type=SinkType.XPATH_QUERY,
    cwe_id="CWE-643",
    severity="high",
)
```

#### 8.2.2 Python — lxml / xml.etree

```python
# VULNERABLE
from lxml import etree
tree = etree.parse("users.xml")
result = tree.xpath(f"//user[name='{request.form['username']}']") # CWE-643

# VULNERABLE — xml.etree
import xml.etree.ElementTree as ET
tree = ET.parse("users.xml")
root = tree.getroot()
root.findall(f".//user[@name='{request.args['name']}']") # CWE-643
```

**CPGQL:**

```python
SinkSpec(
    name="python_lxml_xpath",
    pattern=_PY_CALL_CODE_PATTERN.format(
        name="xpath",
        code=".*[.]xpath[(].*",
    ),
    sink_type=SinkType.XPATH_QUERY,
    cwe_id="CWE-643",
    severity="high",
)
SinkSpec(
    name="python_etree_findall",
    pattern=_PY_CALL_CODE_PATTERN.format(
        name="findall|find|iterfind",
        code=".*[.](?:findall|find|iterfind)[(].*",
    ),
    sink_type=SinkType.XPATH_QUERY,
    cwe_id="CWE-643",
    severity="medium",
)
```

#### 8.2.3 Java — javax.xml.xpath

```java
// VULNERABLE
XPathFactory xpf = XPathFactory.newInstance();
XPath xpath = xpf.newXPath();
String expr = "//user[name/text()='" + request.getParameter("username") + "']";
XPathExpression xpe = xpath.compile(expr); // CWE-643
NodeList nodes = (NodeList) xpe.evaluate(doc, XPathConstants.NODESET);
```

**CPGQL:**

```python
SinkSpec(
    name="java_xpath_compile",
    pattern=_JAVA_METHOD_FULL_NAME_PATTERN.format(
        class_pattern="XPath",
        method="compile|evaluate",
    ),
    sink_type=SinkType.XPATH_QUERY,
    cwe_id="CWE-643",
    severity="high",
)
```

#### 8.2.4 Go — xmlpath / etree

```go
// VULNERABLE
import "github.com/antchfx/xmlquery"
expr := fmt.Sprintf("//user[name='%s']", r.FormValue("username"))
nodes, _ := xmlquery.QueryAll(doc, expr) // CWE-643
```

**CPGQL:**

```python
SinkSpec(
    name="go_xpath_query",
    pattern=_GO_CALL_CODE_PATTERN.format(
        method="QueryAll|Query|Find|FindOne",
        code=".*(?:xmlquery|xpath)[.](?:QueryAll|Query|Find|FindOne)[(].*",
    ),
    sink_type=SinkType.XPATH_QUERY,
    cwe_id="CWE-643",
    severity="high",
)
```

### 8.3 Sanitizers

```python
SanitizerSpec(
    name="xpath_parameterized",
    pattern='cpg.call.name("XPathVariable|setVariable|addVariable")',
    kind=SanitizerKind.PARAMETERIZE,
    mitigates=("CWE-643",),
    confidence=0.95,
)
SanitizerSpec(
    name="xpath_input_validation",
    pattern='cpg.call.name("replace").code(".*replace[(].*[\\x27\\x22].*")',
    kind=SanitizerKind.ESCAPE,
    mitigates=("CWE-643",),
    confidence=0.6, # quote escaping is fragile
)
```

### 8.4 Ground Truth

| ID | File | Label | Pattern |
|----|------|-------|---------|
| gt-174 | `eval/synthetic/xpathi-xpath-select.ts` | TP | `xpath.select("//user[name='" + input + "']", doc)` |
| gt-175 | `eval/synthetic/xpathi-lxml-xpath.py` | TP | `tree.xpath(f"//user[name='{input}']")` |
| gt-176 | `eval/synthetic/xpathi-java-compile.java` | TP | `xpath.compile("//user[name='" + input + "']")` |
| gt-fp-043 | `eval/synthetic/xpathi-parameterized.java` | FP | XPath with `XPathVariable` resolver |

---

## 9. Implementation Notes

### 9.1 SinkType Enum Extensions

Add to `SinkType` in `scan/specs.py`:

```python
class SinkType(StrEnum):
    # ... existing members ...
    NOSQL_QUERY = "nosql_query"
    TEMPLATE_INJECTION = "template_injection"
    REGEX_INJECTION = "regex_injection"
    LDAP_QUERY = "ldap_query"
    EXPRESSION_INJECTION = "expression_injection"
    XPATH_QUERY = "xpath_query"
    # HEADER_INJECTION already exists
```

### 9.2 Severity Mapping

Add to `_SEVERITY_BY_CWE` in `detect/flows.py`:

```python
_SEVERITY_BY_CWE = {
    # ... existing entries ...
    "CWE-943": "high",
    "CWE-1336": "critical",
    "CWE-1333": "medium",
    "CWE-90": "high",
    "CWE-113": "medium",
    "CWE-917": "critical",
    "CWE-643": "high",
}
```

### 9.3 Sanitizer Effectiveness Matrix Extension

Add to `SANITIZER_EFFECTIVENESS` in `detect/sanitizer_validation.py`:

```python
SANITIZER_EFFECTIVENESS: dict[str, dict[str, SanitizerEffectiveness]] = {
    # ... existing entries ...
    # NoSQL sanitizers
    "mongo_sanitize": _effectiveness_map(effective=("CWE-943",)),
    "nosql_field_pick": _effectiveness_map(partial=("CWE-943",)),
    # SSTI sanitizers
    "template_sandbox": _effectiveness_map(partial=("CWE-1336",)),
    "template_file_loader": _effectiveness_map(effective=("CWE-1336",)),
    # ReDoS sanitizers
    "regex_escape": _effectiveness_map(effective=("CWE-1333",)),
    "python_re_escape": _effectiveness_map(effective=("CWE-1333",)),
    "regex_timeout": _effectiveness_map(partial=("CWE-1333",)),
    # LDAP sanitizers
    "ldap_escape": _effectiveness_map(effective=("CWE-90",)),
    "python_ldap_filter_escape": _effectiveness_map(effective=("CWE-90",)),
    "spring_ldap_filter_encode": _effectiveness_map(effective=("CWE-90",)),
    # Header injection sanitizers
    "crlf_strip": _effectiveness_map(effective=("CWE-113",)),
    "header_value_encode": _effectiveness_map(partial=("CWE-113",)),
    # EL injection sanitizers
    "spel_simple_evaluation_context": _effectiveness_map(partial=("CWE-917",)),
    # XPath sanitizers
    "xpath_parameterized": _effectiveness_map(effective=("CWE-643",)),
    "xpath_input_validation": _effectiveness_map(partial=("CWE-643",)),
}
```

Also update `_COMMON_INJECTION_CWES` to include the new CWEs where JSON schema validation provides partial mitigation:

```python
_COMMON_INJECTION_CWES = ("CWE-89", "CWE-79", "CWE-78", "CWE-22", "CWE-918", "CWE-943", "CWE-90", "CWE-643")
```

### 9.4 detect/injection_variants.py

New module providing shared detection helpers:

```python
"""Shared detection helpers for injection variant CWEs (943, 1336, 90, 113, 917, 643).

These CWEs all follow the standard taint analysis pattern (source -> sink)
but require additional context-sensitive logic for:
  - NoSQL: distinguishing scalar field value (safe) from operator position (dangerous)
  - SSTI: distinguishing template source (dangerous) from template context (safe)
  - LDAP: detecting string concatenation into filter position
  - Header injection: verifying CRLF absence/presence in flow
"""

from __future__ import annotations
from dataclasses import dataclass
from piranesi.models import TaintStep

def is_nosql_operator_position(flow_path: list[TaintStep]) -> bool: ...
def is_template_source_position(flow_path: list[TaintStep]) -> bool: ...
def is_ldap_filter_concat(flow_path: list[TaintStep]) -> bool: ...
def is_header_value_position(flow_path: list[TaintStep]) -> bool: ...
```

### 9.5 detect/redos.py

New module for ReDoS detection — standalone from taint infrastructure:

```python
"""ReDoS detection via NFA state analysis.

Two modes:
1. Static: scan regex literals for catastrophic backtracking patterns.
2. Taint: detect user input flowing to RegExp constructor (handled by taint infra).

Static analysis uses star height computation:
- Parse regex into AST
- Compute star height (max nesting depth of quantifiers)
- If star_height >= 2, check overlapping branches
- If overlap found, report as CWE-1333
"""

from __future__ import annotations
from dataclasses import dataclass, field
from pathlib import Path

@dataclass(frozen=True, slots=True)
class RegexVulnerability:
    pattern: str
    file_path: str
    line_number: int
    vuln_type: str  # "nested_quantifier" | "overlapping_alternation" | "backreference_quantifier"
    star_height: int
    confidence: float

class RedosAnalyzer:
    def analyze_regex(self, pattern: str) -> RegexVulnerability | None: ...
    def scan_cpg(self, cpg_nodes: list) -> list[RegexVulnerability]: ...
    def _parse_regex(self, pattern: str) -> RegexAST: ...
    def _compute_star_height(self, node: RegexAST) -> int: ...
    def _has_overlapping_branches(self, node: RegexAST) -> bool: ...
    def _simulate_adversarial_input(self, nfa: NFA, max_steps: int = 10000) -> bool: ...
```

### 9.6 Pipeline Integration

In `pipeline.py`, add ReDoS as a sub-stage after taint flow extraction:

```python
# in run_detect_stage()
findings = extract_taint_findings(...)
# add ReDoS static findings
redos_findings = RedosAnalyzer().scan_cpg(cpg_regex_nodes)
findings.extend(_redos_to_candidate_findings(redos_findings))
```

### 9.7 Total New Specs Summary

| Category | New SinkSpecs | New SanitizerSpecs | New SinkType Members |
|----------|:---:|:---:|:---:|
| NoSQL Injection | 12 (JS 5, Py 3, Go 2, Java 2) | 3 (+ 1 modified) | `NOSQL_QUERY` |
| SSTI | 10 (JS 4, Py 4, Java 3) | 2 | `TEMPLATE_INJECTION` |
| ReDoS | 3 (JS 1, Py 1, Java 1 — taint mode) | 3 | `REGEX_INJECTION` |
| LDAP | 5 (JS 2, Py 1, Go 1, Java 1) | 3 | `LDAP_QUERY` |
| Header Injection | 4 (JS 1, Py 1, Go 1, Java 1) | 2 | (existing) |
| EL Injection | 5 (all Java) | 2 | `EXPRESSION_INJECTION` |
| XPath Injection | 6 (JS 2, Py 2, Java 1, Go 1) | 2 | `XPATH_QUERY` |
| **Total** | **45** | **17 (+ 1 modified)** | **6 new** |

### 9.8 Ground Truth Summary

| CWE | TP | FP (safe) | Total |
|-----|:---:|:---:|:---:|
| CWE-943 | 6 | 2 | 8 |
| CWE-1336 | 5 | 3 | 8 |
| CWE-1333 | 6 | 2 | 8 |
| CWE-90 | 4 | 1 | 5 |
| CWE-113 | 4 | 1 | 5 |
| CWE-917 | 4 | 1 | 5 |
| CWE-643 | 3 | 1 | 4 |
| **Total** | **32** | **11** | **43** |

---

## 10. Testing Strategy

### 10.1 Fixture Organization

```
tests/fixtures/
├── typescript/
│   ├── nosqli/
│   │   ├── mongoose_find_vuln.ts
│   │   ├── mongodb_findone_vuln.ts
│   │   ├── mongo_sanitized_safe.ts
│   │   └── schema_validated_safe.ts
│   ├── ssti/
│   │   ├── ejs_render_vuln.ts
│   │   ├── handlebars_compile_vuln.ts
│   │   ├── nunjucks_renderstring_vuln.ts
│   │   └── ejs_context_safe.ts
│   ├── redos/
│   │   ├── nested_quantifier_vuln.ts
│   │   ├── overlapping_alt_vuln.ts
│   │   ├── regexp_injection_vuln.ts
│   │   └── linear_regex_safe.ts
│   ├── ldapi/
│   │   ├── ldapjs_search_vuln.ts
│   │   └── ldapjs_escaped_safe.ts
│   ├── header_inj/
│   │   ├── setHeader_vuln.ts
│   │   └── crlf_stripped_safe.ts
│   └── xpathi/
│       ├── xpath_select_vuln.ts
│       └── parameterized_safe.ts
├── python/
│   ├── nosqli/
│   │   └── pymongo_find_vuln.py
│   ├── ssti/
│   │   ├── jinja2_template_vuln.py
│   │   ├── mako_template_vuln.py
│   │   └── jinja2_file_safe.py
│   ├── redos/
│   │   └── re_compile_vuln.py
│   ├── ldapi/
│   │   └── python_ldap_vuln.py
│   ├── header_inj/
│   │   └── django_header_vuln.py
│   └── xpathi/
│       └── lxml_xpath_vuln.py
├── java/
│   ├── nosqli/
│   │   └── spring_basicquery_vuln.java
│   ├── ssti/
│   │   ├── thymeleaf_process_vuln.java
│   │   └── velocity_evaluate_vuln.java
│   ├── eli/
│   │   ├── spel_parse_vuln.java
│   │   ├── ognl_getvalue_vuln.java
│   │   ├── mvel_eval_vuln.java
│   │   └── spel_simple_ctx_safe.java
│   └── xpathi/
│       ├── xpath_compile_vuln.java
│       └── xpath_variable_safe.java
└── go/
    ├── nosqli/
    │   └── mongo_find_vuln.go
    ├── ldapi/
    │   └── go_ldap_vuln.go
    ├── header_inj/
    │   └── header_set_vuln.go
    └── xpathi/
        └── xmlquery_vuln.go
```

### 10.2 Test Cases per CWE

#### CWE-943 (NoSQL Injection)

```python
# tests/test_detect/test_nosql_injection.py

def test_mongoose_find_with_raw_body_detected():
    """User.find(req.body) — entire body as query filter."""
    
def test_mongodb_findone_with_field_detected():
    """collection.findOne({ email: req.body.email }) — object in field position."""
    
def test_mongodb_where_concat_detected():
    """find({ $where: '...' + input }) — $where with string concat."""
    
def test_mongodb_regex_user_input_detected():
    """find({ name: { $regex: req.query.q } }) — user-controlled regex."""
    
def test_mongo_sanitize_suppresses():
    """mongoSanitize(req.body) before find should suppress finding."""
    
def test_schema_validated_body_suppresses():
    """Joi.validate(req.body, schema) before find should suppress."""
    
def test_pymongo_find_detected():
    """db.users.find(request.json) in Python."""
    
def test_spring_basicquery_detected():
    """new BasicQuery(request.getParameter('q')) in Java."""
```

#### CWE-1336 (SSTI)

```python
# tests/test_detect/test_ssti.py

def test_ejs_render_user_template_detected():
    """ejs.render(req.body.template) — user controls template source."""
    
def test_handlebars_compile_user_template_detected():
    """Handlebars.compile(req.body.tpl)."""
    
def test_jinja2_template_constructor_detected():
    """Template(request.form['tpl']).render() in Python."""
    
def test_ejs_render_with_context_safe():
    """ejs.render(staticTemplate, { name: req.body.name }) — user in context = safe."""
    
def test_jinja2_file_loader_safe():
    """env.get_template('page.html').render(name=input) — file template = safe."""
```

#### CWE-1333 (ReDoS)

```python
# tests/test_detect/test_redos.py

def test_nested_quantifier_detected():
    """/^(a+)+$/ — star height 2."""
    
def test_overlapping_alternation_detected():
    """/^(\\w+|\\d+)+$/ — overlapping character classes."""
    
def test_regexp_injection_detected():
    """new RegExp(req.query.q) — user controls regex."""
    
def test_linear_regex_safe():
    """/^[a-z]+$/ — star height 1, no overlap."""
    
def test_escaped_input_safe():
    """new RegExp(escapeRegExp(req.query.q)) — escaped input."""
    
def test_redos_analyzer_star_height():
    """Unit test: star_height('(a+)+') == 2."""
    
def test_redos_analyzer_overlap():
    """Unit test: overlapping branches in '(\\w|\\d)+'."""
```

#### CWE-90, 113, 917, 643

Follow the same pattern: one test per TP ground truth entry, one test per FP ground truth entry, plus unit tests for any custom detection logic.

### 10.3 Cross-Framework Coverage Matrix

Test matrix verifying each CWE is tested across all applicable language/framework combinations:

| CWE | Express | Fastify | NestJS | Flask | Django | FastAPI | Spring | Gin | Echo |
|-----|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|
| 943 | Y | Y | Y | Y | Y | Y | Y | Y | - |
| 1336 | Y | Y | - | Y | Y | - | Y | - | - |
| 1333 | Y | Y | Y | Y | Y | Y | Y | - | - |
| 90 | Y | - | - | Y | - | - | Y | Y | - |
| 113 | Y | Y* | - | Y | Y | - | Y | Y | - |
| 917 | - | - | - | - | - | - | Y | - | - |
| 643 | Y | - | - | Y | - | - | Y | Y | - |

`*` = already covered by existing `fastify_reply_header` spec.
`-` = not applicable or extremely uncommon in that framework.

---

## 11. Milestones

| # | Milestone | Deliverable | Est. Hours |
|---|-----------|-------------|:---:|
| 27.1 | SinkType + severity scaffolding | Enum members, `_SEVERITY_BY_CWE` entries, sanitizer matrix extensions | 3-4 |
| 27.2 | NoSQL injection specs + detection | SinkSpec/SanitizerSpec entries, `is_nosql_operator_position()`, fixtures, tests, 8 GT | 12-15 |
| 27.3 | SSTI specs + detection | SinkSpec/SanitizerSpec entries, `is_template_source_position()`, fixtures, tests, 8 GT | 10-14 |
| 27.4 | ReDoS analyzer | `detect/redos.py` NFA analysis, regex extraction CPGQL, taint specs, pipeline integration, fixtures, tests, 8 GT | 12-16 |
| 27.5 | LDAP injection specs | SinkSpec/SanitizerSpec entries, fixtures, tests, 5 GT | 6-8 |
| 27.6 | Header injection specs | SinkSpec/SanitizerSpec entries (extend existing), fixtures, tests, 5 GT | 6-8 |
| 27.7 | EL injection specs | SinkSpec/SanitizerSpec entries (Java-only), fixtures, tests, 5 GT | 8-10 |
| 27.8 | XPath injection specs | SinkSpec/SanitizerSpec entries, fixtures, tests, 4 GT | 5-7 |
| 27.9 | Integration + regression | Full scan regression on existing fixtures (no regressions), cross-CWE interaction tests | 3-4 |

---

## 12. Risks and Mitigations

| Risk | Impact | Likelihood | Mitigation |
|------|--------|:---:|-----------|
| NoSQL FP from scalar field values | Many false positives for safe `find({ field: string })` patterns | High | `is_nosql_operator_position()` discriminator; only flag when whole body or operator-position object reaches filter |
| SSTI FP from context variable usage | False positive when user data is in template context (safe), not template source | Medium | Argument position analysis: only flag argument(1) flows to template constructor/render, not argument(2+) |
| ReDoS FP from defensive regex | Safe regex falsely flagged as vulnerable | Low | Star height threshold (>= 2) + overlap confirmation + adversarial simulation |
| Header injection FP on modern frameworks | Express 4.x+ strips CRLF automatically | Medium | Framework-version-aware confidence reduction; lower severity to `low` for Express 4.x+ |
| EL injection limited to Java | Limited language coverage | N/A | Document as Java-specific; monitor for analogous patterns in other languages |
| Go regex engine is safe | Go's `regexp` uses Thompson NFA (no backtracking) | N/A | Skip ReDoS static analysis for Go `.go` files; still flag `regexp2` or CGo calls |
| Sink name collision with existing specs | `find`, `search`, `evaluate` are generic method names | High | Use `.code()` filter to scope by receiver object pattern (e.g., `collection.find`, `xpath.select`) |
