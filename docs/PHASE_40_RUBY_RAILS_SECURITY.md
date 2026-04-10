# Phase 40: Ruby / Rails Web Security Analysis

**Estimated effort: 55-70 ideal hours**
**Blocked by: Phase 13 (multi-language architecture), Phase 22 (advanced taint analysis)**
**Blocks: Nothing (incremental value)**
**Target milestone: v0.7.0**

---

## 1. Overview

### 1.1 Motivation

Ruby on Rails powers major web applications including GitHub, Shopify, Basecamp, Airbnb, and Stripe. Rails' convention-over-configuration philosophy introduces framework-specific security patterns (Strong Parameters, CSRF protection, auto-escaping) that a SAST tool must understand to avoid both false positives (flagging safe Rails conventions) and false negatives (missing Rails-specific bypasses).

### 1.2 Joern Ruby Support

Joern supports Ruby via the `rubysrc2cpg` frontend, which parses Ruby source into the Code Property Graph. Registration in Piranesi:

1. Add `ruby` -> `rubysrc2cpg` in `LANGUAGE_TO_JOERN_FRONTEND`
2. Language detection from `*.rb`, `Gemfile`, `config/routes.rb`
3. Ruby/Rails-specific source/sink/sanitizer specifications
4. Framework-specific detection for Rails, Sinatra, Hanami

### 1.3 Detection Strategy

Rails applications exhibit strong framework coupling — most security vulnerabilities arise from misuse of framework APIs rather than raw library calls. Detection priorities:

1. **Strong Parameters bypass** — mass assignment without `permit()` or with `permit!`
2. **SQL injection via ActiveRecord** — string interpolation in `.where()`, `.order()`, `.find_by_sql()`
3. **XSS via `raw` / `html_safe`** — bypassing Rails' default auto-escaping
4. **Command injection** — Ruby's rich shell interface (backticks, `system`, `exec`, `IO.popen`)
5. **Insecure deserialization** — `Marshal.load`, `YAML.load` (pre-Psych safe_load)

### 1.4 Language / Framework Detection

| Signal | Framework | Detection Source |
|--------|-----------|-----------------|
| `*.rb` | Ruby | File extension |
| `Gemfile` with `gem 'rails'` | Rails | Gem dependency |
| `config/routes.rb`, `app/controllers/` | Rails | Directory structure |
| `Gemfile` with `gem 'sinatra'` | Sinatra | Gem dependency |
| `Gemfile` with `gem 'hanami'` | Hanami | Gem dependency |
| `Rakefile`, `config.ru` | Rack-based app | Configuration files |
| `bin/rails` | Rails | CLI entry point |

---

## 2. Ruby Source/Sink Specifications

**Estimated effort: 12-15h**

### 2.1 Sources (User-Controlled Input)

#### 2.1.1 Rails Sources

| Source | Description | Taint Type |
|--------|-------------|------------|
| `params` | All request parameters (GET, POST, route) | `request_param` |
| `params[:key]` | Specific parameter | `request_param` |
| `params.permit(:key)` | Permitted parameter (still user-controlled) | `request_param` |
| `request.body` | Raw request body | `request_body` |
| `request.body.read` | Read raw body | `request_body` |
| `request.raw_post` | Raw POST data | `request_body` |
| `request.headers` | HTTP headers hash | `request_header` |
| `request.headers['X-Custom']` | Specific header | `request_header` |
| `cookies` | Cookie hash | `cookie` |
| `cookies[:key]` | Specific cookie | `cookie` |
| `request.env` | Rack environment hash | `request_header` |
| `request.referer` | HTTP Referer | `request_header` |
| `request.user_agent` | User-Agent string | `request_header` |
| `request.remote_ip` | Client IP (spoofable via X-Forwarded-For) | `request_header` |
| `request.url` | Full request URL | `request_param` |
| `request.path` | Request path | `request_param` |

#### 2.1.2 Sinatra Sources

| Source | Description |
|--------|-------------|
| `params` | Merged GET/POST/route parameters |
| `request.body.read` | Raw body |
| `request.env` | Rack env |
| `request.cookies` | Cookies |

#### 2.1.3 Environment Sources

| Source | Description |
|--------|-------------|
| `ENV['KEY']` | Environment variable (may be user-controlled in some contexts) |
| `STDIN.read` / `gets` | Standard input |

### 2.2 Sinks by CWE

#### 2.2.1 CWE-89: SQL Injection

| Sink | Context | Risk |
|------|---------|------|
| `ActiveRecord::Base.find_by_sql("...")` | Raw SQL | String interpolation |
| `.where("name = '#{params[:name]}'")` | String interpolation in where | Direct injection |
| `.where("name = '" + params[:name] + "'")` | Concatenation in where | Direct injection |
| `.order(params[:sort])` | User-controlled ORDER BY | SQL injection in ORDER clause |
| `.order("#{params[:col]} #{params[:dir]}")` | Interpolated order | Direct injection |
| `.group(params[:group])` | User-controlled GROUP BY | SQL injection |
| `.having(params[:having])` | User-controlled HAVING | SQL injection |
| `.pluck(params[:column])` | User-controlled column name | Column name injection |
| `.select(params[:fields])` | User-controlled SELECT | SQL injection |
| `.joins(params[:table])` | User-controlled JOIN | SQL injection |
| `.from(params[:table])` | User-controlled FROM | SQL injection |
| `connection.execute(sql)` | Raw SQL execution | Direct concatenation |
| `connection.exec_query(sql)` | Raw SQL execution | Direct concatenation |
| `ActiveRecord::Base.connection.select_all(sql)` | Raw select | Direct concatenation |
| `Sequel::Database#run(sql)` | Sequel raw SQL | Direct concatenation |
| `Sequel::Database#fetch(sql)` | Sequel raw fetch | Direct concatenation |

**Safe patterns (not sinks):**
- `.where(name: params[:name])` — hash condition (parameterized)
- `.where("name = ?", params[:name])` — array condition (parameterized)
- `.where(["name = ?", params[:name]])` — array condition (parameterized)
- `.order(:name)` — symbol (column reference, safe)
- `.order(Arel.sql(sanitize_sql(params[:sort])))` — sanitized

#### 2.2.2 CWE-79: Cross-Site Scripting

| Sink | Context | Risk |
|------|---------|------|
| `raw(user_input)` | ERB helper bypassing escaping | Unescaped output |
| `.html_safe` | Mark string as safe (bypass escaping) | Unescaped output |
| `<%= raw user_input %>` | ERB raw output | Unescaped output |
| `content_tag(:div, user_input, escape: false)` | Content tag without escaping | Unescaped output |
| `concat(user_input.html_safe)` | String concatenation bypassing escaping | Unescaped output |
| `render inline: user_input` | Inline template rendering | Template injection |
| `send_data user_input, type: 'text/html'` | Sending raw HTML data | Unescaped output |
| `response.body = user_input` | Direct body assignment | Unescaped output |

**Safe patterns:**
- `<%= user_input %>` — ERB auto-escapes by default (Rails 3+)
- `content_tag(:div, user_input)` — auto-escapes content
- `sanitize(user_input)` — Rails sanitizer applied

#### 2.2.3 CWE-78: Command Injection

| Sink | Context | Risk |
|------|---------|------|
| `system(cmd)` | System call | Direct execution |
| `system(cmd, arg)` | System with separate args | Safer but cmd still controlled |
| `` `#{cmd}` `` | Backtick interpolation | Shell execution |
| `exec(cmd)` | Exec (replaces process) | Direct execution |
| `%x(#{cmd})` | Percent literal | Shell execution |
| `IO.popen(cmd)` | Pipe open | Shell execution |
| `Open3.capture3(cmd)` | Open3 capture | Shell execution |
| `Open3.popen3(cmd)` | Open3 popen | Shell execution |
| `Kernel.spawn(cmd)` | Process spawn | Shell execution |
| `Process.spawn(cmd)` | Process spawn | Shell execution |
| `PTY.spawn(cmd)` | PTY spawn | Shell execution |
| `Shellwords.shellescape` then `system()` | Escaped then executed | SAFE if `shellescape` applied |

#### 2.2.4 CWE-22: Path Traversal

| Sink | Context | Risk |
|------|---------|------|
| `File.read(params[:path])` | File read | User-controlled path |
| `File.open(params[:path])` | File open | User-controlled path |
| `File.write(params[:path], data)` | File write | User-controlled path |
| `File.delete(params[:path])` | File delete | User-controlled path |
| `FileUtils.cp(params[:src], params[:dst])` | File copy | User-controlled paths |
| `send_file(params[:file])` | Download file | User-controlled path |
| `render file: params[:template]` | Render arbitrary file | File disclosure |
| `IO.read(params[:path])` | IO read | User-controlled path |
| `Pathname.new(params[:path]).read` | Pathname read | User-controlled path |
| `Dir.glob(params[:pattern])` | Directory glob | User-controlled pattern |

#### 2.2.5 CWE-915: Mass Assignment

| Sink | Context | Risk |
|------|---------|------|
| `User.new(params[:user])` | Create without permit | All attributes assignable |
| `User.create(params[:user])` | Create without permit | All attributes assignable |
| `user.update(params[:user])` | Update without permit | All attributes assignable |
| `user.update_attributes(params[:user])` | Legacy update | All attributes assignable |
| `User.new(params.permit!)` | Permit all | All attributes assignable |
| `user.assign_attributes(params[:user])` | Assign without permit | All attributes assignable |

**Safe patterns:**
- `User.new(params.require(:user).permit(:name, :email))` — Strong Parameters
- `user.update(user_params)` where `user_params` is defined via `params.require(...).permit(...)` — safe

#### 2.2.6 CWE-502: Insecure Deserialization

| Sink | Context | Risk |
|------|---------|------|
| `Marshal.load(data)` | Ruby native deserialization | Arbitrary object instantiation, RCE |
| `Marshal.restore(data)` | Alias for Marshal.load | Same risk |
| `YAML.load(data)` | YAML deserialization (Ruby < 3.1 default unsafe) | Object instantiation via `!ruby/object` |
| `YAML.unsafe_load(data)` | Explicit unsafe YAML load | Object instantiation |
| `Oj.load(data, mode: :object)` | Oj JSON with object mode | Object instantiation |
| `JSON.parse(data, create_additions: true)` | JSON with custom class creation | Object instantiation |

**Safe alternatives:**
- `YAML.safe_load(data)` — restricted to basic types
- `JSON.parse(data)` — default mode is safe
- `Oj.load(data, mode: :strict)` — strict mode safe

---

## 3. Rails-Specific Detection

**Estimated effort: 18-22h**

### 3.1 Strong Parameters Analysis

Strong Parameters is Rails' primary defense against mass assignment. Analysis must verify correct usage:

#### 3.1.1 Permit Validation

```ruby
# SAFE: explicit permit
def user_params
  params.require(:user).permit(:name, :email)
end

# UNSAFE: permit! allows all
def user_params
  params.require(:user).permit!
end

# UNSAFE: params used directly
def create
  User.create(params[:user])
end
```

**Detection:**
1. Find all controller actions that create/update ActiveRecord models
2. Trace the parameter argument back to its definition
3. Verify it passes through `.permit()` with an explicit allowlist
4. Flag `.permit!` as CWE-915 (permits all attributes)
5. Flag direct `params[:model]` usage without `.permit()` as CWE-915

#### 3.1.2 Nested Attributes

```ruby
# watch for overly permissive nested permit
params.require(:user).permit(:name, addresses_attributes: [:id, :street, :_destroy])
```

Flag if nested attributes include sensitive fields (`role`, `admin`, `password_digest`).

### 3.2 CSRF Protection Analysis

#### 3.2.1 ApplicationController Check

```ruby
class ApplicationController < ActionController::Base
  protect_from_forgery with: :exception  # SAFE: CSRF enabled
end

class ApplicationController < ActionController::Base
  # UNSAFE: no CSRF protection declared
end
```

**Detection:**
1. Parse `app/controllers/application_controller.rb`
2. Verify `protect_from_forgery` is called
3. Flag `skip_before_action :verify_authenticity_token` in controllers handling state-changing operations
4. Flag controllers inheriting from `ActionController::API` that handle browser requests (should use `ActionController::Base`)

#### 3.2.2 API vs Browser Controllers

Rails API controllers (`ActionController::API`) intentionally skip CSRF. This is safe for pure API endpoints but dangerous if the controller serves browser requests. Heuristic: if the controller renders HTML views or uses session cookies, flag missing CSRF.

### 3.3 SQL Injection in ActiveRecord

#### 3.3.1 String vs Hash vs Array Conditions

| Pattern | Safety | Example |
|---------|--------|---------|
| Hash condition | Safe | `.where(name: params[:name])` |
| Array condition | Safe | `.where("name = ?", params[:name])` |
| String condition (no interpolation) | Safe | `.where("active = true")` |
| String with interpolation | Unsafe | `.where("name = '#{params[:name]}'")` |
| String with concatenation | Unsafe | `.where("name = '" + params[:name] + "'")` |

**Detection:** For each `.where()`, `.order()`, `.group()`, `.having()`, `.select()`, `.joins()`, `.from()` call:
1. Check argument type from AST (string literal, hash literal, array literal, variable)
2. If string literal with `#{}` interpolation containing user input, flag CWE-89
3. If variable, trace back — if it's a string built with user input concatenation/interpolation, flag

#### 3.3.2 `order` and `pluck` Injection

Often overlooked: `.order()` and `.pluck()` accept raw column names. If user-controlled:

```ruby
# UNSAFE: SQL injection in ORDER BY
User.order(params[:sort])

# SAFE: allowlist validation
ALLOWED_SORTS = %w[name created_at email].freeze
sort = ALLOWED_SORTS.include?(params[:sort]) ? params[:sort] : 'created_at'
User.order(sort)
```

Detection: flag `.order(params[...])` or `.pluck(params[...])` without preceding allowlist validation.

### 3.4 Render Injection

```ruby
# UNSAFE: arbitrary view rendering
render params[:action]

# UNSAFE: arbitrary file rendering
render file: params[:template]

# UNSAFE: inline template from user input
render inline: params[:template]
```

Detection: flag `render` calls where the argument (action, file, inline, template) is user-controlled.

### 3.5 Cookie and Session Security

#### 3.5.1 Session Configuration

```ruby
# config/initializers/session_store.rb
Rails.application.config.session_store :cookie_store,
  key: '_app_session',
  secure: Rails.env.production?,      # SAFE: secure in production
  httponly: true,                       # SAFE: no JS access
  same_site: :lax                      # SAFE: SameSite attribute

# UNSAFE: missing secure flag
Rails.application.config.session_store :cookie_store,
  key: '_app_session'
  # defaults: secure=false, httponly=true
```

Detection:
- Flag `secure: false` or missing `secure:` in production session config
- Flag `httponly: false`
- Flag missing `same_site` attribute

#### 3.5.2 Cookie Signing

```ruby
# UNSAFE: unsigned cookie with user-controlled data
cookies[:preference] = params[:pref]

# SAFE: signed cookie
cookies.signed[:preference] = params[:pref]

# SAFE: encrypted cookie
cookies.encrypted[:token] = session_token
```

Flag: `cookies[:key] = user_input` without `.signed` or `.encrypted` — data tampering risk.

### 3.6 Redirect Open Redirect

```ruby
# UNSAFE: open redirect
redirect_to params[:url]
redirect_to params[:return_to]

# SAFE: path-only redirect
redirect_to root_path

# SAFER: allowlist
redirect_to allowed_redirect_url(params[:url])
```

Detection: flag `redirect_to` where the argument is user-controlled and not validated against an allowlist or restricted to internal paths.

---

## 4. Sanitizers

**Estimated effort: 5-7h**

### 4.1 Rails Auto-Escaping

Rails ERB templates auto-escape output by default (since Rails 3):

```erb
<%= user_input %>        <!-- auto-escaped (safe) -->
<%= raw user_input %>    <!-- NOT escaped (sink) -->
<%= user_input.html_safe %> <!-- NOT escaped (sink) -->
```

Implementation: recognize `<%= expr %>` (without `raw` or `html_safe`) as a sanitizer for CWE-79.

### 4.2 Explicit Sanitizers

| Sanitizer | CWE | Description |
|-----------|-----|-------------|
| `sanitize(input)` | CWE-79 | Rails HTML sanitizer (allowlist-based) |
| `sanitize(input, tags: [...])` | CWE-79 | Allowlisted tags |
| `strip_tags(input)` | CWE-79 | Remove all HTML tags |
| `h(input)` / `html_escape(input)` | CWE-79 | HTML entity encoding |
| `ERB::Util.html_escape(input)` | CWE-79 | HTML entity encoding |
| `CGI.escapeHTML(input)` | CWE-79 | HTML entity encoding |
| `Rack::Utils.escape_html(input)` | CWE-79 | HTML entity encoding |
| `ActiveRecord::Base.sanitize_sql(input)` | CWE-89 | SQL sanitization |
| `ActiveRecord::Base.sanitize_sql_array(arr)` | CWE-89 | Array SQL sanitization |
| `Arel.sql(sanitized)` | CWE-89 | Explicit SQL literal (safe if input already sanitized) |
| `params.require(:m).permit(:f1, :f2)` | CWE-915 | Strong Parameters allowlist |
| `Shellwords.shellescape(input)` | CWE-78 | Shell argument escaping |
| `Shellwords.escape(input)` | CWE-78 | Shell argument escaping |
| `File.basename(input)` | CWE-22 | Strip directory components |
| `File.expand_path(input)` + prefix check | CWE-22 | Canonicalize + validate |
| `URI.parse(input)` + host check | CWE-918 | URL validation |
| `YAML.safe_load(input)` | CWE-502 | Safe YAML deserialization |
| `JSON.parse(input)` | CWE-502 | Safe JSON parsing (default mode) |

### 4.3 Content Security Policy

Rails 5.2+ supports CSP configuration:

```ruby
# config/initializers/content_security_policy.rb
Rails.application.config.content_security_policy do |policy|
  policy.default_src :self
  policy.script_src  :self, :https
  policy.style_src   :self, :unsafe_inline
end
```

Detection:
- Verify CSP initializer exists in Rails 5.2+ projects
- Flag `policy.script_src :unsafe_inline, :unsafe_eval` as weakening CSP
- Flag missing CSP in production configuration

---

## 5. Gem Vulnerability Scanning

**Estimated effort: 8-10h**

### 5.1 Gemfile.lock Parsing

Parse `Gemfile.lock` to extract dependency tree with pinned versions:

```ruby
# Gemfile.lock format
GEM
  remote: https://rubygems.org/
  specs:
    rails (7.0.4)
      actioncable (= 7.0.4)
      ...
    nokogiri (1.13.9)
    rack (2.2.4)
```

Implementation:
1. Parse `Gemfile.lock` — extract `(gem_name, version)` tuples
2. For each gem, check against advisory database
3. Report vulnerable gems with CVE, advisory ID, affected version range, patched version

### 5.2 Ruby Advisory Database Integration

Cross-reference with `rubysec/ruby-advisory-db` (https://github.com/rubysec/ruby-advisory-db):

- Advisory format: YAML files per gem per CVE
- Fields: `gem`, `cve`, `osvdb`, `url`, `title`, `date`, `description`, `cvss_v3`, `patched_versions`, `unaffected_versions`
- Integration with Phase 37 advisory DB infrastructure

### 5.3 Known Vulnerable Patterns by Gem

| Gem | Version | CVE | Pattern |
|-----|---------|-----|---------|
| `nokogiri` | < 1.13.10 | CVE-2022-23476 | XXE via default parser settings |
| `rack` | < 2.2.6.2 | CVE-2023-27530 | DoS via multipart parsing |
| `rails-html-sanitizer` | < 1.4.4 | CVE-2022-32209 | XSS bypass in sanitize |
| `puma` | < 5.6.7 | CVE-2023-40175 | HTTP request smuggling |
| `devise` | < 4.9.0 | Multiple | Authentication bypass |

### 5.4 Bundler Audit Integration

Recognize `bundle-audit` output format for cross-tool compatibility. If `bundle-audit` is available in the project, Piranesi can ingest its results alongside its own analysis.

---

## 6. Implementation Details

### 6.1 New/Modified Files

```
src/piranesi/scan/specs.py            # add Ruby source/sink SourceSpec/SinkSpec entries
src/piranesi/scan/surface.py          # Ruby file detection, Gemfile parsing
src/piranesi/scan/framework.py        # Ruby framework detection (Rails, Sinatra, Hanami)
src/piranesi/scan/joern.py            # register ruby -> rubysrc2cpg frontend
src/piranesi/detect/ruby_patterns.py  # NEW: Ruby-specific pattern detectors (YAML.load, Marshal.load, permit!)
src/piranesi/detect/flows.py          # add Ruby sanitizer recognition
src/piranesi/detect/dependencies.py   # Gemfile.lock parsing, ruby-advisory-db integration
```

### 6.2 SourceSpec / SinkSpec Registration

```python
SourceSpec(
    name="rails_params",
    language="ruby",
    pattern="params",
    source_type="request_param",
    frameworks=["rails", "sinatra"],
    confidence=1.0,
)

SinkSpec(
    name="rails_find_by_sql",
    language="ruby",
    pattern="find_by_sql",
    cwe="CWE-89",
    sink_type="sql_query",
    frameworks=["rails"],
    argument_index=0,
)
```

### 6.3 Framework Detection from Gemfile

```python
def detect_ruby_framework(project_root: Path) -> str | None:
    gemfile = project_root / "Gemfile"
    if not gemfile.exists():
        return None
    content = gemfile.read_text()
    if re.search(r"""gem\s+['"]rails['"]""", content):
        return "rails"
    if re.search(r"""gem\s+['"]sinatra['"]""", content):
        return "sinatra"
    if re.search(r"""gem\s+['"]hanami['"]""", content):
        return "hanami"
    if (project_root / "config" / "routes.rb").exists():
        return "rails"
    return "ruby"
```

### 6.4 ActiveRecord Query Analysis

Detecting string interpolation in ActiveRecord queries requires AST-level analysis. The `rubysrc2cpg` frontend represents string interpolation as call nodes in the CPG:

```scala
// CPGQL: find .where calls with string interpolation
cpg.call.name("where")
  .argument.isCall.name("<operator>.formatString")
  .map(c => (c.lineNumber, c.code, c.method.fullName))
  .toJsonPretty
```

Supplementary regex for cases Joern may not model:

```python
_RUBY_STRING_INTERPOLATION_IN_QUERY = re.compile(
    r"""\.(?:where|order|group|having|select|joins|from|find_by_sql)\s*\(\s*["'].*#\{.*params.*\}"""
)
```

---

## 7. Testing Strategy

**Estimated effort: 12-15h**

### 7.1 Ground Truth Entries (20+ entries)

| ID | CWE | Framework | Pattern | Label |
|----|-----|-----------|---------|-------|
| gt-230 | CWE-89 | Rails | `.where("name = '#{params[:name]}'")` | TP |
| gt-231 | CWE-89 | Rails | `.where(name: params[:name])` | FP (parameterized) |
| gt-232 | CWE-89 | Rails | `.where("name = ?", params[:name])` | FP (parameterized) |
| gt-233 | CWE-89 | Rails | `.order(params[:sort])` | TP |
| gt-234 | CWE-89 | Rails | `find_by_sql("SELECT * WHERE id=#{params[:id]}")` | TP |
| gt-235 | CWE-89 | Rails | `connection.execute("DELETE WHERE id=#{params[:id]}")` | TP |
| gt-236 | CWE-79 | Rails | `raw(params[:html])` in ERB | TP |
| gt-237 | CWE-79 | Rails | `<%= params[:name] %>` in ERB | FP (auto-escaped) |
| gt-238 | CWE-79 | Rails | `params[:bio].html_safe` in view | TP |
| gt-239 | CWE-78 | Rails | `system("convert #{params[:file]}")` | TP |
| gt-240 | CWE-78 | Rails | `system("convert", params[:file])` with `Shellwords.shellescape` | FP (sanitized) |
| gt-241 | CWE-22 | Rails | `send_file(params[:path])` | TP |
| gt-242 | CWE-22 | Rails | `send_file(Rails.root.join('public', File.basename(params[:file])))` | FP (basename sanitizes) |
| gt-243 | CWE-915 | Rails | `User.create(params[:user])` without permit | TP |
| gt-244 | CWE-915 | Rails | `User.create(params.require(:user).permit(:name, :email))` | FP (Strong Parameters) |
| gt-245 | CWE-915 | Rails | `User.create(params.require(:user).permit!)` | TP (permit! allows all) |
| gt-246 | CWE-502 | Ruby | `Marshal.load(cookies[:data])` | TP |
| gt-247 | CWE-502 | Ruby | `YAML.load(params[:config])` | TP |
| gt-248 | CWE-502 | Ruby | `YAML.safe_load(params[:config])` | FP (safe_load) |
| gt-249 | CWE-352 | Rails | Controller with `skip_before_action :verify_authenticity_token` | TP |
| gt-250 | CWE-601 | Rails | `redirect_to params[:url]` | TP (open redirect) |
| gt-251 | CWE-89 | Sinatra | `DB.fetch("SELECT * WHERE id=#{params[:id]}")` (Sequel) | TP |
| gt-252 | CWE-79 | Sinatra | `erb "<p>#{params[:name]}</p>"` inline template | TP |

### 7.2 Fixture Files

```
tests/fixtures/ruby/
    rails/
        controllers/
            users_controller.rb        # various ActionController patterns
            admin_controller.rb        # auth middleware patterns
            api_controller.rb          # API controller (no CSRF expected)
        models/
            user.rb                    # mass assignment patterns
            post.rb                    # safe model
        views/
            users/
                show.html.erb          # XSS patterns (raw, html_safe, auto-escape)
                index.html.erb         # safe output
        config/
            routes.rb                  # route definitions
            initializers/
                session_store.rb       # session config
                content_security_policy.rb  # CSP config
        Gemfile                        # dependency list
        Gemfile.lock                   # pinned versions
    sinatra/
        app.rb                         # Sinatra application
        Gemfile                        # Sinatra dependencies
    raw/
        marshal_load.rb                # Marshal.load from input
        yaml_load.rb                   # YAML.load unsafe
        yaml_safe_load.rb             # YAML.safe_load safe
        command_injection.rb           # system/exec/backtick
        file_read.rb                   # File.read with user path
```

### 7.3 Framework Detection Tests

```python
def test_detect_rails_from_gemfile():
    """Verify Rails detected from Gemfile gem 'rails' declaration."""
    assert detect_ruby_framework(fixture_path) == "rails"

def test_detect_rails_from_structure():
    """Verify Rails detected from config/routes.rb presence."""
    assert detect_ruby_framework(fixture_path) == "rails"

def test_detect_sinatra_from_gemfile():
    """Verify Sinatra detected from Gemfile gem 'sinatra' declaration."""
    assert detect_ruby_framework(fixture_path) == "sinatra"

def test_raw_ruby_fallback():
    """Verify raw ruby returned when no framework detected."""
    assert detect_ruby_framework(fixture_path) == "ruby"
```

### 7.4 Strong Parameters Tests

```python
def test_permit_with_allowlist_is_safe():
    """params.require(:user).permit(:name) should not flag mass assignment."""
    assert not is_mass_assignment_vulnerable(fixture)

def test_permit_bang_is_unsafe():
    """params.require(:user).permit! should flag CWE-915."""
    findings = scan(fixture)
    assert any(f.cwe == "CWE-915" for f in findings)

def test_no_permit_is_unsafe():
    """User.create(params[:user]) without permit should flag CWE-915."""
    findings = scan(fixture)
    assert any(f.cwe == "CWE-915" for f in findings)
```

### 7.5 Acceptance Criteria

- All 20+ GT entries pass with correct labels
- Zero regressions on existing TS/JS/Python/Go/Java test suite
- Framework detection correctly identifies Rails, Sinatra from fixtures
- Strong Parameters analysis correctly distinguishes `permit` vs `permit!` vs no permit
- CSRF detection flags `skip_before_action :verify_authenticity_token`
- Gem vulnerability scanning parses `Gemfile.lock` and reports known CVEs
- ActiveRecord string interpolation detection works for `.where`, `.order`, `.find_by_sql`

---

## 8. Risks and Mitigations

| Risk | Severity | Mitigation |
|------|----------|------------|
| `rubysrc2cpg` Joern frontend may have lower maturity than Java/JS frontends | Medium | Test against diverse Rails apps; supplement with regex patterns for constructs Joern may miss |
| Ruby metaprogramming (`method_missing`, `define_method`, `send`) creates dynamic dispatch that static analysis cannot resolve | High | Conservative: flag `send(params[:method])` as potential code execution; treat `method_missing` targets as unknown |
| ERB template parsing may not be supported by `rubysrc2cpg` | Medium | Pre-process ERB to extract Ruby expressions; regex-match `raw`/`html_safe` patterns |
| Rails autoloading (`zeitwerk`) means class-to-file mapping follows naming conventions | Low | Use Rails conventions (class `UsersController` in `app/controllers/users_controller.rb`) for file resolution |
| `Gemfile.lock` may not be committed (some projects gitignore it) | Low | Fall back to `Gemfile` version constraints (less precise); warn user about missing lockfile |
| Ruby DSL-heavy style (routes, migrations, config) is hard to statically analyze | Medium | Focus on controller/model/view analysis; treat DSL config as declarative metadata parsed via regex |
