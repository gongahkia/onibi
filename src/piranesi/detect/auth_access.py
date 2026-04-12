from __future__ import annotations

import hashlib
import re
from bisect import bisect_right
from collections.abc import Sequence
from dataclasses import dataclass
from pathlib import Path

from piranesi.detect.flows import severity_for_cwe
from piranesi.models import CandidateFinding, SourceLocation, TaintSink, TaintSource
from piranesi.scan.specs import SinkType, SourceType

_SOURCE_FILE_EXTENSIONS = frozenset({".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".py", ".java"})
_DEFAULT_DATA_CATEGORIES = ["unknown"]

_EXPRESS_ROUTE_CALL = re.compile(
    r"\b(?:app|router)\s*\.\s*(?P<method>get|post|put|delete|patch)\s*\(",
    re.IGNORECASE,
)
_JS_FUNCTION_PATTERN = r"[A-Za-z_$][\w$]*"
_JS_HANDLER_NAME = re.compile(rf"(?P<name>{_JS_FUNCTION_PATTERN})\s*$")
_JS_FUNCTION_DECLARATION = r"function\s+{name}\s*\([^)]*\)\s*\{{"
_JS_ARROW_DECLARATION = r"(?:const|let|var)\s+{name}\s*=\s*(?:async\s*)?\([^)]*\)\s*=>\s*\{{"
_JS_FUNCTION_EXPRESSION = (
    r"(?:const|let|var)\s+{name}\s*=\s*(?:async\s*)?function\s*\([^)]*\)\s*\{{"
)

_PYTHON_FUNCTION = re.compile(
    r"(?ms)(?P<decorators>(?:^[ \t]*@[^\n]+\n)*)"
    r"^[ \t]*def\s+(?P<name>[A-Za-z_]\w*)\s*\([^)]*\):(?P<body>.*?)"
    r"(?=^(?:[ \t]*def|[ \t]*class)\s+|\Z)"
)
_FLASK_ROUTE_DECORATOR = re.compile(
    r"@(?:\w+\.)?route\(\s*['\"](?P<path>[^'\"]+)['\"](?P<args>[^)]*)\)",
    re.IGNORECASE,
)
_FLASK_METHODS = re.compile(r"methods\s*=\s*\[(?P<methods>[^\]]+)\]", re.IGNORECASE)

_JAVA_ANNOTATED_METHOD = re.compile(
    r"(?ms)(?P<annotations>(?:^[ \t]*@[^\n]+\n)+)"
    r"[ \t]*(?P<signature>(?:public|private|protected)\s+[^{;\n]+\s+"
    r"(?P<name>[A-Za-z_]\w*)\s*\([^)]*\))\s*\{"
)
_JAVA_MAPPING_PATH = re.compile(
    r"@(?:Get|Post|Put|Delete|Patch|Request)Mapping\s*\(\s*(?:value\s*=\s*)?\"(?P<path>[^\"]+)\"",
    re.IGNORECASE,
)
_SPRING_ENTITY = re.compile(r"@Entity\b[\s\S]{0,200}?\bclass\s+(?P<name>[A-Za-z_]\w*)")

_CSRF_MIDDLEWARE_PRESENT = re.compile(
    r"(?:csurf|csrfProtection|lusca\.csrf|CSRFProtect|csrf\.init_app|helmet\.csrf)\s*\(",
    re.IGNORECASE,
)
_DJANGO_CSRF_EXEMPT = re.compile(r"@csrf_exempt\b")
_SPRING_CSRF_DISABLE = re.compile(r"\.csrf\(\)\s*\.\s*disable\(\)")
_SPRING_CSRF_DISABLE_LAMBDA = re.compile(r"\.csrf\(\s*\w+\s*->\s*\w+\.disable\(\)\s*\)")

_DB_LOOKUP_PATTERN = re.compile(
    r"\b(?:findById|findByPk|findUnique|findOne|findFirst|findByIdAndUpdate|"
    r"findOneAndUpdate|updateOne|updateMany|destroy|delete|remove|query|getReferenceById|getById)\s*\(",
    re.IGNORECASE,
)
_JS_ID_SOURCE = re.compile(
    r"req\.(?:params|query)\.(?:id|[A-Za-z_]\w*Id|uuid)\b|req\.param\s*\(",
    re.IGNORECASE,
)
_DJANGO_ID_LOOKUP = re.compile(r"\.objects\.(?:get|filter)\(\s*(?:pk|id)\s*=\s*(?:pk|id)\b")
_DJANGO_OWNERSHIP = re.compile(
    r"(?:request\.user|user\s*=\s*request\.user|owner\s*=\s*request\.user|author\s*=\s*request\.user)"
)
_SPRING_PRINCIPAL = re.compile(
    r"(?:SecurityContext|getPrincipal|getAuthentication|Principal\s+[A-Za-z_]\w*)"
)

_AUTH_MIDDLEWARE_HINT = re.compile(
    r"(?:requireAdmin|isAdmin|requireRole|checkRole|authorize|requireAuth|isAuthenticated|"
    r"ensureAuthenticated|requirePermission|checkPermission|guard|protect|authMiddleware|"
    r"adminMiddleware|roleMiddleware|passport\.authenticate|verifyToken|checkToken|authGuard|"
    r"jwtGuard|rolesGuard)",
    re.IGNORECASE,
)
_CSRF_GUARD_HINT = re.compile(r"(?:csurf|csrf|CsrfGuard|csrfProtection)", re.IGNORECASE)
_GLOBAL_AUTH_MIDDLEWARE = re.compile(
    r"app\.use\s*\(\s*(?:[^)]*(?:requireAuth|isAuthenticated|ensureAuthenticated|"
    r"passport\.authenticate|verifyToken|protect|authGuard))",
    re.IGNORECASE,
)
_NEST_GUARD_HINT = re.compile(r"@UseGuards\s*\(", re.IGNORECASE)
_NEST_ROLE_HINT = re.compile(r"@(?:Roles|UseGuards)\s*\(", re.IGNORECASE)

_TIMING_UNSAFE_COMPARE = re.compile(
    r"(?:"
    r"[^\n;]*(?:password|passwd|secret|token|apiKey|api_key|hash)[^\n;]*(?:===|==|!==|!=)[^\n;]*"
    r"|[^\n;]*(?:===|==|!==|!=)[^\n;]*(?:password|passwd|secret|token|apiKey|api_key|hash)[^\n;]*"
    r")",
    re.IGNORECASE,
)
_SAFE_SECRET_COMPARE = re.compile(
    r"(?:timingSafeEqual|constantTimeCompare|compare_digest|MessageDigest\.isEqual|"
    r"bcrypt\.(?:compare|compareSync)|argon2\.verify|scrypt\.(?:check|verify)|check_password)",
    re.IGNORECASE,
)
_PASSPORT_LOCAL_STRATEGY = re.compile(r"new\s+LocalStrategy\s*\(", re.IGNORECASE)
_DJANGO_RAW_PASSWORD_CHECK = re.compile(
    r"\.password\s*==\s*request\.(?:POST|GET|body)",
    re.IGNORECASE,
)
_DJANGO_PASSWORD_SAFE = re.compile(r"(?:\.check_password\s*\(|\bauthenticate\s*\()", re.IGNORECASE)
_JWT_CALL = re.compile(r"\bjwt\.(?P<kind>sign|verify|decode)\s*\(", re.IGNORECASE)
_JWT_ALG_NONE = re.compile(r"algorithms?\s*:\s*\[[^\]]*['\"]none['\"]", re.IGNORECASE)
_JWT_HS_WITH_ASYMMETRIC_KEY = re.compile(
    r"(?:publicKey|pubKey|certificate|cert|rsaPublicKey)[^)]*algorithms?\s*:\s*\[[^\]]*['\"]HS(?:256|384|512)['\"]",
    re.IGNORECASE | re.DOTALL,
)
_JWT_RS_WITH_SECRET = re.compile(
    r"(?:secret|sharedSecret|jwtSecret)[^)]*algorithms?\s*:\s*\[[^\]]*['\"]RS(?:256|384|512)['\"]",
    re.IGNORECASE | re.DOTALL,
)
_JWT_IGNORE_EXPIRATION = re.compile(r"ignoreExpiration\s*:\s*true", re.IGNORECASE)
_COOKIE_OBJECT = re.compile(r"cookie\s*:\s*\{(?P<body>.*?)\}", re.IGNORECASE | re.DOTALL)

_LOGIN_ROUTE = re.compile(r"/(?:auth/)?(?:login|signin|authenticate)\b", re.IGNORECASE)
_LOGIN_HINT = re.compile(r"\b(?:authenticate|login|signIn|sign_in)\s*\(", re.IGNORECASE)
_SESSION_ASSIGNMENT_JS = re.compile(r"(?:req\.)?session\.[A-Za-z_]\w*\s*=", re.IGNORECASE)
_SESSION_REGENERATE = re.compile(r"(?:req\.)?session\.regenerate\s*\(", re.IGNORECASE)
_SESSION_ASSIGNMENT_PY = re.compile(
    r"(?:session|request\.session)\[\s*['\"](?:user_id|_auth_user_id)['\"]\s*\]\s*=",
    re.IGNORECASE,
)
_FLASK_SESSION_CLEAR = re.compile(r"session\.clear\s*\(")
_DJANGO_AUTH_LOGIN = re.compile(r"\blogin\s*\(\s*request\b", re.IGNORECASE)
_DJANGO_SESSION_CYCLE = re.compile(r"(?:request\.)?session\.cycle_key\s*\(", re.IGNORECASE)
_SPRING_SESSION_FIXATION_NONE = re.compile(r"sessionFixation\(\)\s*\.\s*none\(\)", re.IGNORECASE)

_SEQUELIZE_MASS_ASSIGN = re.compile(
    r"\.(?:create|update|build|upsert|bulkCreate)\s*\(\s*(?:req|request)\.body\b",
    re.IGNORECASE,
)
_MONGOOSE_MASS_ASSIGN = re.compile(
    r"(?:new\s+[A-Za-z_]\w*\s*\(\s*(?:req|request)\.body\b|"
    r"\.(?:findByIdAndUpdate|findOneAndUpdate|updateOne|updateMany)\s*\([^,]+,\s*(?:req|request)\.body\b)",
    re.IGNORECASE,
)
_PRISMA_MASS_ASSIGN = re.compile(
    r"prisma\.[A-Za-z_]\w*\.(?:create|update|upsert)\s*\(\s*\{\s*data\s*:\s*(?:req|request)\.body\b",
    re.IGNORECASE | re.DOTALL,
)
_DJANGO_FIELDS_ALL = re.compile(r"fields\s*=\s*['\"]__all__['\"]", re.IGNORECASE)
_DJANGO_DIRECT_UNPACK = re.compile(
    r"\.objects\.(?:create|update_or_create|get_or_create)\s*\(\s*\*\*request\.(?:POST|data|body)",
    re.IGNORECASE,
)
_MASS_ASSIGN_SAFE_HINT = re.compile(
    r"(?:\b(?:pick|omit|parse|safeParse|plainToClass|plainToInstance)\s*\(|"
    r"(?:zod|joi|class-validator|pydantic|schema)\b)",
    re.IGNORECASE,
)
_REQUEST_BODY_PARAM = re.compile(
    r"@RequestBody(?:\s+@Valid)?\s+(?P<type>[A-Za-z_]\w*)\s+(?P<var>[A-Za-z_]\w*)"
)

_ADMIN_ROUTE_PATTERNS = re.compile(
    r"(?:"
    r"/admin(?:/.*)?|/api/v\d+/admin(?:/.*)?|/api/v\d+/manage(?:/.*)?|"
    r"/manage(?:/.*)?|/dashboard/admin(?:/.*)?|/internal(?:/.*)?|"
    r"/superuser(?:/.*)?|/settings/global(?:/.*)?"
    r")",
    re.IGNORECASE,
)
_DJANGO_ADMIN_VIEW = re.compile(r"^(?:admin_|manage_|delete_)", re.IGNORECASE)
_DJANGO_PERMISSION_DECORATOR = re.compile(
    r"@(?:login_required|permission_required|user_passes_test|staff_member_required)",
    re.IGNORECASE,
)
_SPRING_AUTH_ANNOTATIONS = re.compile(
    r"@(?:PreAuthorize|Secured|RolesAllowed)",
    re.IGNORECASE,
)

_CRITICAL_ROUTE_KEYWORDS = frozenset(
    {
        "delete",
        "remove",
        "destroy",
        "password",
        "reset-password",
        "change-password",
        "payment",
        "charge",
        "purchase",
        "checkout",
        "billing",
        "transfer",
        "withdraw",
        "deposit",
        "settings",
        "profile/edit",
        "account",
        "api-key",
        "token",
        "secret",
        "export",
        "download/all",
        "backup",
        "invite",
        "grant",
        "revoke",
    }
)
_CRITICAL_HANDLER_KEYWORDS = re.compile(
    r"(?:deleteUser|removeAccount|resetPassword|changePassword|processPayment|chargeCard|"
    r"transferFunds|updateRole|generateApiKey|revokeToken|exportData|deleteAll|bulkDelete|purge|wipe)",
    re.IGNORECASE,
)
_PUBLIC_BY_DESIGN = frozenset(
    {
        "/login",
        "/signin",
        "/signup",
        "/register",
        "/forgot-password",
        "/reset-password",
        "/health",
        "/healthcheck",
        "/ping",
        "/public",
        "/oauth/callback",
    }
)
_FINANCIAL_KEYWORDS = frozenset(
    {"transfer", "payment", "purchase", "withdraw", "deposit", "charge"}
)
_SENSITIVE_FIELDS = frozenset(
    {
        "isAdmin",
        "is_admin",
        "isStaff",
        "is_staff",
        "isSuperuser",
        "is_superuser",
        "role",
        "roles",
        "permission",
        "permissions",
        "admin",
        "price",
        "balance",
        "credit",
        "amount",
        "verified",
        "approved",
        "active",
        "deleted",
        "passwordHash",
        "password_hash",
        "password",
    }
)


@dataclass(frozen=True, slots=True)
class AuthAccessConfig:
    enable_csrf: bool = True
    enable_idor: bool = True
    enable_broken_auth: bool = True
    enable_session_fixation: bool = True
    enable_mass_assignment: bool = True
    enable_privilege_escalation: bool = True
    enable_missing_auth: bool = True
    confidence_floor: float = 0.3


@dataclass(frozen=True, slots=True)
class _ScannedFile:
    path: Path
    text: str
    line_starts: tuple[int, ...]
    brace_pairs: dict[int, int]

    @classmethod
    def load(cls, path: Path) -> _ScannedFile | None:
        try:
            text = path.read_text(encoding="utf-8")
        except OSError:
            return None
        return cls(
            path=path.resolve(strict=False),
            text=text,
            line_starts=_line_starts(text),
            brace_pairs=_brace_pairs(text),
        )

    def location_for_index(self, index: int, *, snippet: str | None = None) -> SourceLocation:
        line_number, column_number = _line_and_column(self.line_starts, index)
        return SourceLocation(
            file=str(self.path),
            line=line_number,
            column=column_number,
            snippet=snippet or _line_text(self.text, line_number),
        )

    def containing_block(self, index: int) -> tuple[int, int]:
        block_start = 0
        block_end = len(self.text)
        for start, end in self.brace_pairs.items():
            if start < index < end and start >= block_start:
                block_start = start
                block_end = end
        return block_start, block_end


@dataclass(frozen=True, slots=True)
class _AuthAccessContext:
    has_global_js_csrf: bool
    has_flask_csrf: bool
    django_csrf_enabled: bool
    has_global_js_auth: bool
    spring_entity_names: frozenset[str]

    @classmethod
    def build(cls, scanned_files: Sequence[_ScannedFile]) -> _AuthAccessContext:
        has_global_js_csrf = any(
            _CSRF_MIDDLEWARE_PRESENT.search(file.text) for file in scanned_files
        )
        has_flask_csrf = any(
            file.path.suffix == ".py"
            and re.search(r"(?:CSRFProtect|csrf\.init_app)\s*\(", file.text)
            for file in scanned_files
        )
        django_settings = [file for file in scanned_files if file.path.name.endswith("settings.py")]
        if django_settings:
            django_csrf_enabled = any("CsrfViewMiddleware" in file.text for file in django_settings)
        else:
            middleware_files = [file for file in scanned_files if "MIDDLEWARE" in file.text]
            django_csrf_enabled = not middleware_files or any(
                "CsrfViewMiddleware" in file.text for file in middleware_files
            )
        has_global_js_auth = any(
            _GLOBAL_AUTH_MIDDLEWARE.search(file.text) for file in scanned_files
        )
        spring_entity_names = frozenset(
            match.group("name")
            for file in scanned_files
            if file.path.suffix == ".java"
            for match in _SPRING_ENTITY.finditer(file.text)
        )
        return cls(
            has_global_js_csrf=has_global_js_csrf,
            has_flask_csrf=has_flask_csrf,
            django_csrf_enabled=django_csrf_enabled,
            has_global_js_auth=has_global_js_auth,
            spring_entity_names=spring_entity_names,
        )


@dataclass(frozen=True, slots=True)
class _JsRoute:
    method: str
    path: str | None
    call_text: str
    middleware: tuple[str, ...]
    handler_text: str
    start_index: int


@dataclass(frozen=True, slots=True)
class _PythonFunction:
    name: str
    decorators: str
    body: str
    full_text: str
    start_index: int


@dataclass(frozen=True, slots=True)
class _JavaMethod:
    name: str
    annotations: str
    signature: str
    body: str
    full_text: str
    start_index: int
    path: str | None


def extract_auth_access_findings(
    project_root: str | Path,
    *,
    frameworks: Sequence[str] | None = None,
    files: Sequence[Path] | None = None,
    config: AuthAccessConfig | None = None,
) -> tuple[CandidateFinding, ...]:
    del frameworks
    cfg = config or AuthAccessConfig()
    root = Path(project_root).resolve(strict=False)
    scanned_files = tuple(_load_scanned_files(root, files=files))
    if not scanned_files:
        return ()

    context = _AuthAccessContext.build(scanned_files)
    findings: list[CandidateFinding] = []

    if cfg.enable_csrf:
        findings.extend(_detect_csrf(scanned_files, context))
    if cfg.enable_idor:
        findings.extend(_detect_idor(scanned_files))
    if cfg.enable_broken_auth:
        findings.extend(_detect_broken_auth(scanned_files))
    if cfg.enable_session_fixation:
        findings.extend(_detect_session_fixation(scanned_files))
    if cfg.enable_mass_assignment:
        findings.extend(_detect_mass_assignment(scanned_files, context))
    if cfg.enable_privilege_escalation:
        findings.extend(_detect_privilege_escalation(scanned_files, context))
    if cfg.enable_missing_auth:
        findings.extend(_detect_missing_auth(scanned_files, context))

    return tuple(
        finding
        for finding in _dedupe_findings(findings)
        if finding.confidence >= cfg.confidence_floor
    )


def _detect_csrf(
    scanned_files: Sequence[_ScannedFile],
    context: _AuthAccessContext,
) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    for scanned_file in scanned_files:
        suffix = scanned_file.path.suffix
        if suffix in {".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs"}:
            for route in _iter_express_routes(scanned_file):
                if route.method not in {"post", "put", "delete", "patch"}:
                    continue
                if context.has_global_js_csrf or any(
                    _CSRF_GUARD_HINT.search(argument) for argument in route.middleware
                ):
                    continue
                findings.append(
                    _build_static_finding(
                        cwe_id="CWE-352",
                        location=scanned_file.location_for_index(route.start_index),
                        source_type=SourceType.REQUEST_BODY.value,
                        sink_type=SinkType.STATE_CHANGE_HANDLER.value,
                        api_name=f"{route.method.upper()} {route.path or '<unknown>'}",
                        parameter_name=route.path,
                        confidence=0.6,
                        severity=(
                            "high"
                            if _is_financial_route(route.path, route.handler_text)
                            else severity_for_cwe("CWE-352")
                        ),
                        metadata={"framework": "express"},
                    )
                )
            if (
                re.search(r"@(?:Post|Put|Delete|Patch)\s*\(", scanned_file.text)
                and "@Controller" in scanned_file.text
                and not context.has_global_js_csrf
                and not re.search(r"@UseGuards\s*\([^)]*Csrf", scanned_file.text, re.IGNORECASE)
            ):
                match = re.search(r"@(?:Post|Put|Delete|Patch)\s*\(", scanned_file.text)
                assert match is not None
                findings.append(
                    _build_static_finding(
                        cwe_id="CWE-352",
                        location=scanned_file.location_for_index(match.start()),
                        source_type=SourceType.REQUEST_BODY.value,
                        sink_type=SinkType.STATE_CHANGE_HANDLER.value,
                        api_name="NestJS state-changing handler",
                        parameter_name=None,
                        confidence=0.4,
                        metadata={"framework": "nestjs"},
                    )
                )
        elif suffix == ".py":
            for match in _DJANGO_CSRF_EXEMPT.finditer(scanned_file.text):
                findings.append(
                    _build_static_finding(
                        cwe_id="CWE-352",
                        location=scanned_file.location_for_index(match.start()),
                        source_type=SourceType.REQUEST_BODY.value,
                        sink_type=SinkType.STATE_CHANGE_HANDLER.value,
                        api_name="csrf_exempt",
                        parameter_name="csrf_exempt",
                        confidence=0.7,
                        metadata={"framework": "django"},
                    )
                )
            if (
                scanned_file.path.name.endswith("settings.py")
                and "MIDDLEWARE" in scanned_file.text
                and "CsrfViewMiddleware" not in scanned_file.text
                and not context.django_csrf_enabled
            ):
                index = scanned_file.text.index("MIDDLEWARE")
                findings.append(
                    _build_static_finding(
                        cwe_id="CWE-352",
                        location=scanned_file.location_for_index(index),
                        source_type="security_configuration",
                        sink_type=SinkType.STATE_CHANGE_HANDLER.value,
                        api_name="Django CSRF middleware",
                        parameter_name="CsrfViewMiddleware",
                        confidence=0.65,
                        metadata={"framework": "django"},
                    )
                )
            for function in _iter_python_functions(scanned_file):
                route_path, methods = _flask_route_info(function.decorators)
                if route_path is None or "POST" not in methods or context.has_flask_csrf:
                    continue
                findings.append(
                    _build_static_finding(
                        cwe_id="CWE-352",
                        location=scanned_file.location_for_index(function.start_index),
                        source_type=SourceType.REQUEST_BODY.value,
                        sink_type=SinkType.STATE_CHANGE_HANDLER.value,
                        api_name=f"POST {route_path}",
                        parameter_name=route_path,
                        confidence=0.55,
                        metadata={"framework": "flask"},
                    )
                )
        elif suffix == ".java":
            for pattern in (_SPRING_CSRF_DISABLE, _SPRING_CSRF_DISABLE_LAMBDA):
                for match in pattern.finditer(scanned_file.text):
                    findings.append(
                        _build_static_finding(
                            cwe_id="CWE-352",
                            location=scanned_file.location_for_index(match.start()),
                            source_type="security_configuration",
                            sink_type=SinkType.STATE_CHANGE_HANDLER.value,
                            api_name="Spring csrf().disable()",
                            parameter_name="csrf",
                            confidence=0.7,
                            metadata={"framework": "spring"},
                        )
                    )
    return findings


def _detect_idor(scanned_files: Sequence[_ScannedFile]) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    for scanned_file in scanned_files:
        suffix = scanned_file.path.suffix
        if suffix in {".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs"}:
            for route in _iter_express_routes(scanned_file):
                if not route.handler_text or not _DB_LOOKUP_PATTERN.search(route.handler_text):
                    continue
                if not _JS_ID_SOURCE.search(route.handler_text) and not (
                    route.path and ":" in route.path and "id" in route.path.lower()
                ):
                    continue
                if _has_strong_ownership_check(route.handler_text):
                    continue
                confidence = 0.45
                if route.path and re.search(
                    r"/:[A-Za-z_]\w*id\b|/:id\b", route.path, re.IGNORECASE
                ):
                    confidence = 0.55
                confidence = 0.6 if not _has_user_context(route.handler_text) else 0.3
                findings.append(
                    _build_static_finding(
                        cwe_id="CWE-639",
                        location=scanned_file.location_for_index(route.start_index),
                        source_type=SourceType.REQUEST_PARAM.value,
                        sink_type=SinkType.AUTH_SENSITIVE.value,
                        api_name=f"{route.method.upper()} {route.path or '<unknown>'}",
                        parameter_name=route.path,
                        confidence=confidence,
                        metadata={"framework": "express"},
                    )
                )
            if (
                "@Controller" in scanned_file.text
                and re.search(r"@Get\s*\(\s*['\"]:id['\"]\s*\)", scanned_file.text)
                and _DB_LOOKUP_PATTERN.search(scanned_file.text)
                and not _has_strong_ownership_check(scanned_file.text)
                and not re.search(r"@(?:UseGuards|Authorize)\s*\(", scanned_file.text)
            ):
                match = re.search(r"@Get\s*\(\s*['\"]:id['\"]\s*\)", scanned_file.text)
                assert match is not None
                findings.append(
                    _build_static_finding(
                        cwe_id="CWE-639",
                        location=scanned_file.location_for_index(match.start()),
                        source_type=SourceType.REQUEST_PARAM.value,
                        sink_type=SinkType.AUTH_SENSITIVE.value,
                        api_name="NestJS :id lookup",
                        parameter_name=":id",
                        confidence=0.5,
                        metadata={"framework": "nestjs"},
                    )
                )
        elif suffix == ".py":
            for function in _iter_python_functions(scanned_file):
                if _DJANGO_ID_LOOKUP.search(function.full_text) and not _DJANGO_OWNERSHIP.search(
                    function.full_text
                ):
                    findings.append(
                        _build_static_finding(
                            cwe_id="CWE-639",
                            location=scanned_file.location_for_index(function.start_index),
                            source_type=SourceType.REQUEST_PARAM.value,
                            sink_type=SinkType.AUTH_SENSITIVE.value,
                            api_name=function.name,
                            parameter_name=function.name,
                            confidence=0.55,
                            metadata={"framework": "django"},
                        )
                    )
        elif suffix == ".java":
            for method in _iter_java_methods(scanned_file):
                if not method.path or "{id}" not in method.path:
                    continue
                if "@PathVariable" not in method.signature or "findById(" not in method.body:
                    continue
                if _SPRING_PRINCIPAL.search(method.full_text) or _SPRING_AUTH_ANNOTATIONS.search(
                    method.annotations
                ):
                    continue
                findings.append(
                    _build_static_finding(
                        cwe_id="CWE-639",
                        location=scanned_file.location_for_index(method.start_index),
                        source_type=SourceType.REQUEST_PARAM.value,
                        sink_type=SinkType.AUTH_SENSITIVE.value,
                        api_name=f"{method.name}({method.path})",
                        parameter_name=method.path,
                        confidence=0.55,
                        metadata={"framework": "spring"},
                    )
                )
    return findings


def _detect_broken_auth(scanned_files: Sequence[_ScannedFile]) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    for scanned_file in scanned_files:
        suffix = scanned_file.path.suffix
        if suffix in {".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".java"}:
            for match in _TIMING_UNSAFE_COMPARE.finditer(scanned_file.text):
                block = _brace_scoped_text(scanned_file, match.start())
                if _SAFE_SECRET_COMPARE.search(block):
                    continue
                findings.append(
                    _build_static_finding(
                        cwe_id="CWE-287",
                        location=scanned_file.location_for_index(match.start()),
                        source_type="security_configuration",
                        sink_type=SinkType.AUTH_SENSITIVE.value,
                        api_name="timing-unsafe secret comparison",
                        parameter_name="secret_compare",
                        confidence=0.5,
                        severity="critical",
                    )
                )
            for match in _JWT_CALL.finditer(scanned_file.text):
                call_text, _ = _extract_enclosed(scanned_file.text, match.end() - 1, "(", ")")
                kind = match.group("kind").lower()
                if kind == "verify" and _JWT_ALG_NONE.search(call_text):
                    findings.append(
                        _build_static_finding(
                            cwe_id="CWE-287",
                            location=scanned_file.location_for_index(match.start()),
                            source_type="security_configuration",
                            sink_type=SinkType.AUTH_SENSITIVE.value,
                            api_name="jwt.verify",
                            parameter_name="alg=none",
                            confidence=0.65,
                            severity="critical",
                        )
                    )
                    continue
                if kind == "verify" and (
                    _JWT_HS_WITH_ASYMMETRIC_KEY.search(call_text)
                    or _JWT_RS_WITH_SECRET.search(call_text)
                ):
                    findings.append(
                        _build_static_finding(
                            cwe_id="CWE-287",
                            location=scanned_file.location_for_index(match.start()),
                            source_type="security_configuration",
                            sink_type=SinkType.AUTH_SENSITIVE.value,
                            api_name="jwt.verify",
                            parameter_name="jwt algorithm confusion",
                            confidence=0.55,
                        )
                    )
                    continue
                if kind == "verify" and _JWT_IGNORE_EXPIRATION.search(call_text):
                    findings.append(
                        _build_static_finding(
                            cwe_id="CWE-287",
                            location=scanned_file.location_for_index(match.start()),
                            source_type="security_configuration",
                            sink_type=SinkType.AUTH_SENSITIVE.value,
                            api_name="jwt.verify",
                            parameter_name="ignoreExpiration",
                            confidence=0.45,
                            severity="medium",
                        )
                    )
                    continue
                if kind == "sign" and "expiresIn" not in call_text and "exp:" not in call_text:
                    findings.append(
                        _build_static_finding(
                            cwe_id="CWE-287",
                            location=scanned_file.location_for_index(match.start()),
                            source_type="security_configuration",
                            sink_type=SinkType.AUTH_SENSITIVE.value,
                            api_name="jwt.sign",
                            parameter_name="missing expiry",
                            confidence=0.45,
                            severity="medium",
                        )
                    )
            for match in _COOKIE_OBJECT.finditer(scanned_file.text):
                body = match.group("body")
                missing_flags: list[str] = []
                if "httpOnly" not in body:
                    missing_flags.append("httpOnly")
                if "secure" not in body:
                    missing_flags.append("secure")
                if "sameSite" not in body:
                    missing_flags.append("sameSite")
                if re.search(
                    r"sameSite\s*:\s*['\"]none['\"]", body, re.IGNORECASE
                ) and not re.search(r"secure\s*:\s*true", body, re.IGNORECASE):
                    missing_flags.append("sameSite=none_without_secure")
                if not missing_flags:
                    continue
                findings.append(
                    _build_static_finding(
                        cwe_id="CWE-287",
                        location=scanned_file.location_for_index(match.start()),
                        source_type="security_configuration",
                        sink_type=SinkType.AUTH_SENSITIVE.value,
                        api_name="session.cookie",
                        parameter_name=", ".join(missing_flags),
                        confidence=0.45,
                        severity="medium",
                        metadata={"missing_flags": missing_flags},
                    )
                )
            for match in _PASSPORT_LOCAL_STRATEGY.finditer(scanned_file.text):
                strategy_text = _brace_scoped_text(scanned_file, match.start())
                if _SAFE_SECRET_COMPARE.search(strategy_text):
                    continue
                if not _TIMING_UNSAFE_COMPARE.search(strategy_text):
                    continue
                findings.append(
                    _build_static_finding(
                        cwe_id="CWE-287",
                        location=scanned_file.location_for_index(match.start()),
                        source_type="security_configuration",
                        sink_type=SinkType.AUTH_SENSITIVE.value,
                        api_name="passport LocalStrategy",
                        parameter_name="plaintext password compare",
                        confidence=0.6,
                        severity="critical",
                    )
                )
        elif suffix == ".py":
            for function in _iter_python_functions(scanned_file):
                if _DJANGO_RAW_PASSWORD_CHECK.search(
                    function.full_text
                ) and not _DJANGO_PASSWORD_SAFE.search(function.full_text):
                    findings.append(
                        _build_static_finding(
                            cwe_id="CWE-287",
                            location=scanned_file.location_for_index(function.start_index),
                            source_type="security_configuration",
                            sink_type=SinkType.AUTH_SENSITIVE.value,
                            api_name=function.name,
                            parameter_name="raw password compare",
                            confidence=0.7,
                            severity="critical",
                            metadata={"framework": "django"},
                        )
                    )
                elif _TIMING_UNSAFE_COMPARE.search(
                    function.full_text
                ) and not _SAFE_SECRET_COMPARE.search(function.full_text):
                    timing_match = _TIMING_UNSAFE_COMPARE.search(function.full_text)
                    assert timing_match is not None
                    findings.append(
                        _build_static_finding(
                            cwe_id="CWE-287",
                            location=scanned_file.location_for_index(
                                function.start_index + timing_match.start()
                            ),
                            source_type="security_configuration",
                            sink_type=SinkType.AUTH_SENSITIVE.value,
                            api_name=function.name,
                            parameter_name="timing-unsafe secret comparison",
                            confidence=0.5,
                            severity="critical",
                        )
                    )
    return findings


def _detect_session_fixation(scanned_files: Sequence[_ScannedFile]) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    for scanned_file in scanned_files:
        suffix = scanned_file.path.suffix
        if suffix in {".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs"}:
            for route in _iter_express_routes(scanned_file):
                if route.method != "post":
                    continue
                handler_text = route.handler_text
                if not handler_text:
                    continue
                if not _LOGIN_ROUTE.search(route.path or "") and not _LOGIN_HINT.search(
                    handler_text
                ):
                    continue
                if not _SESSION_ASSIGNMENT_JS.search(handler_text):
                    continue
                if _SESSION_REGENERATE.search(handler_text):
                    continue
                findings.append(
                    _build_static_finding(
                        cwe_id="CWE-384",
                        location=scanned_file.location_for_index(route.start_index),
                        source_type="security_configuration",
                        sink_type=SinkType.STATE_CHANGE_HANDLER.value,
                        api_name=f"{route.method.upper()} {route.path or '<unknown>'}",
                        parameter_name=route.path,
                        confidence=0.55,
                        metadata={"framework": "express"},
                    )
                )
        elif suffix == ".py":
            for function in _iter_python_functions(scanned_file):
                route_path, methods = _flask_route_info(function.decorators)
                if route_path and "POST" in methods and _LOGIN_ROUTE.search(route_path):
                    if _SESSION_ASSIGNMENT_PY.search(
                        function.full_text
                    ) and not _FLASK_SESSION_CLEAR.search(function.full_text):
                        findings.append(
                            _build_static_finding(
                                cwe_id="CWE-384",
                                location=scanned_file.location_for_index(function.start_index),
                                source_type="security_configuration",
                                sink_type=SinkType.STATE_CHANGE_HANDLER.value,
                                api_name=function.name,
                                parameter_name=route_path,
                                confidence=0.5,
                                metadata={"framework": "flask"},
                            )
                        )
                    continue
                if _SESSION_ASSIGNMENT_PY.search(function.full_text) and not (
                    _DJANGO_SESSION_CYCLE.search(function.full_text)
                    or _DJANGO_AUTH_LOGIN.search(function.full_text)
                ):
                    findings.append(
                        _build_static_finding(
                            cwe_id="CWE-384",
                            location=scanned_file.location_for_index(function.start_index),
                            source_type="security_configuration",
                            sink_type=SinkType.STATE_CHANGE_HANDLER.value,
                            api_name=function.name,
                            parameter_name="session fixation",
                            confidence=0.6,
                            metadata={"framework": "django"},
                        )
                    )
        elif suffix == ".java":
            for match in _SPRING_SESSION_FIXATION_NONE.finditer(scanned_file.text):
                findings.append(
                    _build_static_finding(
                        cwe_id="CWE-384",
                        location=scanned_file.location_for_index(match.start()),
                        source_type="security_configuration",
                        sink_type=SinkType.STATE_CHANGE_HANDLER.value,
                        api_name="sessionFixation().none()",
                        parameter_name="session fixation",
                        confidence=0.7,
                        metadata={"framework": "spring"},
                    )
                )
    return findings


def _detect_mass_assignment(
    scanned_files: Sequence[_ScannedFile],
    context: _AuthAccessContext,
) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    for scanned_file in scanned_files:
        suffix = scanned_file.path.suffix
        if suffix in {".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs"}:
            for pattern, api_name in (
                (_SEQUELIZE_MASS_ASSIGN, "ORM.create/update(req.body)"),
                (_MONGOOSE_MASS_ASSIGN, "Mongoose mass assignment"),
                (_PRISMA_MASS_ASSIGN, "Prisma data: req.body"),
            ):
                for match in pattern.finditer(scanned_file.text):
                    block = _brace_scoped_text(scanned_file, match.start())
                    if _MASS_ASSIGN_SAFE_HINT.search(block):
                        continue
                    findings.append(
                        _build_static_finding(
                            cwe_id="CWE-915",
                            location=scanned_file.location_for_index(match.start()),
                            source_type=SourceType.REQUEST_BODY.value,
                            sink_type=SinkType.ORM_WRITE.value,
                            api_name=api_name,
                            parameter_name="req.body",
                            confidence=_mass_assignment_confidence(scanned_file.text, block),
                        )
                    )
        elif suffix == ".py":
            for pattern, api_name in (
                (_DJANGO_FIELDS_ALL, "ModelForm fields='__all__'"),
                (_DJANGO_DIRECT_UNPACK, "objects.create(**request.POST)"),
            ):
                for match in pattern.finditer(scanned_file.text):
                    findings.append(
                        _build_static_finding(
                            cwe_id="CWE-915",
                            location=scanned_file.location_for_index(match.start()),
                            source_type=SourceType.REQUEST_BODY.value,
                            sink_type=SinkType.ORM_WRITE.value,
                            api_name=api_name,
                            parameter_name="request.POST",
                            confidence=0.55,
                            metadata={"framework": "django"},
                        )
                    )
        elif suffix == ".java":
            for method in _iter_java_methods(scanned_file):
                request_body_match = _REQUEST_BODY_PARAM.search(method.signature)
                if request_body_match is None:
                    continue
                body_type = request_body_match.group("type")
                body_var = request_body_match.group("var")
                if body_type.endswith(("Dto", "DTO", "Request", "Payload")):
                    continue
                if context.spring_entity_names and body_type not in context.spring_entity_names:
                    continue
                if not re.search(
                    rf"\bsave(?:AndFlush)?\s*\(\s*{re.escape(body_var)}\s*\)", method.body
                ):
                    continue
                findings.append(
                    _build_static_finding(
                        cwe_id="CWE-915",
                        location=scanned_file.location_for_index(method.start_index),
                        source_type=SourceType.REQUEST_BODY.value,
                        sink_type=SinkType.ORM_WRITE.value,
                        api_name=method.name,
                        parameter_name=body_type,
                        confidence=0.6,
                        metadata={"framework": "spring"},
                    )
                )
    return findings


def _detect_privilege_escalation(
    scanned_files: Sequence[_ScannedFile],
    context: _AuthAccessContext,
) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    for scanned_file in scanned_files:
        suffix = scanned_file.path.suffix
        if suffix in {".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs"}:
            for route in _iter_express_routes(scanned_file):
                if not route.path or not _ADMIN_ROUTE_PATTERNS.search(route.path):
                    continue
                if _route_has_auth(route.middleware):
                    continue
                confidence = (
                    0.6
                    if route.method == "delete"
                    or re.search(
                        r"\.(?:destroy|delete|remove)\s*\(",
                        route.handler_text,
                        re.IGNORECASE,
                    )
                    else 0.45
                )
                if context.has_global_js_auth:
                    confidence = 0.3
                findings.append(
                    _build_static_finding(
                        cwe_id="CWE-269",
                        location=scanned_file.location_for_index(route.start_index),
                        source_type="security_configuration",
                        sink_type=SinkType.AUTH_SENSITIVE.value,
                        api_name=f"{route.method.upper()} {route.path}",
                        parameter_name=route.path,
                        confidence=confidence,
                        metadata={"framework": "express"},
                    )
                )
            if (
                re.search(r"@Controller\s*\(\s*['\"]admin", scanned_file.text, re.IGNORECASE)
                and re.search(r"@(?:Delete|Post|Put|Patch)\s*\(", scanned_file.text)
                and not _NEST_ROLE_HINT.search(scanned_file.text)
            ):
                match = re.search(
                    r"@Controller\s*\(\s*['\"]admin", scanned_file.text, re.IGNORECASE
                )
                assert match is not None
                findings.append(
                    _build_static_finding(
                        cwe_id="CWE-269",
                        location=scanned_file.location_for_index(match.start()),
                        source_type="security_configuration",
                        sink_type=SinkType.AUTH_SENSITIVE.value,
                        api_name="NestJS admin controller",
                        parameter_name="admin",
                        confidence=0.5,
                        metadata={"framework": "nestjs"},
                    )
                )
        elif suffix == ".py":
            for function in _iter_python_functions(scanned_file):
                if not _DJANGO_ADMIN_VIEW.search(function.name):
                    continue
                if _DJANGO_PERMISSION_DECORATOR.search(function.decorators):
                    continue
                findings.append(
                    _build_static_finding(
                        cwe_id="CWE-269",
                        location=scanned_file.location_for_index(function.start_index),
                        source_type="security_configuration",
                        sink_type=SinkType.AUTH_SENSITIVE.value,
                        api_name=function.name,
                        parameter_name=function.name,
                        confidence=0.5,
                        metadata={"framework": "django"},
                    )
                )
        elif suffix == ".java":
            for method in _iter_java_methods(scanned_file):
                if not method.path or not _ADMIN_ROUTE_PATTERNS.search(method.path):
                    continue
                if _SPRING_AUTH_ANNOTATIONS.search(method.annotations):
                    continue
                findings.append(
                    _build_static_finding(
                        cwe_id="CWE-269",
                        location=scanned_file.location_for_index(method.start_index),
                        source_type="security_configuration",
                        sink_type=SinkType.AUTH_SENSITIVE.value,
                        api_name=f"{method.name}({method.path})",
                        parameter_name=method.path,
                        confidence=0.5,
                        metadata={"framework": "spring"},
                    )
                )
    return findings


def _detect_missing_auth(
    scanned_files: Sequence[_ScannedFile],
    context: _AuthAccessContext,
) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    for scanned_file in scanned_files:
        suffix = scanned_file.path.suffix
        if suffix in {".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs"}:
            for route in _iter_express_routes(scanned_file):
                if not _route_is_critical(route):
                    continue
                if route.path and _path_is_public_by_design(route.path):
                    continue
                if _route_has_auth(route.middleware):
                    continue
                confidence = (
                    0.6
                    if _is_financial_route(route.path, route.handler_text)
                    or route.method
                    in {
                        "post",
                        "put",
                        "delete",
                        "patch",
                    }
                    else 0.45
                )
                if context.has_global_js_auth:
                    confidence = 0.3
                findings.append(
                    _build_static_finding(
                        cwe_id="CWE-306",
                        location=scanned_file.location_for_index(route.start_index),
                        source_type="security_configuration",
                        sink_type=SinkType.AUTH_SENSITIVE.value,
                        api_name=f"{route.method.upper()} {route.path or '<unknown>'}",
                        parameter_name=route.path,
                        confidence=confidence,
                        metadata={"framework": "express"},
                    )
                )
            if (
                "@Controller" in scanned_file.text
                and re.search(
                    r"@(?:Post|Put|Delete|Patch)\s*\(\s*['\"](?:payment|charge|transfer|delete)",
                    scanned_file.text,
                    re.IGNORECASE,
                )
                and not _NEST_GUARD_HINT.search(scanned_file.text)
            ):
                match = re.search(
                    r"@(?:Post|Put|Delete|Patch)\s*\(\s*['\"](?:payment|charge|transfer|delete)",
                    scanned_file.text,
                    re.IGNORECASE,
                )
                assert match is not None
                findings.append(
                    _build_static_finding(
                        cwe_id="CWE-306",
                        location=scanned_file.location_for_index(match.start()),
                        source_type="security_configuration",
                        sink_type=SinkType.AUTH_SENSITIVE.value,
                        api_name="NestJS critical handler",
                        parameter_name="critical route",
                        confidence=0.5,
                        metadata={"framework": "nestjs"},
                    )
                )
        elif suffix == ".py":
            for function in _iter_python_functions(scanned_file):
                route_path, _ = _flask_route_info(function.decorators)
                full_text = function.full_text
                if route_path is not None:
                    if _path_is_public_by_design(route_path):
                        continue
                    if _is_critical_path(route_path) or _CRITICAL_HANDLER_KEYWORDS.search(
                        full_text
                    ):
                        if _DJANGO_PERMISSION_DECORATOR.search(function.decorators):
                            continue
                        findings.append(
                            _build_static_finding(
                                cwe_id="CWE-306",
                                location=scanned_file.location_for_index(function.start_index),
                                source_type="security_configuration",
                                sink_type=SinkType.AUTH_SENSITIVE.value,
                                api_name=function.name,
                                parameter_name=route_path,
                                confidence=0.5,
                                metadata={"framework": "flask"},
                            )
                        )
                    continue
                if _function_is_critical(
                    function.name, function.full_text
                ) and not _DJANGO_PERMISSION_DECORATOR.search(function.decorators):
                    findings.append(
                        _build_static_finding(
                            cwe_id="CWE-306",
                            location=scanned_file.location_for_index(function.start_index),
                            source_type="security_configuration",
                            sink_type=SinkType.AUTH_SENSITIVE.value,
                            api_name=function.name,
                            parameter_name=function.name,
                            confidence=0.45,
                            metadata={"framework": "django"},
                        )
                    )
        elif suffix == ".java":
            for method in _iter_java_methods(scanned_file):
                if method.path and _path_is_public_by_design(method.path):
                    continue
                if not _java_method_is_critical(method):
                    continue
                if _SPRING_AUTH_ANNOTATIONS.search(method.annotations):
                    continue
                findings.append(
                    _build_static_finding(
                        cwe_id="CWE-306",
                        location=scanned_file.location_for_index(method.start_index),
                        source_type="security_configuration",
                        sink_type=SinkType.AUTH_SENSITIVE.value,
                        api_name=f"{method.name}({method.path or ''})",
                        parameter_name=method.path,
                        confidence=0.6
                        if _is_financial_route(method.path, method.full_text)
                        else 0.45,
                        metadata={"framework": "spring"},
                    )
                )
    return findings


def _load_scanned_files(project_root: Path, *, files: Sequence[Path] | None) -> list[_ScannedFile]:
    candidate_paths = (
        [Path(path) for path in files]
        if files is not None
        else sorted(path for path in project_root.rglob("*") if path.is_file())
    )
    scanned_files: list[_ScannedFile] = []
    for path in candidate_paths:
        if path.suffix not in _SOURCE_FILE_EXTENSIONS:
            continue
        scanned_file = _ScannedFile.load(path)
        if scanned_file is not None:
            scanned_files.append(scanned_file)
    return scanned_files


def _iter_express_routes(scanned_file: _ScannedFile) -> list[_JsRoute]:
    routes: list[_JsRoute] = []
    for match in _EXPRESS_ROUTE_CALL.finditer(scanned_file.text):
        call_text, call_end = _extract_enclosed(scanned_file.text, match.end() - 1, "(", ")")
        del call_end
        args = _split_top_level(call_text[1:-1])
        if len(args) < 2:
            continue
        path = _unquote(args[0])
        middleware = tuple(argument.strip() for argument in args[1:-1])
        handler_text = _resolve_js_handler(scanned_file, args[-1])
        routes.append(
            _JsRoute(
                method=match.group("method").lower(),
                path=path,
                call_text=scanned_file.text[match.start() : match.end() - 1] + call_text,
                middleware=middleware,
                handler_text=handler_text,
                start_index=match.start(),
            )
        )
    return routes


def _iter_python_functions(scanned_file: _ScannedFile) -> list[_PythonFunction]:
    functions: list[_PythonFunction] = []
    for match in _PYTHON_FUNCTION.finditer(scanned_file.text):
        functions.append(
            _PythonFunction(
                name=match.group("name"),
                decorators=match.group("decorators"),
                body=match.group("body"),
                full_text=match.group(0),
                start_index=match.start(),
            )
        )
    return functions


def _iter_java_methods(scanned_file: _ScannedFile) -> list[_JavaMethod]:
    methods: list[_JavaMethod] = []
    for match in _JAVA_ANNOTATED_METHOD.finditer(scanned_file.text):
        body_text, _ = _extract_enclosed(scanned_file.text, match.end() - 1, "{", "}")
        annotations = match.group("annotations")
        path_match = _JAVA_MAPPING_PATH.search(annotations)
        methods.append(
            _JavaMethod(
                name=match.group("name"),
                annotations=annotations,
                signature=match.group("signature"),
                body=body_text,
                full_text=annotations + match.group("signature") + body_text,
                start_index=match.start(),
                path=path_match.group("path") if path_match is not None else None,
            )
        )
    return methods


def _resolve_js_handler(scanned_file: _ScannedFile, handler_argument: str) -> str:
    stripped = handler_argument.strip()
    if "=>" in stripped or re.search(r"\bfunction\b", stripped):
        return stripped
    match = _JS_HANDLER_NAME.search(stripped)
    if match is None:
        return stripped
    name = match.group("name")
    patterns = (
        re.compile(_JS_FUNCTION_DECLARATION.format(name=re.escape(name))),
        re.compile(_JS_ARROW_DECLARATION.format(name=re.escape(name))),
        re.compile(_JS_FUNCTION_EXPRESSION.format(name=re.escape(name))),
    )
    for pattern in patterns:
        definition = pattern.search(scanned_file.text)
        if definition is None:
            continue
        brace_index = scanned_file.text.find("{", definition.start())
        if brace_index < 0:
            continue
        body_text, _ = _extract_enclosed(scanned_file.text, brace_index, "{", "}")
        return scanned_file.text[definition.start() : brace_index] + body_text
    return stripped


def _extract_enclosed(
    text: str, start_index: int, open_char: str, close_char: str
) -> tuple[str, int]:
    depth = 0
    quote: str | None = None
    escape = False
    for index in range(start_index, len(text)):
        char = text[index]
        if quote is not None:
            if escape:
                escape = False
            elif char == "\\":
                escape = True
            elif char == quote:
                quote = None
            continue
        if char in {'"', "'", "`"}:
            quote = char
            continue
        if char == open_char:
            depth += 1
        elif char == close_char:
            depth -= 1
            if depth == 0:
                return text[start_index : index + 1], index
    return text[start_index:], len(text) - 1


def _split_top_level(text: str) -> list[str]:
    parts: list[str] = []
    buffer: list[str] = []
    stack: list[str] = []
    quote: str | None = None
    escape = False
    pairs = {"(": ")", "[": "]", "{": "}"}
    closers = set(pairs.values())
    for char in text:
        if quote is not None:
            buffer.append(char)
            if escape:
                escape = False
            elif char == "\\":
                escape = True
            elif char == quote:
                quote = None
            continue
        if char in {'"', "'", "`"}:
            quote = char
            buffer.append(char)
            continue
        if char in pairs:
            stack.append(pairs[char])
            buffer.append(char)
            continue
        if char in closers:
            if stack and stack[-1] == char:
                stack.pop()
            buffer.append(char)
            continue
        if char == "," and not stack:
            part = "".join(buffer).strip()
            if part:
                parts.append(part)
            buffer = []
            continue
        buffer.append(char)
    part = "".join(buffer).strip()
    if part:
        parts.append(part)
    return parts


def _flask_route_info(decorators: str) -> tuple[str | None, set[str]]:
    for match in _FLASK_ROUTE_DECORATOR.finditer(decorators):
        path = match.group("path")
        args = match.group("args")
        methods_match = _FLASK_METHODS.search(args)
        if methods_match is None:
            return path, {"GET"}
        methods = {
            method.strip().strip("'\"").upper()
            for method in methods_match.group("methods").split(",")
            if method.strip()
        }
        return path, methods
    return None, set()


def _brace_scoped_text(scanned_file: _ScannedFile, index: int) -> str:
    block_start, block_end = scanned_file.containing_block(index)
    return scanned_file.text[block_start:block_end]


def _has_strong_ownership_check(text: str) -> bool:
    lowered = text.lower()
    if (
        "user_id" in lowered or "userid" in lowered or "owner_id" in lowered or "ownerid" in lowered
    ) and (
        "req.user" in lowered
        or "request.user" in lowered
        or "session.user" in lowered
        or "principal" in lowered
    ):
        return True
    return bool(
        _DJANGO_OWNERSHIP.search(text)
        or re.search(r"@PreAuthorize\s*\([^)]*owner", text, re.IGNORECASE)
    )


def _has_user_context(text: str) -> bool:
    lowered = text.lower()
    return any(
        token in lowered
        for token in (
            "req.user",
            "request.user",
            "session.user",
            "req.session",
            "currentuser",
            "principal",
        )
    )


def _route_has_auth(middleware: Sequence[str]) -> bool:
    return any(_AUTH_MIDDLEWARE_HINT.search(argument) for argument in middleware)


def _is_financial_route(path: str | None, body: str) -> bool:
    haystack = f"{path or ''}\n{body}".lower()
    return any(keyword in haystack for keyword in _FINANCIAL_KEYWORDS)


def _path_is_public_by_design(path: str) -> bool:
    normalized = path.rstrip("/").lower() or "/"
    return normalized in _PUBLIC_BY_DESIGN or normalized.startswith("/public/")


def _is_critical_path(path: str) -> bool:
    lowered = path.lower()
    return any(keyword in lowered for keyword in _CRITICAL_ROUTE_KEYWORDS)


def _function_is_critical(name: str, body: str) -> bool:
    lowered_name = name.lower()
    if any(keyword.replace("-", "") in lowered_name for keyword in _CRITICAL_ROUTE_KEYWORDS):
        return True
    return bool(
        _CRITICAL_HANDLER_KEYWORDS.search(body)
        or re.search(r"\.(?:delete|destroy|remove)\s*\(", body)
    )


def _route_is_critical(route: _JsRoute) -> bool:
    if route.path and _is_critical_path(route.path):
        return True
    if (
        route.method == "delete"
        and route.path
        and any(
            token in route.path.lower() for token in ("/user", "/account", "/profile", "/admin")
        )
    ):
        return True
    return bool(
        _CRITICAL_HANDLER_KEYWORDS.search(route.handler_text)
        or re.search(r"\.(?:destroy|delete|remove|update)\s*\(", route.handler_text, re.IGNORECASE)
    )


def _java_method_is_critical(method: _JavaMethod) -> bool:
    if method.path and _is_critical_path(method.path):
        return True
    if method.path and method.path.lower() in _PUBLIC_BY_DESIGN:
        return False
    return _function_is_critical(method.name, method.full_text)


def _mass_assignment_confidence(file_text: str, block_text: str) -> float:
    lowered = f"{file_text}\n{block_text}".lower()
    if any(
        field.lower() in lowered for field in _SENSITIVE_FIELDS
    ) and not _MASS_ASSIGN_SAFE_HINT.search(block_text):
        return 0.65
    return 0.5


def _unquote(value: str) -> str | None:
    stripped = value.strip()
    if len(stripped) >= 2 and stripped[0] == stripped[-1] and stripped[0] in {'"', "'", "`"}:
        return stripped[1:-1]
    return None


def _build_static_finding(
    *,
    cwe_id: str,
    location: SourceLocation,
    source_type: str,
    sink_type: str,
    api_name: str,
    parameter_name: str | None,
    confidence: float,
    severity: str | None = None,
    metadata: dict[str, object] | None = None,
) -> CandidateFinding:
    resolved_metadata = metadata or {}
    return CandidateFinding(
        id=_static_finding_id(
            cwe_id=cwe_id,
            file=location.file,
            line=location.line,
            column=location.column,
            api_name=api_name,
            parameter_name=parameter_name,
        ),
        vuln_class=cwe_id,
        source=TaintSource(
            location=location,
            source_type=source_type,
            data_categories=list(_DEFAULT_DATA_CATEGORIES),
            parameter_name=parameter_name,
        ),
        sink=TaintSink(
            location=location,
            sink_type=sink_type,
            api_name=api_name,
        ),
        taint_path=[],
        path_conditions=[],
        confidence=confidence,
        severity=severity or severity_for_cwe(cwe_id),
        metadata=resolved_metadata,
    )


def _static_finding_id(
    *,
    cwe_id: str,
    file: str,
    line: int,
    column: int,
    api_name: str,
    parameter_name: str | None,
) -> str:
    material = "|".join((cwe_id, file, str(line), str(column), api_name, parameter_name or ""))
    return hashlib.sha256(material.encode("utf-8")).hexdigest()


def _dedupe_findings(findings: Sequence[CandidateFinding]) -> list[CandidateFinding]:
    deduped: list[CandidateFinding] = []
    seen_ids: set[str] = set()
    for finding in findings:
        if finding.id in seen_ids:
            continue
        deduped.append(finding)
        seen_ids.add(finding.id)
    return deduped


def _line_starts(text: str) -> tuple[int, ...]:
    starts = [0]
    for index, char in enumerate(text):
        if char == "\n":
            starts.append(index + 1)
    return tuple(starts)


def _line_and_column(line_starts: Sequence[int], index: int) -> tuple[int, int]:
    line_index = bisect_right(line_starts, index) - 1
    line_start = line_starts[max(line_index, 0)]
    return max(line_index + 1, 1), index - line_start + 1


def _line_text(text: str, line_number: int) -> str:
    lines = text.splitlines()
    if 1 <= line_number <= len(lines):
        return lines[line_number - 1]
    return ""


def _brace_pairs(text: str) -> dict[int, int]:
    stack: list[int] = []
    pairs: dict[int, int] = {}
    quote: str | None = None
    escape = False
    for index, char in enumerate(text):
        if quote is not None:
            if escape:
                escape = False
            elif char == "\\":
                escape = True
            elif char == quote:
                quote = None
            continue
        if char in {'"', "'", "`"}:
            quote = char
            continue
        if char == "{":
            stack.append(index)
        elif char == "}" and stack:
            pairs[stack.pop()] = index
    return pairs


__all__ = ["AuthAccessConfig", "extract_auth_access_findings"]
