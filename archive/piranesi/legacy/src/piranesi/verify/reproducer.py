from __future__ import annotations

import json
import shlex
from datetime import UTC, datetime
from pathlib import Path
from urllib.parse import urlsplit

from piranesi.models import CandidateFinding, ConfirmedFinding, TriagedFinding
from piranesi.verify.sandbox import PayloadEncoding, SynthesizedPayload

DEFAULT_INTERNAL_PORT = 3000


def generate_reproducer_script(
    finding: CandidateFinding | TriagedFinding | ConfirmedFinding,
    *,
    target_path: str | Path | None = None,
    payload: SynthesizedPayload | None = None,
    internal_port: int = DEFAULT_INTERNAL_PORT,
    generated_at: datetime | None = None,
) -> str:
    candidate = _candidate_finding(finding)
    resolved_payload = payload or _payload_from_confirmed_finding(finding)
    if resolved_payload is None:
        raise ValueError(
            "payload is required unless a ConfirmedFinding provides a captured request"
        )

    generated_timestamp = (generated_at or datetime.now(UTC)).isoformat().replace("+00:00", "Z")
    target_path_value = (
        str(Path(target_path).expanduser().resolve())
        if target_path is not None
        else "/path/to/target/app"
    )
    expected_value = _expected_confirmation_value(candidate, resolved_payload)
    request_url = _request_path(resolved_payload)
    summary_taint_path = _taint_path_summary(candidate)
    curl_lines = _curl_command_lines(resolved_payload)
    confirmation_lines = _confirmation_lines(candidate.vuln_class, expected_value)

    lines = [
        "#!/usr/bin/env bash",
        "# =============================================================================",
        "# PIRANESI EXPLOIT REPRODUCER",
        "# =============================================================================",
        "# WARNING: This script demonstrates a security vulnerability.",
        "# Only run against systems you own and have authorization to test.",
        "# =============================================================================",
        "#",
        f"# Vulnerability: {candidate.vuln_class}",
        f"# File:          {candidate.sink.location.file}:{candidate.sink.location.line}",
        f"# Taint Path:    {summary_taint_path}",
        f"# Severity:      {candidate.severity.upper()}",
        f"# Generated:     {generated_timestamp}",
        "# =============================================================================",
        "",
        "set -euo pipefail",
        "",
        f"TARGET_APP_PATH=${{TARGET_APP_PATH:-{shlex.quote(target_path_value)}}}",
        f"INTERNAL_PORT=${{INTERNAL_PORT:-{internal_port}}}",
        'IMAGE_NAME="${IMAGE_NAME:-piranesi-repro-$(date +%s)}"',
        'CONTAINER_NAME="${CONTAINER_NAME:-piranesi-repro-$$}"',
        'NETWORK_NAME="${NETWORK_NAME:-piranesi-repro-net-$$}"',
        'HOST_PORT=""',
        "PASS=0",
        "FAIL=0",
        "",
        "cleanup() {",
        '  echo "[*] Cleaning up..."',
        '  docker stop "$CONTAINER_NAME" >/dev/null 2>&1 || true',
        '  docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true',
        '  docker network rm "$NETWORK_NAME" >/dev/null 2>&1 || true',
        '  echo "[*] Done."',
        "}",
        "trap cleanup EXIT",
        "",
        'echo "[*] Building target application..."',
        'docker build -t "$IMAGE_NAME" "$TARGET_APP_PATH"',
        "",
        'echo "[*] Creating isolated network..."',
        'docker network create --internal "$NETWORK_NAME" >/dev/null',
        "",
        'echo "[*] Starting container..."',
        "docker run -d \\",
        '  --name "$CONTAINER_NAME" \\',
        '  --network "$NETWORK_NAME" \\',
        "  --read-only \\",
        "  --tmpfs /tmp:size=64m \\",
        "  --cap-drop ALL \\",
        "  --security-opt no-new-privileges \\",
        "  --memory 512m \\",
        "  --cpus 1 \\",
        "  --pids-limit 256 \\",
        "  --user node \\",
        '  -p 0:"${INTERNAL_PORT}" \\',
        '  "$IMAGE_NAME" >/dev/null',
        "",
        'HOST_PORT=$(docker port "$CONTAINER_NAME" '
        "\"${INTERNAL_PORT}/tcp\" | tail -n 1 | awk -F: '{print $NF}')",
        'if [ -z "$HOST_PORT" ]; then',
        '  echo "[FAIL] Unable to determine mapped host port."',
        "  exit 1",
        "fi",
        "",
        'echo "[*] Waiting for application to be ready..."',
        "for i in $(seq 1 30); do",
        '  if curl -sf "http://127.0.0.1:${HOST_PORT}/" >/dev/null 2>&1; then',
        '    echo "[*] Application is ready."',
        "    break",
        "  fi",
        '  if [ "$i" -eq 30 ]; then',
        '    echo "[FAIL] Application did not become ready in time."',
        "    exit 1",
        "  fi",
        "  sleep 1",
        "done",
        "",
        'echo "[*] Firing exploit payload..."',
        'HTTP_BODY_FILE="$(mktemp)"',
        *curl_lines,
        'HTTP_BODY="$(cat "$HTTP_BODY_FILE")"',
        'rm -f "$HTTP_BODY_FILE"',
        "",
        'echo "[*] Request path: ' + request_url + '"',
        'echo "[*] Response code: $HTTP_CODE"',
        'echo "[*] Response body: $HTTP_BODY"',
        "",
        'echo "[*] Checking confirmation indicators..."',
        *confirmation_lines,
        "",
        'echo ""',
        'echo "============================================"',
        'echo "Results: $PASS passed, $FAIL failed"',
        'echo "============================================"',
        "",
        'if [ "$FAIL" -gt 0 ]; then',
        "  exit 1",
        "fi",
    ]
    return "\n".join(lines) + "\n"


def write_reproducer_script(path: str | Path, script: str) -> Path:
    output_path = Path(path)
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(script, encoding="utf-8")
    output_path.chmod(0o755)
    return output_path


def _candidate_finding(
    finding: CandidateFinding | TriagedFinding | ConfirmedFinding,
) -> CandidateFinding:
    if isinstance(finding, ConfirmedFinding):
        return finding.finding.finding
    if isinstance(finding, TriagedFinding):
        return finding.finding
    return finding


def _payload_from_confirmed_finding(
    finding: CandidateFinding | TriagedFinding | ConfirmedFinding,
) -> SynthesizedPayload | None:
    if not isinstance(finding, ConfirmedFinding):
        return None

    request = finding.sandbox_result.request
    method = request.get("method")
    url = request.get("url")
    if not isinstance(method, str) or not isinstance(url, str):
        return None

    parsed = urlsplit(url)
    payload_encoding = request.get("encoding", "json")
    encoding = _normalize_encoding(payload_encoding)
    body = request.get("body")
    headers = request.get("headers", {})
    payload_values = request.get("payload_values", {})
    return SynthesizedPayload(
        method=method,
        url=parsed.path or "/",
        headers=dict(headers) if isinstance(headers, dict) else {},
        body=body,
        payload_values=dict(payload_values) if isinstance(payload_values, dict) else {},
        encoding=encoding,
    )


def _taint_path_summary(candidate: CandidateFinding) -> str:
    source = _source_expression(candidate)
    return f"{source} -> {candidate.sink.api_name} at line {candidate.sink.location.line}"


def _source_expression(candidate: CandidateFinding) -> str:
    source_type = candidate.source.source_type
    if source_type.startswith("req.") or source_type.startswith("process.env"):
        return source_type

    prefix = {
        "request_body": "req.body",
        "request_param": "req.query",
        "header": "req.headers",
        "cookie": "req.cookies",
    }.get(source_type)
    if prefix is None:
        return source_type
    if candidate.source.parameter_name:
        return f"{prefix}.{candidate.source.parameter_name}"
    return prefix


def _request_path(payload: SynthesizedPayload) -> str:
    split = urlsplit(payload.url)
    if split.query:
        return f"{split.path}?{split.query}"
    return split.path or "/"


def _curl_command_lines(payload: SynthesizedPayload) -> list[str]:
    request_url = f"http://127.0.0.1:${{HOST_PORT}}{_request_path(payload)}"
    lines = [
        "HTTP_CODE=$(curl -sS -o \"$HTTP_BODY_FILE\" -w '%{http_code}' \\",
        f"  -X {shlex.quote(payload.method.upper())} \\",
    ]
    for name, value in payload.headers.items():
        lines.append(f"  -H {shlex.quote(f'{name}: {value}')} \\")

    if payload.encoding == "json" and payload.body is not None:
        lines.append(f"  --data {shlex.quote(json.dumps(payload.body, separators=(',', ':')))} \\")
    elif payload.encoding == "urlencoded" and isinstance(payload.body, dict):
        for key, value in payload.body.items():
            lines.append(f"  --data-urlencode {shlex.quote(f'{key}={value}')} \\")
    elif payload.encoding == "query" and isinstance(payload.body, dict):
        lines.append("  --get \\")
        for key, value in payload.body.items():
            lines.append(f"  --data-urlencode {shlex.quote(f'{key}={value}')} \\")
    lines.append(f"  {shlex.quote(request_url)})")
    return lines


def _expected_confirmation_value(candidate: CandidateFinding, payload: SynthesizedPayload) -> str:
    for value in payload.payload_values.values():
        if value:
            return value
    if payload.encoding == "json" and isinstance(payload.body, dict):
        return json.dumps(payload.body, separators=(",", ":"))
    return candidate.sink.api_name


def _confirmation_lines(vuln_class: str, expected_value: str) -> list[str]:
    normalized = vuln_class.upper()
    if "CWE-89" in normalized or "SQL" in normalized:
        return [
            "if printf '%s' \"$HTTP_BODY\" | grep -qiE "
            "'(syntax error|mysql_fetch|pg_query|ORA-|sqlite|multiple rows)'; then",
            '  echo "[CONFIRMED] SQL injection indicators detected in the response."',
            "  PASS=$((PASS + 1))",
            'elif [ "$HTTP_CODE" = "200" ]; then',
            '  echo "[LIKELY] The injected payload changed application behavior."',
            "  PASS=$((PASS + 1))",
            "else",
            '  echo "[FAIL] Exploit did not produce a SQL injection signal."',
            "  FAIL=$((FAIL + 1))",
            "fi",
        ]
    if "CWE-79" in normalized or "XSS" in normalized:
        return [
            f"if printf '%s' \"$HTTP_BODY\" | grep -Fq -- {shlex.quote(expected_value)}; then",
            '  echo "[CONFIRMED] Reflected XSS payload was returned unescaped."',
            "  PASS=$((PASS + 1))",
            "else",
            '  echo "[FAIL] Response did not reflect the payload."',
            "  FAIL=$((FAIL + 1))",
            "fi",
        ]
    if "CWE-78" in normalized or "CMD" in normalized or "COMMAND" in normalized:
        return [
            "if printf '%s' \"$HTTP_BODY\" | grep -qiE '(uid=|gid=|/root:|/home/|Linux)'; then",
            '  echo "[CONFIRMED] Command execution output was observed."',
            "  PASS=$((PASS + 1))",
            "else",
            '  echo "[FAIL] Command injection indicators were not observed."',
            "  FAIL=$((FAIL + 1))",
            "fi",
        ]
    if "CWE-22" in normalized or "TRAVERS" in normalized:
        return [
            "if printf '%s' \"$HTTP_BODY\" | grep -qiE "
            "'(root:.*:0:0|daemon:.*:1:1|/bin/bash)'; then",
            '  echo "[CONFIRMED] Traversed file contents were returned."',
            "  PASS=$((PASS + 1))",
            "else",
            '  echo "[FAIL] Path traversal indicators were not observed."',
            "  FAIL=$((FAIL + 1))",
            "fi",
        ]
    return [
        'if [ "$HTTP_CODE" -lt 500 ]; then',
        '  echo "[LIKELY] Request completed without a definitive exploit-specific signal."',
        "  PASS=$((PASS + 1))",
        "else",
        '  echo "[FAIL] Request failed without confirmation evidence."',
        "  FAIL=$((FAIL + 1))",
        "fi",
    ]


def _normalize_encoding(value: object) -> PayloadEncoding:
    if isinstance(value, str) and value in {"json", "urlencoded", "query", "path"}:
        return value  # type: ignore[return-value]
    return "json"


__all__ = [
    "DEFAULT_INTERNAL_PORT",
    "generate_reproducer_script",
    "write_reproducer_script",
]
