from __future__ import annotations

import hashlib
import re
from dataclasses import dataclass
from pathlib import Path

from piranesi.models.finding import CandidateFinding
from piranesi.models.taint import (
    SourceLocation,
    TaintSink,
    TaintSource,
    TaintStep,
)

_FETCH_PATTERN = re.compile(  # fetch('/api/foo', ...) or fetch("/api/foo", ...)
    r"""fetch\(\s*['"]([^'"]+)['"]"""
)
_AXIOS_PATTERN = re.compile(  # axios.get('/api/foo') / axios.post(...)
    r"""axios\.(?:get|post|put|delete|patch)\(\s*['"]([^'"]+)['"]"""
)
_HTTP_CLIENT_PATTERN = re.compile(  # http.get('/api/foo') etc
    r"""(?:http|https)\.(?:get|post|put|delete|patch)\(\s*['"]([^'"]+)['"]"""
)
_FLASK_ROUTE_PATTERN = re.compile(  # @app.route("/api/foo") or @bp.route(...)
    r"""@\w+\.route\(\s*['"]([^'"]+)['"]"""
)
_EXPRESS_ROUTE_PATTERN = re.compile(  # app.get('/api/foo', ...) router.post(...)
    r"""(?:app|router)\.(?:get|post|put|delete|patch|all)\(\s*['"]([^'"]+)['"]"""
)
_GO_MUX_PATTERN = re.compile(  # http.HandleFunc("/api/foo", ...)
    r"""(?:HandleFunc|Handle)\(\s*['"]([^'"]+)['"]"""
)
_SPRING_MAPPING_PATTERN = re.compile(  # @GetMapping("/api/foo") @PostMapping(...)
    r"""@(?:Get|Post|Put|Delete|Patch|Request)Mapping\(\s*(?:value\s*=\s*)?['"]([^'"]+)['"]"""
)
_TAINT_SOURCE_PATTERNS_TS = re.compile(  # tainted data flowing into fetch body
    r"""(?:req\.body|req\.query|req\.params|document\.getElementById|"""
    r"""(?:use|get)(?:State|Input|Form)|\.value|formData|searchParams)"""
)
_PYTHON_SINK_PATTERNS = re.compile(  # known Python sinks, longer matches first
    r"""(?:cursor\.execute|os\.system|subprocess\.(?:run|call|Popen|check_output)|"""
    r"""render_template_string|requests\.get|open\(|(?<!\w)eval\(|(?<!\w)exec\()"""
)
_GO_SINK_PATTERNS = re.compile(
    r"""(?:db\.(?:Query|Exec)|exec\.Command|fmt\.Sprintf.*(?:Query|Exec)|os\.(?:Create|Open))"""
)
_JAVA_SINK_PATTERNS = re.compile(
    r"""(?:jdbcTemplate\.(?:query|update|execute)|statement\.execute|"""
    r"""Runtime\.getRuntime\(\)\.exec|ProcessBuilder)"""
)

_TS_EXTENSIONS = frozenset({".ts", ".tsx", ".js", ".jsx", ".mjs"})
_PYTHON_EXTENSIONS = frozenset({".py"})
_GO_EXTENSIONS = frozenset({".go"})
_JAVA_EXTENSIONS = frozenset({".java"})
_BACKEND_EXTENSIONS = _PYTHON_EXTENSIONS | _GO_EXTENSIONS | _JAVA_EXTENSIONS

_DEFAULT_CONFIDENCE = 0.6
_SEVERITY_MAP = {
    "CWE-89": "high",
    "CWE-78": "critical",
    "CWE-79": "medium",
    "CWE-94": "critical",
    "CWE-22": "medium",
    "CWE-918": "high",
}


@dataclass(frozen=True, slots=True)
class ApiBoundary:
    url_path: str
    file: str
    line: int
    snippet: str
    language: str  # "typescript" | "python" | "go" | "java"
    direction: str  # "client" (sends request) | "server" (handles request)


@dataclass(frozen=True, slots=True)
class CrossLanguageFlow:
    client: ApiBoundary
    server: ApiBoundary
    client_taint_snippet: str | None  # tainted data on the client side
    server_sink_snippet: str | None  # sink on the server side
    vuln_class: str
    cwe_id: str


def _normalize_route(route: str) -> str:
    """Normalize URL path for matching: strip trailing slash, collapse params."""
    route = route.split("?")[0].rstrip("/") or "/"
    route = re.sub(r"<[^>]+>", ":param", route)  # flask <param>
    route = re.sub(r"\{[^}]+\}", ":param", route)  # spring {param}
    route = re.sub(r":[^/]+", ":param", route)  # express :param
    return route


def _detect_language(file_path: str) -> str | None:
    suffix = Path(file_path).suffix.lower()
    if suffix in _TS_EXTENSIONS:
        return "typescript"
    if suffix in _PYTHON_EXTENSIONS:
        return "python"
    if suffix in _GO_EXTENSIONS:
        return "go"
    if suffix in _JAVA_EXTENSIONS:
        return "java"
    return None


def extract_api_boundaries(project_root: str | Path) -> list[ApiBoundary]:
    """Walk project tree and extract API boundaries (client fetch calls + server route defs)."""
    root = Path(project_root)
    boundaries: list[ApiBoundary] = []
    for path in sorted(root.rglob("*")):
        if not path.is_file():
            continue
        if any(
            part.startswith(".")
            or part
            in ("node_modules", "venv", ".venv", "__pycache__", "vendor", "target", "build", "dist")
            for part in path.parts
        ):
            continue
        lang = _detect_language(str(path))
        if lang is None:
            continue
        try:
            lines = path.read_text(encoding="utf-8", errors="replace").splitlines()
        except (OSError, UnicodeDecodeError):
            continue
        rel = str(path.relative_to(root))
        for line_no, line in enumerate(lines, start=1):
            _extract_line_boundaries(boundaries, rel, line_no, line, lang)
    return boundaries


def _extract_line_boundaries(
    out: list[ApiBoundary],
    rel_path: str,
    line_no: int,
    line: str,
    lang: str,
) -> None:
    # client-side fetch calls (TS/JS)
    if lang == "typescript":
        for pattern in (_FETCH_PATTERN, _AXIOS_PATTERN, _HTTP_CLIENT_PATTERN):
            for m in pattern.finditer(line):
                out.append(
                    ApiBoundary(
                        url_path=m.group(1),
                        file=rel_path,
                        line=line_no,
                        snippet=line.strip(),
                        language=lang,
                        direction="client",
                    )
                )
    # server-side route definitions
    if lang == "python":
        for m in _FLASK_ROUTE_PATTERN.finditer(line):
            out.append(
                ApiBoundary(
                    url_path=m.group(1),
                    file=rel_path,
                    line=line_no,
                    snippet=line.strip(),
                    language=lang,
                    direction="server",
                )
            )
    elif lang == "typescript":
        for m in _EXPRESS_ROUTE_PATTERN.finditer(line):
            out.append(
                ApiBoundary(
                    url_path=m.group(1),
                    file=rel_path,
                    line=line_no,
                    snippet=line.strip(),
                    language=lang,
                    direction="server",
                )
            )
    elif lang == "go":
        for m in _GO_MUX_PATTERN.finditer(line):
            out.append(
                ApiBoundary(
                    url_path=m.group(1),
                    file=rel_path,
                    line=line_no,
                    snippet=line.strip(),
                    language=lang,
                    direction="server",
                )
            )
    elif lang == "java":
        for m in _SPRING_MAPPING_PATTERN.finditer(line):
            out.append(
                ApiBoundary(
                    url_path=m.group(1),
                    file=rel_path,
                    line=line_no,
                    snippet=line.strip(),
                    language=lang,
                    direction="server",
                )
            )


def match_api_boundaries(boundaries: list[ApiBoundary]) -> list[tuple[ApiBoundary, ApiBoundary]]:
    """Match client→server boundaries by normalized URL path."""
    clients = [b for b in boundaries if b.direction == "client"]
    servers = [b for b in boundaries if b.direction == "server"]
    server_by_route: dict[str, list[ApiBoundary]] = {}
    for s in servers:
        key = _normalize_route(s.url_path)
        server_by_route.setdefault(key, []).append(s)
    matches: list[tuple[ApiBoundary, ApiBoundary]] = []
    for c in clients:
        key = _normalize_route(c.url_path)
        for s in server_by_route.get(key, []):
            if c.language != s.language:  # cross-language only
                matches.append((c, s))
    return matches


_FUNCTION_START_PATTERNS = {
    "python": re.compile(r"^\s*(?:@\w+\.route\(|def\s+)"),
    "go": re.compile(r"^func\s+"),
    "java": re.compile(r"^\s*(?:@\w+Mapping|public\s|private\s|protected\s)"),
}


def _find_taint_in_context(
    project_root: Path,
    boundary: ApiBoundary,
    *,
    search_range: int = 30,
) -> str | None:
    """Look in the function body near a boundary for tainted data or sinks."""
    fpath = project_root / boundary.file
    if not fpath.is_file():
        return None
    try:
        lines = fpath.read_text(encoding="utf-8", errors="replace").splitlines()
    except (OSError, UnicodeDecodeError):
        return None
    if boundary.direction == "client":
        start = max(0, boundary.line - search_range - 1)
        end = min(len(lines), boundary.line + search_range)
        context = "\n".join(lines[start:end])
        m = _TAINT_SOURCE_PATTERNS_TS.search(context)
        return m.group(0) if m else None
    # server side — scan forward from route line to next function/route def
    fn_start = _FUNCTION_START_PATTERNS.get(boundary.language)
    start = boundary.line  # line after the decorator (0-indexed = boundary.line)
    end = min(len(lines), boundary.line + search_range)
    if fn_start:
        for i in range(boundary.line + 1, end):  # skip current line
            if fn_start.search(lines[i]):
                end = i
                break
    context = "\n".join(lines[start:end])
    lang = boundary.language
    pattern = (
        _PYTHON_SINK_PATTERNS
        if lang == "python"
        else _GO_SINK_PATTERNS
        if lang == "go"
        else _JAVA_SINK_PATTERNS
    )
    m = pattern.search(context)
    return m.group(0) if m else None


def _classify_sink(sink_snippet: str) -> tuple[str, str]:
    """Return (vuln_class, cwe_id) for a detected sink."""
    s = sink_snippet.lower()
    if "execute" in s and (
        "cursor" in s or "query" in s or "jdbc" in s or "statement" in s or "db." in s
    ):
        return "sql_injection", "CWE-89"
    if any(
        k in s for k in ("os.system", "subprocess", "exec.command", "processbuilder", "runtime")
    ):
        return "command_injection", "CWE-78"
    if "render_template_string" in s:
        return "xss", "CWE-79"
    if "eval(" in s or "exec(" in s:
        return "code_injection", "CWE-94"
    if "open(" in s or "os.create" in s or "os.open" in s:
        return "path_traversal", "CWE-22"
    if "requests.get" in s or "http.get" in s:
        return "ssrf", "CWE-918"
    return "taint_flow", "CWE-20"


def _cross_language_finding_id(client: ApiBoundary, server: ApiBoundary, vuln_class: str) -> str:
    material = f"xlang|{client.file}:{client.line}|{server.file}:{server.line}|{vuln_class}"
    return hashlib.sha256(material.encode()).hexdigest()[:16]


def detect_cross_language_flows(
    project_root: str | Path,
    boundaries: list[ApiBoundary] | None = None,
) -> list[CrossLanguageFlow]:
    """Detect cross-language taint flows by matching client API calls to server routes."""
    root = Path(project_root)
    if boundaries is None:
        boundaries = extract_api_boundaries(root)
    matched = match_api_boundaries(boundaries)
    flows: list[CrossLanguageFlow] = []
    for client, server in matched:
        client_taint = _find_taint_in_context(root, client)
        server_sink = _find_taint_in_context(root, server)
        if server_sink is None:  # no sink on backend = no finding
            continue
        vuln_class, cwe_id = _classify_sink(server_sink)
        flows.append(
            CrossLanguageFlow(
                client=client,
                server=server,
                client_taint_snippet=client_taint,
                server_sink_snippet=server_sink,
                vuln_class=vuln_class,
                cwe_id=cwe_id,
            )
        )
    return flows


def cross_language_findings(
    project_root: str | Path,
    boundaries: list[ApiBoundary] | None = None,
) -> list[CandidateFinding]:
    """Produce CandidateFindings for cross-language taint flows."""
    root = Path(project_root)
    flows = detect_cross_language_flows(root, boundaries)
    findings: list[CandidateFinding] = []
    for flow in flows:
        fid = _cross_language_finding_id(flow.client, flow.server, flow.vuln_class)
        source = TaintSource(
            location=SourceLocation(
                file=flow.client.file,
                line=flow.client.line,
                column=0,
                snippet=flow.client.snippet,
            ),
            source_type="request_body",
            data_categories=["user_input"],
            parameter_name=flow.client_taint_snippet,
        )
        sink = TaintSink(
            location=SourceLocation(
                file=flow.server.file,
                line=flow.server.line,
                column=0,
                snippet=flow.server.snippet,
            ),
            sink_type=flow.vuln_class,
            api_name=flow.server_sink_snippet or "unknown",
        )
        api_step = TaintStep(
            location=SourceLocation(
                file=flow.client.file,
                line=flow.client.line,
                column=0,
                snippet=(
                    "cross-language API call: "
                    f"{flow.client.language} -> {flow.server.language} "
                    f"via {flow.client.url_path}"
                ),
            ),
            operation="cross_language_api_call",
            taint_state="tainted",
            through_function=f"fetch({flow.client.url_path})",
        )
        server_step = TaintStep(
            location=SourceLocation(
                file=flow.server.file,
                line=flow.server.line,
                column=0,
                snippet=flow.server.snippet,
            ),
            operation="call_arg",
            taint_state="tainted",
            through_function=flow.server_sink_snippet,
        )
        severity = _SEVERITY_MAP.get(flow.cwe_id, "medium")
        findings.append(
            CandidateFinding(
                id=fid,
                vuln_class=flow.vuln_class,
                source=source,
                sink=sink,
                taint_path=[api_step, server_step],
                path_conditions=[],
                confidence=_DEFAULT_CONFIDENCE,
                severity=severity,
                metadata={
                    "cross_language": True,
                    "client_language": flow.client.language,
                    "server_language": flow.server.language,
                    "api_path": flow.client.url_path,
                },
            )
        )
    return findings
