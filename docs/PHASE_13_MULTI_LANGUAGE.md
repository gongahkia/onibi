# Phase 13: Multi-Language Support (Shallow)

**Estimated effort: 60-90 ideal hours (shallow pass across 3 languages)**
**Blocked by: Phase 12 (plugin system), Phase 1 (Joern already supports Java/Go/Python)**
**Blocks: Phase 14 (deep per-language support)**
**Target milestone: v1.0**

---

## 1. Phase Overview

Piranesi's architecture delegates heavy-lifting analysis to Joern. Joern already supports Java, Python, Go, C/C++, PHP, and Ruby via language-specific frontends (`javasrc2cpg`, `pysrc2cpg`, `gosrc2cpg`, etc.). Multi-language support is therefore a matter of:

1. Detecting the language
2. Configuring the correct Joern frontend
3. Providing language-specific source/sink specs
4. Adjusting transpilation (or removing it — Python/Go/Java don't need TS→JS conversion)

This phase adds shallow support for Python (Flask/Django/FastAPI), Go (Gin/Echo/Chi), and Java (Spring Boot). "Shallow" means: detect the language and framework, provide basic source/sink specs for the top 3 vulnerability classes (SQLi, XSS, CMDi), and run Joern's data flow analysis. Deep framework-specific support (Django ORM, Spring Security, Go middleware chains) is deferred.

---

## 2. Language Detection

**Estimated effort: 4-5h**

Implement `src/piranesi/scan/language.py`:

### 2.1 Detection Strategy

```python
def detect_languages(project_root: Path) -> list[LanguageInfo]:
    """Detect programming languages in the project."""
```

| Signal | Language | Frontend |
|--------|----------|----------|
| `*.ts`, `*.tsx`, `tsconfig.json` | TypeScript | jssrc2cpg (via tsc) |
| `*.js`, `*.jsx`, `package.json` | JavaScript | jssrc2cpg |
| `*.py`, `requirements.txt`, `pyproject.toml`, `setup.py` | Python | pysrc2cpg |
| `*.go`, `go.mod` | Go | gosrc2cpg |
| `*.java`, `pom.xml`, `build.gradle` | Java | javasrc2cpg |

### 2.2 Multi-Language Projects

Many web apps have multiple languages (e.g., TypeScript frontend + Python backend). Piranesi should:
1. Detect all languages present
2. Run Joern with the appropriate frontend for each
3. Merge findings across languages
4. Track cross-language data flows where possible (e.g., TypeScript frontend → Python API)

---

## 3. Python Support (Flask/Django/FastAPI)

**Estimated effort: 18-25h**

### 3.1 Python Framework Detection

| Signal | Framework |
|--------|-----------|
| `flask` in requirements/pyproject | Flask |
| `django` in requirements/pyproject | Django |
| `fastapi` in requirements/pyproject | FastAPI |

### 3.2 Python Source Specs

| Framework | Pattern | Source Type |
|-----------|---------|-------------|
| Flask | `request.form['key']` | request_body |
| Flask | `request.args.get('key')` | url_param |
| Flask | `request.json` | request_body |
| Flask | `request.headers.get('key')` | header |
| Django | `request.POST['key']` | request_body |
| Django | `request.GET['key']` | url_param |
| Django | `request.body` | request_body |
| FastAPI | Function parameter with `Body()` | request_body |
| FastAPI | Function parameter with `Query()` | url_param |
| FastAPI | Function parameter with `Path()` | request_param |

### 3.3 Python Sink Specs

| Pattern | Sink Type | CWE |
|---------|-----------|-----|
| `cursor.execute(f"...")` | sql_query | CWE-89 |
| `os.system(cmd)` | shell_exec | CWE-78 |
| `subprocess.run(cmd, shell=True)` | shell_exec | CWE-78 |
| `eval(expr)` | eval | CWE-94 |
| `open(path)` | file_read | CWE-22 |
| `render_template_string(html)` | html_output | CWE-79 |
| `Markup(html)` | html_output | CWE-79 |
| `requests.get(url)` | http_request | CWE-918 |

### 3.4 Python Sanitizer Specs

| Pattern | Mitigates |
|---------|-----------|
| `cursor.execute(sql, params)` (parameterized) | CWE-89 |
| `shlex.quote()` | CWE-78 |
| `markupsafe.escape()` | CWE-79 |
| `bleach.clean()` | CWE-79 |
| `os.path.realpath() + startswith()` | CWE-22 |

### 3.5 Transpilation

Python requires no transpilation. Skip the `tsc` step. Pass the project directory directly to Joern with `pysrc2cpg`.

---

## 4. Go Support (Gin/Echo/Chi)

**Estimated effort: 18-25h**

### 4.1 Go Framework Detection

| Signal | Framework |
|--------|-----------|
| `github.com/gin-gonic/gin` in go.mod | Gin |
| `github.com/labstack/echo` in go.mod | Echo |
| `github.com/go-chi/chi` in go.mod | Chi |
| `net/http` import | stdlib |

### 4.2 Go Source Specs

| Framework | Pattern | Source Type |
|-----------|---------|-------------|
| Gin | `c.Query("key")` | url_param |
| Gin | `c.PostForm("key")` | request_body |
| Gin | `c.Param("key")` | request_param |
| Gin | `c.GetHeader("key")` | header |
| Echo | `c.QueryParam("key")` | url_param |
| Echo | `c.FormValue("key")` | request_body |
| Echo | `c.Param("key")` | request_param |
| stdlib | `r.URL.Query().Get("key")` | url_param |
| stdlib | `r.FormValue("key")` | request_body |

### 4.3 Go Sink Specs

| Pattern | Sink Type | CWE |
|---------|-----------|-----|
| `db.Query(fmt.Sprintf(...))` | sql_query | CWE-89 |
| `db.Exec(fmt.Sprintf(...))` | sql_query | CWE-89 |
| `exec.Command(cmd)` | shell_exec | CWE-78 |
| `template.HTML(s)` | html_output | CWE-79 |
| `os.Open(path)` | file_read | CWE-22 |
| `http.Get(url)` | http_request | CWE-918 |

### 4.4 Go Sanitizer Specs

| Pattern | Mitigates |
|---------|-----------|
| `db.Query(sql, args...)` (parameterized) | CWE-89 |
| `html/template` (auto-escaping) | CWE-79 |
| `filepath.Clean() + strings.HasPrefix()` | CWE-22 |

---

## 5. Java Support (Spring Boot)

**Estimated effort: 18-25h**

### 5.1 Spring Boot Source Specs

| Annotation | Source Type |
|------------|-------------|
| `@RequestBody` | request_body |
| `@RequestParam` | url_param |
| `@PathVariable` | request_param |
| `@RequestHeader` | header |
| `@CookieValue` | cookie |
| `HttpServletRequest.getParameter()` | url_param |

### 5.2 Spring Boot Sink Specs

| Pattern | Sink Type | CWE |
|---------|-----------|-----|
| `jdbcTemplate.query(sql + input)` | sql_query | CWE-89 |
| `Runtime.exec(cmd)` | shell_exec | CWE-78 |
| `ProcessBuilder(cmd)` | shell_exec | CWE-78 |
| `new File(path)` | file_read | CWE-22 |
| `RestTemplate.getForObject(url)` | http_request | CWE-918 |
| `response.getWriter().write(html)` | html_output | CWE-79 |

### 5.3 Spring Security as Sanitizer

Spring Security provides CSRF protection, input validation (`@Valid`), and output encoding. Detect `spring-boot-starter-security` in dependencies and reduce confidence for XSS/CSRF findings in secured endpoints.

---

## 6. Joern Frontend Configuration

### 6.1 Frontend Selection

```python
LANGUAGE_TO_JOERN_FRONTEND = {
    "typescript": "jssrc2cpg",   # via tsc first
    "javascript": "jssrc2cpg",
    "python": "pysrc2cpg",
    "go": "gosrc2cpg",
    "java": "javasrc2cpg",
}
```

### 6.2 Multi-Frontend Pipeline

For multi-language projects, run Joern with each frontend sequentially, then merge the CPGs:

```
project/
  frontend/     → jssrc2cpg → cpg_js.bin
  backend/      → pysrc2cpg → cpg_py.bin
```

Cross-language taint tracking (TypeScript → Python API calls) is deferred to Phase 14.

---

## 7. Acceptance Criteria

- [ ] Language auto-detection for Python, Go, Java
- [ ] Framework detection for Flask/Django/FastAPI, Gin/Echo/Chi, Spring Boot
- [ ] Basic source/sink/sanitizer specs for each language (top 3 CWEs)
- [ ] Joern frontend selection based on detected language
- [ ] 10+ ground truth entries per language
- [ ] No regression on TypeScript/JavaScript test suite
- [ ] Plugin interface used for each language (Phase 12)
