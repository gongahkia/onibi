# Phase 39: PHP Web Security Analysis

**Estimated effort: 65-85 ideal hours**
**Blocked by: Phase 13 (multi-language architecture), Phase 22 (advanced taint analysis)**
**Blocks: Nothing (incremental value)**
**Target milestone: v0.7.0**

---

## 1. Overview

### 1.1 Motivation

PHP powers approximately 77% of all websites with a known server-side language. WordPress alone accounts for ~43% of all websites. Laravel, Symfony, Drupal, and CodeIgniter represent massive enterprise and SMB attack surfaces. Any serious SAST tool must cover PHP to be relevant in web security.

### 1.2 Joern PHP Support

Joern supports PHP via the `php2cpg` frontend, which parses PHP source into the Code Property Graph. Piranesi's existing multi-language architecture (`scan/framework.py`, `scan/joern.py` with `LANGUAGE_TO_JOERN_FRONTEND`) already abstracts over Joern frontends — adding PHP requires:

1. Registering `php` -> `php2cpg` in `LANGUAGE_TO_JOERN_FRONTEND`
2. Language detection from file extensions and `composer.json`
3. PHP-specific source/sink/sanitizer specifications
4. Framework-specific detection logic for Laravel, Symfony, WordPress, CodeIgniter

### 1.3 Detection Strategy

PHP vulnerabilities span both taint-based (SQLi, XSS, CMDi, SSRF) and pattern-based (deserialization, type juggling, extract misuse) categories. The majority of findings will come from taint analysis via Joern `reachableByFlows`, with supplementary regex-based detectors for PHP-specific footguns.

### 1.4 Language Detection

| Signal | Framework | Detection Source |
|--------|-----------|-----------------|
| `*.php` | Raw PHP | File extension |
| `composer.json` with `laravel/framework` | Laravel | Composer dependency |
| `composer.json` with `symfony/symfony` or `symfony/framework-bundle` | Symfony | Composer dependency |
| `wp-config.php`, `wp-includes/`, `wp-content/` | WordPress | Directory structure |
| `composer.json` with `codeigniter4/framework` | CodeIgniter | Composer dependency |
| `composer.json` with `drupal/core` | Drupal | Composer dependency |
| `artisan` file in project root | Laravel | CLI entry point |

---

## 2. PHP Source/Sink Specifications

**Estimated effort: 15-20h**

### 2.1 Sources (User-Controlled Input)

#### 2.1.1 Core PHP Superglobals

| Source | Description | Taint Type |
|--------|-------------|------------|
| `$_GET` | URL query parameters | `request_param` |
| `$_POST` | POST form data | `request_body` |
| `$_REQUEST` | Merged GET/POST/COOKIE | `request_param` |
| `$_COOKIE` | HTTP cookies | `cookie` |
| `$_SERVER` | Server/request metadata (`HTTP_HOST`, `REQUEST_URI`, `QUERY_STRING`, `HTTP_REFERER`, `HTTP_USER_AGENT`) | `request_header` |
| `$_FILES` | Uploaded file metadata (`name`, `tmp_name`, `type`) | `file_upload` |
| `file_get_contents('php://input')` | Raw request body | `request_body` |
| `php://stdin` | CLI stdin input | `stdin` |

#### 2.1.2 Laravel Sources

| Source | Description |
|--------|-------------|
| `$request->input()` | Any input (GET/POST/JSON) |
| `$request->query()` | Query string parameters |
| `$request->post()` | POST data |
| `$request->get()` | Single parameter by key |
| `$request->all()` | All input data |
| `$request->json()` | JSON body |
| `$request->file()` | Uploaded file |
| `$request->header()` | HTTP header value |
| `$request->cookie()` | Cookie value |
| `$request->route()` | Route parameter |
| `$request->segment()` | URL segment |

#### 2.1.3 Symfony Sources

| Source | Description |
|--------|-------------|
| `$request->get()` | Request parameter |
| `$request->query->get()` | Query parameter bag |
| `$request->request->get()` | POST parameter bag |
| `$request->cookies->get()` | Cookie bag |
| `$request->headers->get()` | Header bag |
| `$request->files->get()` | File bag |
| `$request->getContent()` | Raw body |

#### 2.1.4 WordPress Sources

| Source | Description |
|--------|-------------|
| `$_GET` / `$_POST` / `$_REQUEST` | Standard superglobals (WordPress rarely wraps these) |
| `get_query_var()` | WP_Query variable |
| `$_SERVER['REQUEST_URI']` | Request URI |

### 2.2 Sinks by CWE

#### 2.2.1 CWE-89: SQL Injection

| Sink | Context | Risk |
|------|---------|------|
| `mysqli_query($conn, $query)` | Raw MySQLi | Direct concatenation |
| `$mysqli->query($query)` | OOP MySQLi | Direct concatenation |
| `$pdo->query($query)` | PDO without prepare | Direct concatenation |
| `$pdo->exec($query)` | PDO exec | Direct concatenation |
| `mysql_query($query)` | Deprecated mysql_* | Direct concatenation |
| `$wpdb->query($query)` | WordPress raw query | Without `$wpdb->prepare()` |
| `$wpdb->get_results($query)` | WordPress get_results | Without `$wpdb->prepare()` |
| `$wpdb->get_var($query)` | WordPress get_var | Without `$wpdb->prepare()` |
| `$wpdb->get_row($query)` | WordPress get_row | Without `$wpdb->prepare()` |
| `DB::raw($expr)` | Laravel raw expression | Inside query builder |
| `DB::select($raw)` | Laravel raw select | Direct string |
| `->whereRaw($expr)` | Laravel raw where | Unparameterized |
| `->selectRaw($expr)` | Laravel raw select | Unparameterized |
| `->orderByRaw($expr)` | Laravel raw orderBy | Unparameterized |
| `->havingRaw($expr)` | Laravel raw having | Unparameterized |
| `->groupByRaw($expr)` | Laravel raw groupBy | Unparameterized |
| `$connection->executeQuery($sql)` | Doctrine DBAL | Direct string |
| `$entityManager->createQuery($dql)` | Doctrine ORM DQL | String concatenation |

#### 2.2.2 CWE-79: Cross-Site Scripting

| Sink | Context | Risk |
|------|---------|------|
| `echo $var` | Direct output | No escaping |
| `print $var` | Direct output | No escaping |
| `<?= $var ?>` | Short echo tag | No escaping |
| `printf($fmt, $var)` | Formatted output | User data in format args |
| `{!! $var !!}` | Laravel Blade unescaped | Explicit raw output |
| `@php echo $var @endphp` | Blade PHP directive | No auto-escaping |
| `\|raw` | Twig raw filter | Disables auto-escaping |
| `{% autoescape false %}` | Twig autoescape off | Block-level disable |
| `header("Location: $url")` | Header injection | Open redirect / response splitting |
| `setcookie($name, $val)` | Cookie injection | When value is user-controlled |

**Note:** Blade `{{ $var }}` is safe by default (auto-escaped via `htmlspecialchars`). Twig `{{ var }}` is also safe by default. Only unescaped variants are sinks.

#### 2.2.3 CWE-78: Command Injection

| Sink | Context | Risk |
|------|---------|------|
| `exec($cmd)` | Execute command | Direct execution |
| `system($cmd)` | Execute with output | Direct execution |
| `passthru($cmd)` | Execute with raw output | Direct execution |
| `shell_exec($cmd)` | Execute via shell | Direct execution |
| `` `$cmd` `` | Backtick operator | Equivalent to `shell_exec` |
| `proc_open($cmd, ...)` | Process control | Direct execution |
| `popen($cmd, $mode)` | Pipe open | Direct execution |
| `pcntl_exec($path, $args)` | Process execution | Direct execution |
| `Symfony\Process($cmd)` | Symfony Process component | If command from user input |

#### 2.2.4 CWE-22: Path Traversal

| Sink | Context | Risk |
|------|---------|------|
| `file_get_contents($path)` | File read | Path from user input |
| `file_put_contents($path, $data)` | File write | Path from user input |
| `fopen($path, $mode)` | File open | Path from user input |
| `readfile($path)` | Read and output file | Path from user input |
| `include($path)` | Include PHP file | Remote/local file inclusion |
| `include_once($path)` | Include once | Remote/local file inclusion |
| `require($path)` | Require PHP file | Remote/local file inclusion |
| `require_once($path)` | Require once | Remote/local file inclusion |
| `unlink($path)` | File delete | Arbitrary file deletion |
| `rename($old, $new)` | File rename/move | Arbitrary file manipulation |
| `copy($src, $dst)` | File copy | Arbitrary file write |
| `mkdir($path)` | Directory creation | Path from user input |
| `Storage::get($path)` | Laravel Storage | Path from user input |
| `Storage::put($path, $data)` | Laravel Storage | Path from user input |

#### 2.2.5 CWE-502: Insecure Deserialization

| Sink | Context | Risk |
|------|---------|------|
| `unserialize($data)` | PHP native deserialize | Object injection, RCE via POP chains |
| `yaml_parse($data)` | YAML parse | Object instantiation |
| `simplexml_load_string($data)` | XML parse | XXE (also CWE-611) |
| `json_decode($data)` | JSON parse | Generally safe, but flag if input flows to `unserialize` later |

#### 2.2.6 CWE-918: Server-Side Request Forgery

| Sink | Context | Risk |
|------|---------|------|
| `file_get_contents($url)` | URL fetch | User-controlled URL |
| `curl_exec($ch)` with `CURLOPT_URL` from user | cURL | User-controlled URL |
| `$client->get($url)` | Guzzle HTTP | User-controlled URL |
| `$client->post($url)` | Guzzle HTTP | User-controlled URL |
| `$client->request($method, $url)` | Guzzle HTTP | User-controlled URL |
| `Http::get($url)` | Laravel HTTP client | User-controlled URL |
| `Http::post($url)` | Laravel HTTP client | User-controlled URL |
| `fopen($url, 'r')` | URL wrapper | With `allow_url_fopen=On` |

#### 2.2.7 CWE-1336: Server-Side Template Injection

| Sink | Context | Risk |
|------|---------|------|
| `new \Twig\Environment` with user-controlled template string | Twig SSTI | Template from user input |
| `$twig->createTemplate($userInput)` | Twig inline template | Direct template injection |
| `Blade::compileString($userInput)` | Blade compile | Dynamic compilation |
| `eval('?>' . $userInput)` | Eval as template | Code execution via template |

---

## 3. Framework-Specific Detection

**Estimated effort: 20-25h**

### 3.1 Laravel Detection

#### 3.1.1 Middleware Chain Analysis

Laravel routes pass through middleware stacks. Detect security-relevant middleware presence/absence:

```php
// Route without auth middleware — flag if accessing sensitive resources
Route::get('/admin/users', [AdminController::class, 'index']);

// Route with auth middleware — OK
Route::middleware(['auth', 'can:admin'])->group(function () {
    Route::get('/admin/users', [AdminController::class, 'index']);
});
```

Implementation: parse `routes/web.php` and `routes/api.php`. For each route group, track applied middleware. Flag routes to sensitive controllers without `auth` middleware.

#### 3.1.2 Mass Assignment Protection

Detect Eloquent models with misconfigured `$guarded` / `$fillable`:

| Pattern | Risk | CWE |
|---------|------|-----|
| `$guarded = []` | All fields mass-assignable | CWE-915 |
| No `$fillable` and no `$guarded` | Depends on Laravel version | CWE-915 |
| `$fillable` includes sensitive fields (`is_admin`, `role`, `password`) | Privilege escalation | CWE-915 |

#### 3.1.3 CSRF Protection

- Verify `@csrf` or `{{ csrf_field() }}` in Blade forms
- Flag routes using `withoutMiddleware('VerifyCsrfToken')` or routes excluded in `VerifyCsrfToken::$except`
- Flag API routes that accept state-changing operations without token verification

#### 3.1.4 Auth Gates and Policies

- Detect `Gate::define()` and `Policy` classes
- Flag controller methods accessing resources without `$this->authorize()` or `can` middleware
- Detect `Gate::before()` returning `true` unconditionally (bypasses all gates)

### 3.2 Symfony Detection

#### 3.2.1 Security Configuration

Parse `config/packages/security.yaml`:

```yaml
security:
    firewalls:
        main:
            pattern: ^/
            # flag if no authenticator configured
    access_control:
        - { path: ^/admin, roles: ROLE_ADMIN }
        # flag if no access_control for sensitive paths
```

Detect:
- Firewalls without authenticators
- Missing `access_control` for admin/sensitive paths
- `IS_AUTHENTICATED_ANONYMOUSLY` on sensitive routes

#### 3.2.2 Voter and Firewall Analysis

- Detect custom voters that always return `ACCESS_GRANTED`
- Flag `$token->getUser()` usage without null-check (potential null deref)
- Detect CSRF token verification disabled in forms

#### 3.2.3 Doctrine Query Security

- Flag `createQuery()` with string concatenation (DQL injection)
- Detect `$conn->executeQuery($sql)` without parameter binding
- Recognize `createQueryBuilder()` with parameterized `setParameter()` as safe

### 3.3 WordPress Detection

#### 3.3.1 Sanitizer Recognition

WordPress has a rich ecosystem of escaping functions. These must be registered as sanitizers:

| Function | Sanitizes For | CWE |
|----------|---------------|-----|
| `esc_html()` | HTML entity encoding | CWE-79 |
| `esc_attr()` | Attribute context | CWE-79 |
| `esc_url()` | URL validation/sanitization | CWE-79, CWE-918 |
| `esc_js()` | JavaScript string context | CWE-79 |
| `esc_textarea()` | Textarea context | CWE-79 |
| `wp_kses()` | Allowlist-based HTML filter | CWE-79 |
| `wp_kses_post()` | Post-safe HTML filter | CWE-79 |
| `sanitize_text_field()` | General text | CWE-79 |
| `sanitize_email()` | Email format | CWE-79 |
| `sanitize_file_name()` | File name | CWE-22 |
| `sanitize_title()` | Title/slug | CWE-79 |
| `absint()` | Absolute integer | CWE-89 |
| `intval()` | Integer cast | CWE-89 |
| `$wpdb->prepare()` | Parameterized query | CWE-89 |
| `wp_nonce_field()` / `wp_verify_nonce()` | CSRF token | CWE-352 |
| `check_admin_referer()` | Admin CSRF | CWE-352 |

#### 3.3.2 Hook/Action/Filter Taint Propagation

WordPress uses a hook system (`add_action`, `add_filter`, `apply_filters`, `do_action`) that passes data between callbacks. Taint propagation must track data through filters:

```php
// taint enters via filter
add_filter('the_content', function($content) {
    return $content . $_GET['inject']; // XSS: taint from $_GET flows through filter
});

// taint consumed when filter applied
$output = apply_filters('the_content', $post->post_content);
echo $output; // sink: output includes tainted filter result
```

Implementation:
1. Map `add_filter($tag, $callback)` — register callback as a transformer for `$tag`
2. At `apply_filters($tag, $value)` — if any callback registered for `$tag` introduces taint, the return value is tainted
3. Conservative: if any registered callback for a filter tag is tainted, treat the filter output as tainted

#### 3.3.3 WordPress REST API

- Detect `register_rest_route()` endpoints
- Sources: `$request->get_param()`, `$request->get_params()`, `$request->get_body()`
- Verify `permission_callback` is set (not `__return_true` for sensitive operations)
- Flag `'permission_callback' => '__return_true'` on state-changing endpoints

### 3.4 CodeIgniter Detection

| Source | Description |
|--------|-------------|
| `$this->input->post()` | POST parameter |
| `$this->input->get()` | GET parameter |
| `$this->input->cookie()` | Cookie |
| `$this->input->server()` | Server variable |
| `$this->request->getPost()` | CI4 POST |
| `$this->request->getGet()` | CI4 GET |

Sanitizer recognition:
- Active Record / Query Builder with parameter binding (`$this->db->where()` with array)
- `$this->security->xss_clean()` (CI3)
- CI4 validation rules

---

## 4. PHP-Specific Challenges

**Estimated effort: 10-12h**

### 4.1 Variable Variables

PHP allows `$$var` — a variable whose name is determined at runtime:

```php
$field = $_GET['field'];
$$field = $_GET['value']; // creates a variable named by user input
echo $name; // may be tainted if $field == 'name'
```

**Handling:** Treat `$$var` as a computed property access (conservative). When `$$var` is assigned, mark ALL local variables as potentially tainted by that assignment. Flag `$$var` usage with user-controlled `$var` as CWE-473 (Improper Handling of Alternate Value Interpretation).

### 4.2 Type Juggling (CWE-1289)

PHP's loose comparison (`==`) performs type coercion that can bypass security checks:

```php
if ($_GET['token'] == 0) { // "abc" == 0 is TRUE in PHP < 8.0
    grant_access();
}
if (md5($input) == "0e462097431906509019562988736854") { // magic hash
    bypass_auth();
}
```

**Detection rules:**
1. Flag `==` comparisons where one operand is user-controlled and the other is an integer literal `0`
2. Flag `==` comparisons of hash function outputs (MD5, SHA1) against strings starting with `0e`
3. Flag `==` / `!=` in authentication/authorization context (function names containing `auth`, `login`, `verify`, `check`, `validate`) — suggest `===`
4. PHP 8.0+ changed `"string" == 0` to `false`, so version-aware detection via `composer.json` `require.php` constraint

### 4.3 `extract()` Function (CWE-915)

`extract()` imports variables from an array into the current scope:

```php
extract($_POST); // every POST parameter becomes a local variable
// $username, $password, $is_admin all set from POST data
if ($is_admin) { // mass assignment — attacker sets is_admin=1
    grant_admin();
}
```

**Detection:** Flag any `extract()` call where the argument is or derives from a superglobal (`$_GET`, `$_POST`, `$_REQUEST`, `$_COOKIE`). Severity: high. CWE-915 (Improperly Controlled Modification of Dynamically-Determined Object Attributes).

### 4.4 Register Globals (Legacy)

`register_globals` was removed in PHP 5.4 but legacy applications may still rely on its behavior:

**Detection:** Pattern-match `php.ini` or `.htaccess` for `register_globals = On`. Flag as CWE-473, severity critical.

### 4.5 Deserialization Gadget Chains (CWE-502)

PHP object injection via `unserialize()` is exploitable when the codebase contains classes with dangerous magic methods:

| Magic Method | Risk |
|-------------|------|
| `__wakeup()` | Executed on deserialize — if it performs dangerous operations |
| `__destruct()` | Executed when object goes out of scope — file deletion, command execution |
| `__toString()` | Executed on string cast — if used in `include`, `echo`, `query` |
| `__call()` | Invoked on undefined method — can proxy to dangerous functions |
| `__get()` / `__set()` | Property access proxying — can trigger side effects |

**Detection strategy:**
1. Find all `unserialize($user_input)` calls (taint-based)
2. Enumerate classes with dangerous magic methods in the codebase (AST scan)
3. Cross-reference: if `unserialize` is reachable from user input AND the codebase contains exploitable gadget classes, flag as high severity
4. Without gadget classes: flag as medium severity (third-party dependencies may contain gadgets)

### 4.6 PHP `eval()` and Dynamic Includes

| Pattern | CWE | Risk |
|---------|-----|------|
| `eval($userInput)` | CWE-94 | Code injection |
| `assert($userInput)` | CWE-94 | Code injection (PHP < 7.0 evaluates string) |
| `preg_replace('/pattern/e', $replacement, $subject)` | CWE-94 | `e` modifier evaluates replacement as PHP (removed in PHP 7.0) |
| `create_function('$a', $userInput)` | CWE-94 | Dynamic function creation (deprecated PHP 7.2) |
| `include($userControlled)` | CWE-98 | Local/Remote File Inclusion |

---

## 5. Sanitizer Specifications

**Estimated effort: 5-7h**

### 5.1 General PHP Sanitizers

| Sanitizer | Sanitizes For | CWE |
|-----------|---------------|-----|
| `htmlspecialchars($str, ENT_QUOTES, 'UTF-8')` | HTML entity encoding | CWE-79 |
| `htmlentities($str, ENT_QUOTES, 'UTF-8')` | HTML entity encoding (all entities) | CWE-79 |
| `strip_tags($str)` | HTML tag removal | CWE-79 (weak — attribute injection possible) |
| `addslashes($str)` | Quote escaping | CWE-89 (weak — not reliable for all charsets) |
| `intval($str)` / `(int)$str` | Integer cast | CWE-89 |
| `floatval($str)` / `(float)$str` | Float cast | CWE-89 |
| `filter_var($str, FILTER_SANITIZE_EMAIL)` | Email sanitization | CWE-79 |
| `filter_var($str, FILTER_SANITIZE_URL)` | URL sanitization | CWE-79 |
| `filter_var($str, FILTER_VALIDATE_INT)` | Integer validation | CWE-89 |
| `filter_var($str, FILTER_VALIDATE_URL)` | URL validation | CWE-918 |
| `basename($path)` | Strip directory components | CWE-22 |
| `realpath($path)` + prefix check | Canonicalize + verify | CWE-22 |
| `escapeshellarg($str)` | Shell argument escaping | CWE-78 |
| `escapeshellcmd($str)` | Shell command escaping | CWE-78 (partial) |

### 5.2 PDO Prepared Statements

Recognize parameterized queries as safe:

```php
// SAFE: parameterized
$stmt = $pdo->prepare("SELECT * FROM users WHERE id = ?");
$stmt->execute([$id]);

// SAFE: named parameters
$stmt = $pdo->prepare("SELECT * FROM users WHERE id = :id");
$stmt->execute(['id' => $id]);

// UNSAFE: concatenation in prepare argument
$stmt = $pdo->prepare("SELECT * FROM users WHERE name = '" . $name . "'");
```

**Detection:** If `prepare()` is called with a string literal (no concatenation/interpolation), and the tainted value flows to `execute()` as an array element, the flow is sanitized for CWE-89.

### 5.3 Laravel Sanitizers

| Sanitizer | CWE |
|-----------|-----|
| `e()` | CWE-79 (alias for `htmlspecialchars`) |
| `Str::of($input)->ascii()` | CWE-79 |
| Eloquent parameterized queries (`where('col', $val)`) | CWE-89 |
| Query builder with `?` bindings | CWE-89 |
| `Validator::make()` with rules | Context-dependent |
| `$request->validate([...])` | Context-dependent |

### 5.4 Weak Sanitizers (Flag as Insufficient)

| Function | Issue |
|----------|-------|
| `addslashes()` | Fails on multi-byte charsets (GBK); use prepared statements instead |
| `strip_tags()` | Does not prevent attribute injection; insufficient for XSS |
| `htmlspecialchars()` without `ENT_QUOTES` | Single quotes not escaped; attribute context vulnerable |
| `mysql_real_escape_string()` | Deprecated; charset-dependent |
| `escapeshellcmd()` | Does not prevent argument injection (only command injection) |

When a weak sanitizer is the only barrier between source and sink, emit finding with reduced confidence (0.6) and a note about the sanitizer weakness.

---

## 6. Implementation Details

### 6.1 New/Modified Files

```
src/piranesi/scan/specs.py           # add PHP source/sink SourceSpec/SinkSpec entries
src/piranesi/scan/surface.py         # PHP file detection, composer.json parsing
src/piranesi/scan/framework.py       # PHP framework detection (Laravel, Symfony, WordPress, CI)
src/piranesi/scan/joern.py           # register php -> php2cpg frontend
src/piranesi/detect/php_patterns.py  # NEW: PHP-specific pattern detectors (type juggle, extract, variable variables)
src/piranesi/detect/flows.py         # add PHP sanitizer recognition, CWE severity mappings
```

### 6.2 SourceSpec / SinkSpec Registration

Extend `scan/specs.py` with PHP entries. Each spec follows the existing pattern:

```python
SourceSpec(
    name="php_get",
    language="php",
    pattern="$_GET",
    source_type="request_param",
    frameworks=["php"],
    confidence=1.0,
)

SinkSpec(
    name="php_mysqli_query",
    language="php",
    pattern="mysqli_query",
    cwe="CWE-89",
    sink_type="sql_query",
    frameworks=["php"],
    argument_index=1,  # second argument is the query string
)
```

### 6.3 Framework Detection from `composer.json`

```python
def detect_php_framework(project_root: Path) -> str | None:
    composer = project_root / "composer.json"
    if not composer.exists():
        return None
    data = json.loads(composer.read_text())
    requires = {**data.get("require", {}), **data.get("require-dev", {})}
    if "laravel/framework" in requires:
        return "laravel"
    if any(k.startswith("symfony/") for k in requires):
        return "symfony"
    if "drupal/core" in requires:
        return "drupal"
    if "codeigniter4/framework" in requires:
        return "codeigniter"
    return "php"  # raw PHP
```

### 6.4 Severity Mappings

Add to `_SEVERITY_BY_CWE` in `detect/flows.py`:

```python
"CWE-473": "medium",   # variable variables / register_globals
"CWE-915": "high",     # mass assignment (extract)
"CWE-94":  "critical", # code injection (eval)
"CWE-98":  "critical", # file inclusion
"CWE-1289": "medium",  # type juggling
```

---

## 7. Testing Strategy

**Estimated effort: 15-18h**

### 7.1 Ground Truth Entries (25+ entries)

| ID | CWE | Framework | Pattern | Label |
|----|-----|-----------|---------|-------|
| gt-200 | CWE-89 | Raw PHP | `mysqli_query($conn, "SELECT * WHERE id=" . $_GET['id'])` | TP |
| gt-201 | CWE-89 | Raw PHP | `$stmt = $pdo->prepare("SELECT * WHERE id=?"); $stmt->execute([$_GET['id']])` | FP (parameterized) |
| gt-202 | CWE-79 | Raw PHP | `echo $_GET['name']` | TP |
| gt-203 | CWE-79 | Raw PHP | `echo htmlspecialchars($_GET['name'], ENT_QUOTES, 'UTF-8')` | FP (sanitized) |
| gt-204 | CWE-78 | Raw PHP | `exec("ls " . $_GET['dir'])` | TP |
| gt-205 | CWE-78 | Raw PHP | `exec("ls " . escapeshellarg($_GET['dir']))` | FP (sanitized) |
| gt-206 | CWE-22 | Raw PHP | `include($_GET['page'])` | TP |
| gt-207 | CWE-502 | Raw PHP | `unserialize($_COOKIE['data'])` | TP |
| gt-208 | CWE-918 | Raw PHP | `file_get_contents($_GET['url'])` | TP |
| gt-209 | CWE-89 | Laravel | `DB::raw($_POST['sort'])` in `orderByRaw` | TP |
| gt-210 | CWE-89 | Laravel | `User::where('email', $request->input('email'))->first()` | FP (parameterized) |
| gt-211 | CWE-79 | Laravel | `{!! $userInput !!}` in Blade | TP |
| gt-212 | CWE-79 | Laravel | `{{ $userInput }}` in Blade | FP (auto-escaped) |
| gt-213 | CWE-915 | Laravel | `$guarded = []` on User model | TP |
| gt-214 | CWE-89 | WordPress | `$wpdb->query("SELECT * WHERE id=" . $_GET['id'])` | TP |
| gt-215 | CWE-89 | WordPress | `$wpdb->get_results($wpdb->prepare("SELECT * WHERE id=%d", $_GET['id']))` | FP (prepared) |
| gt-216 | CWE-79 | WordPress | `echo $_GET['name']` in plugin | TP |
| gt-217 | CWE-79 | WordPress | `echo esc_html($_GET['name'])` | FP (sanitized) |
| gt-218 | CWE-915 | Raw PHP | `extract($_POST)` | TP |
| gt-219 | CWE-1289 | Raw PHP | `if ($_GET['token'] == 0)` | TP |
| gt-220 | CWE-94 | Raw PHP | `eval($_POST['code'])` | TP |
| gt-221 | CWE-89 | Symfony | `$conn->executeQuery("SELECT * WHERE id=" . $request->get('id'))` | TP |
| gt-222 | CWE-89 | Symfony | `$conn->executeQuery("SELECT * WHERE id=?", [$request->get('id')])` | FP (parameterized) |
| gt-223 | CWE-918 | Laravel | `Http::get($request->input('url'))` | TP |
| gt-224 | CWE-1336 | Raw PHP | `$twig->createTemplate($_GET['tpl'])->render()` | TP |
| gt-225 | CWE-502 | Raw PHP | `unserialize()` with `__destruct` gadget class in scope | TP (high confidence) |

### 7.2 Fixture Files

```
tests/fixtures/php/
    raw/
        sqli_concat.php                # CWE-89 raw concatenation
        sqli_prepared.php              # CWE-89 parameterized (safe)
        xss_echo.php                   # CWE-79 raw echo
        xss_escaped.php                # CWE-79 htmlspecialchars (safe)
        cmdi_exec.php                  # CWE-78 exec with user input
        cmdi_escaped.php               # CWE-78 escapeshellarg (safe)
        path_traversal_include.php     # CWE-22 include user input
        deserialize_cookie.php         # CWE-502 unserialize cookie
        ssrf_file_get_contents.php     # CWE-918 file_get_contents URL
        extract_post.php               # CWE-915 extract($_POST)
        type_juggling.php              # CWE-1289 loose comparison
        eval_post.php                  # CWE-94 eval user input
        ssti_twig.php                  # CWE-1336 Twig template injection
        gadget_chain.php               # CWE-502 with __destruct gadget
        variable_variables.php         # $$var from user input
    laravel/
        controller_raw_query.php       # DB::raw with user input
        controller_eloquent_safe.php   # parameterized Eloquent
        blade_unescaped.blade.php      # {!! !!} sink
        blade_escaped.blade.php        # {{ }} safe
        model_unguarded.php            # $guarded = []
        route_no_auth.php              # missing auth middleware
        http_ssrf.php                  # Http::get with user URL
    wordpress/
        plugin_sqli.php                # $wpdb->query concatenation
        plugin_sqli_safe.php           # $wpdb->prepare
        plugin_xss.php                 # echo without escaping
        plugin_xss_safe.php            # esc_html
        rest_api_no_perm.php           # __return_true permission
    symfony/
        controller_dql_inject.php      # DQL string concatenation
        controller_param_safe.php      # parameterized query
```

### 7.3 Framework Detection Tests

```python
def test_detect_laravel_from_composer():
    """Verify Laravel detected from composer.json laravel/framework dependency."""
    # fixture with composer.json containing "laravel/framework"
    assert detect_php_framework(fixture_path) == "laravel"

def test_detect_wordpress_from_structure():
    """Verify WordPress detected from wp-config.php presence."""
    assert detect_php_framework(fixture_path) == "wordpress"

def test_detect_symfony_from_composer():
    """Verify Symfony detected from symfony/ packages."""
    assert detect_php_framework(fixture_path) == "symfony"

def test_raw_php_fallback():
    """Verify raw PHP returned when no framework detected."""
    assert detect_php_framework(fixture_path) == "php"
```

### 7.4 Acceptance Criteria

- All 25+ GT entries pass with correct labels
- Zero regressions on existing TS/JS/Python/Go/Java test suite
- Framework detection correctly identifies Laravel, Symfony, WordPress, CodeIgniter from fixtures
- Weak sanitizer detection emits reduced-confidence findings
- `extract($_POST)` and type juggling patterns detected without Joern (regex-based)

---

## 8. Risks and Mitigations

| Risk | Severity | Mitigation |
|------|----------|------------|
| `php2cpg` Joern frontend has lower maturity than JS/Java frontends | Medium | Test against diverse PHP codebases; fall back to regex patterns for unsupported constructs |
| WordPress hook/filter taint propagation is complex (>100 filters in WP core) | Medium | Start with conservative: any filter callback touching user input taints the filter output |
| Blade/Twig template parsing not directly supported by `php2cpg` | High | Pre-process templates to extract PHP expressions; or regex-match `{!! !!}` / `\|raw` patterns |
| Legacy PHP (5.x) codebases with deprecated functions | Low | Include deprecated function patterns (`mysql_*`, `ereg`, `create_function`); emit deprecation warning alongside security finding |
| Composer `autoload` means class resolution depends on PSR-4 mapping | Medium | Parse `composer.json` `autoload` section to resolve class-to-file mapping for interprocedural analysis |
| PHP string interpolation (`"Hello $name"` / `"Hello {$name}"`) may be missed by Joern | Medium | Supplement Joern flows with regex detection of variable interpolation in double-quoted strings near sinks |
