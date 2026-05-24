from __future__ import annotations

import json
from pathlib import Path

from piranesi.detect.misconfigurations import extract_misconfiguration_findings


def test_extract_misconfiguration_findings_flags_wildcard_cors_with_credentials(
    tmp_path: Path,
) -> None:
    app_file = _write_express_app(
        tmp_path,
        (
            'import express from "express";\n'
            'import helmet from "helmet";\n'
            "const app = express();\n"
            "app.use(helmet());\n"
            'app.get("/cors", (req, res) => {\n'
            "  res.setHeader('Access-Control-Allow-Origin', '*');\n"
            "  res.setHeader('Access-Control-Allow-Credentials', 'true');\n"
            "  res.send('ok');\n"
            "});\n"
        ),
    )

    findings = extract_misconfiguration_findings(
        tmp_path,
        frameworks=("express",),
        files=(app_file,),
    )

    cors_finding = next(finding for finding in findings if finding.vuln_class == "CWE-942")
    assert cors_finding.sink.api_name == "res.setHeader"
    assert cors_finding.sink.location.line == 6


def test_extract_misconfiguration_findings_flags_missing_security_headers(
    tmp_path: Path,
) -> None:
    app_file = _write_express_app(
        tmp_path,
        (
            'import express from "express";\n'
            "const app = express();\n"
            'app.get("/page", (_req, res) => {\n'
            "  res.send('ok');\n"
            "});\n"
        ),
    )

    findings = extract_misconfiguration_findings(
        tmp_path,
        frameworks=("express",),
        files=(app_file,),
    )

    send_findings = {
        (finding.vuln_class, finding.source.parameter_name)
        for finding in findings
        if finding.sink.api_name == "res.send"
    }
    assert ("CWE-1021", "X-Frame-Options") in send_findings
    assert ("CWE-693", "Content-Security-Policy") in send_findings
    assert ("CWE-319", "Strict-Transport-Security") in send_findings


def test_extract_misconfiguration_findings_skips_headers_when_set_upstream(
    tmp_path: Path,
) -> None:
    app_file = _write_express_app(
        tmp_path,
        (
            'import express from "express";\n'
            "const app = express();\n"
            'app.get("/page", (_req, res) => {\n'
            "  res.setHeader('X-Frame-Options', 'DENY');\n"
            '  res.setHeader("Content-Security-Policy", "default-src \'self\'");\n'
            "  res.setHeader('Strict-Transport-Security', 'max-age=31536000');\n"
            "  res.send('ok');\n"
            "});\n"
        ),
    )

    findings = extract_misconfiguration_findings(
        tmp_path,
        frameworks=("express",),
        files=(app_file,),
    )

    send_cwes = {finding.vuln_class for finding in findings if finding.sink.api_name == "res.send"}
    assert "CWE-1021" not in send_cwes
    assert "CWE-693" not in send_cwes
    assert "CWE-319" not in send_cwes


def test_extract_misconfiguration_findings_flags_insecure_cookie_settings(
    tmp_path: Path,
) -> None:
    app_file = _write_express_app(
        tmp_path,
        (
            'import express from "express";\n'
            'import helmet from "helmet";\n'
            'import session from "express-session";\n'
            "const app = express();\n"
            "app.use(helmet());\n"
            "app.use(\n"
            "  session({\n"
            "    secret: 'dev',\n"
            "    cookie: {\n"
            "      secure: false,\n"
            "      httpOnly: false,\n"
            "    },\n"
            "  })\n"
            ");\n"
        ),
    )

    findings = extract_misconfiguration_findings(
        tmp_path,
        frameworks=("express",),
        files=(app_file,),
    )

    insecure_flags = {(finding.vuln_class, finding.sink.api_name) for finding in findings}
    assert ("CWE-614", "cookie.secure") in insecure_flags
    assert ("CWE-1004", "cookie.httpOnly") in insecure_flags


def test_extract_misconfiguration_findings_flags_missing_helmet(tmp_path: Path) -> None:
    app_file = _write_express_app(
        tmp_path,
        (
            'import express from "express";\n'
            "const app = express();\n"
            'app.get("/health", (_req, res) => {\n'
            "  res.send('ok');\n"
            "});\n"
        ),
    )

    findings = extract_misconfiguration_findings(
        tmp_path,
        frameworks=("express",),
        files=(app_file,),
    )

    helmet_finding = next(
        finding
        for finding in findings
        if finding.vuln_class == "CWE-693" and finding.sink.api_name == "helmet()"
    )
    assert helmet_finding.sink.location.file == str(app_file.resolve(strict=False))


def _write_express_app(tmp_path: Path, source: str) -> Path:
    (tmp_path / "package.json").write_text(
        json.dumps({"dependencies": {"express": "^4.21.0"}}),
        encoding="utf-8",
    )
    app_file = tmp_path / "app.ts"
    app_file.write_text(source, encoding="utf-8")
    return app_file
