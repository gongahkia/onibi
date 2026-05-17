from __future__ import annotations

from collections.abc import Generator
from contextlib import contextmanager
from pathlib import Path
from types import SimpleNamespace

import pytest

import piranesi.pipeline as pipeline_module
from piranesi.config import OutputConfig, PiranesiConfig, ScanConfig
from piranesi.detect.crypto_transport import extract_crypto_transport_findings
from piranesi.pipeline import DetectArtifact, PipelineContext


@pytest.mark.parametrize(
    ("relative_path", "source", "expected_cwe"),
    [
        (
            "src/md5_password.ts",
            (
                'import crypto from "crypto";\n'
                "export function hashPassword(password: string): string {\n"
                '  return crypto.createHash("md5").update(password).digest("hex");\n'
                "}\n"
            ),
            "CWE-328",
        ),
        (
            "src/md5_token.py",
            (
                "import hashlib\n\n"
                "def issue_token(user: str) -> str:\n"
                "    token = hashlib.md5(user.encode()).hexdigest()\n"
                "    return token\n"
            ),
            "CWE-328",
        ),
        (
            "src/md5_token.go",
            (
                "package main\n\n"
                'import "crypto/md5"\n\n'
                "func authToken(v string) [16]byte {\n"
                "    return md5.Sum([]byte(v))\n"
                "}\n"
            ),
            "CWE-328",
        ),
        (
            "src/md5_password.java",
            (
                "import java.security.MessageDigest;\n\n"
                "class AuthService {\n"
                "  byte[] hashPassword(String password) throws Exception {\n"
                '    return MessageDigest.getInstance("MD5").digest(password.getBytes());\n'
                "  }\n"
                "}\n"
            ),
            "CWE-328",
        ),
        (
            "src/des_ecb.ts",
            (
                'import crypto from "crypto";\n'
                'const cipher = crypto.createCipheriv("des-ecb", key, Buffer.from("12345678"));\n'
            ),
            "CWE-327",
        ),
        (
            "src/des_cipher.py",
            (
                "from Crypto.Cipher import DES\n\n"
                "def encrypt(secret: bytes) -> object:\n"
                "    return DES.new(secret, DES.MODE_ECB)\n"
            ),
            "CWE-327",
        ),
        (
            "src/rc4_cipher.go",
            (
                "package main\n\n"
                'import "crypto/rc4"\n\n'
                "func encrypt(secret []byte) *rc4.Cipher {\n"
                "    c, _ := rc4.NewCipher(secret)\n"
                "    return c\n"
                "}\n"
            ),
            "CWE-327",
        ),
        (
            "src/java_aes_default.java",
            (
                "import javax.crypto.Cipher;\n\n"
                "class CryptoConfig {\n"
                "  Cipher insecure() throws Exception {\n"
                '    return Cipher.getInstance("AES");\n'
                "  }\n"
                "}\n"
            ),
            "CWE-327",
        ),
        (
            "src/js_rsa_1024.ts",
            (
                'import crypto from "crypto";\n'
                "crypto.generateKeyPairSync('rsa', { modulusLength: 1024 });\n"
            ),
            "CWE-326",
        ),
        (
            "src/js_ec_weak.ts",
            (
                'import crypto from "crypto";\n'
                "crypto.generateKeyPairSync('ec', { namedCurve: 'prime192v1' });\n"
            ),
            "CWE-326",
        ),
        (
            "src/py_rsa_1024.py",
            (
                "from cryptography.hazmat.primitives.asymmetric import rsa\n\n"
                "rsa.generate_private_key(public_exponent=65537, key_size=1024)\n"
            ),
            "CWE-326",
        ),
        (
            "src/go_rsa_1024.go",
            (
                "package main\n\n"
                'import "crypto/rsa"\n'
                'import "crypto/rand"\n\n'
                "func weak() {\n"
                "    _, _ = rsa.GenerateKey(rand.Reader, 1024)\n"
                "}\n"
            ),
            "CWE-326",
        ),
        (
            "src/java_rsa_1024.java",
            (
                "import java.security.KeyPairGenerator;\n\n"
                "class KeyConfig {\n"
                "  void weak() throws Exception {\n"
                '    KeyPairGenerator kpg = KeyPairGenerator.getInstance("RSA");\n'
                "    kpg.initialize(1024);\n"
                "  }\n"
                "}\n"
            ),
            "CWE-326",
        ),
        (
            "src/java_aes_64.java",
            (
                "import javax.crypto.KeyGenerator;\n\n"
                "class AesConfig {\n"
                "  void weak() throws Exception {\n"
                '    KeyGenerator keyGen = KeyGenerator.getInstance("AES");\n'
                "    keyGen.init(64);\n"
                "  }\n"
                "}\n"
            ),
            "CWE-326",
        ),
        (
            "src/cleartext_fetch.ts",
            'await fetch("http://api.example.com/users");\n',
            "CWE-319",
        ),
        (
            "src/cleartext_requests.py",
            'import requests\nrequests.get("http://api.example.com/users")\n',
            "CWE-319",
        ),
        (
            "src/cleartext_http.go",
            (
                "package main\n\n"
                'import "net/http"\n\n'
                "func fetch() {\n"
                '    _, _ = http.Get("http://api.example.com/users")\n'
                "}\n"
            ),
            "CWE-319",
        ),
        (
            "src/cleartext_url.java",
            'class Urls { java.net.URL u = new java.net.URL("http://api.example.com/users"); }\n',
            "CWE-319",
        ),
        (
            "src/weak_tls_node.ts",
            "tls.createServer({ minVersion: 'TLSv1.1' });\n",
            "CWE-319",
        ),
        (
            "src/weak_tls_python.py",
            "import ssl\ncontext = ssl.SSLContext(ssl.PROTOCOL_TLSv1)\n",
            "CWE-319",
        ),
        (
            "src/weak_tls_go.go",
            (
                "package main\n\n"
                'import "crypto/tls"\n\n'
                "var cfg = tls.Config{ MinVersion: tls.VersionTLS10 }\n"
            ),
            "CWE-319",
        ),
        (
            "src/weak_tls_java.java",
            'class TlsConfig { Object ctx = javax.net.ssl.SSLContext.getInstance("TLSv1"); }\n',
            "CWE-319",
        ),
        (
            "src/reject_unauthorized.ts",
            "const agent = new https.Agent({ rejectUnauthorized: false });\n",
            "CWE-295",
        ),
        (
            "src/tls_env.ts",
            'process.env.NODE_TLS_REJECT_UNAUTHORIZED = "0";\n',
            "CWE-295",
        ),
        (
            "src/verify_false.py",
            'import requests\nrequests.get("https://api.example.com", verify=False)\n',
            "CWE-295",
        ),
        (
            "src/unverified_context.py",
            "import ssl\nctx = ssl._create_unverified_context()\n",
            "CWE-295",
        ),
        (
            "src/insecure_skip_verify.go",
            (
                "package main\n\n"
                'import "crypto/tls"\n\n'
                "var cfg = tls.Config{ InsecureSkipVerify: true }\n"
            ),
            "CWE-295",
        ),
        (
            "src/hostname_verifier.java",
            (
                "import javax.net.ssl.HostnameVerifier;\n\n"
                "class V {\n"
                "  HostnameVerifier insecure = (host, session) -> { return true; };\n"
                "}\n"
            ),
            "CWE-295",
        ),
        (
            "src/math_random_token.ts",
            "const sessionToken = Math.random();\n",
            "CWE-338",
        ),
        (
            "src/math_random_flow.ts",
            "const seed = Math.random();\nconst resetToken = String(seed);\n",
            "CWE-338",
        ),
        (
            "src/python_random_password.py",
            "import random\npassword = random.randint(100000, 999999)\n",
            "CWE-338",
        ),
        (
            "src/go_random_token.go",
            (
                "package main\n\n"
                'import "math/rand"\n\n'
                "func issue() int {\n"
                "    sessionToken := rand.Intn(1000000)\n"
                "    return sessionToken\n"
                "}\n"
            ),
            "CWE-338",
        ),
        (
            "src/java_random_key.java",
            (
                "import java.util.Random;\n\n"
                "class Keys {\n"
                "  int insecure() {\n"
                "    int secretKey = new Random().nextInt();\n"
                "    return secretKey;\n"
                "  }\n"
                "}\n"
            ),
            "CWE-338",
        ),
        (
            "src/jwt_verify_no_alg.ts",
            ('import jwt from "jsonwebtoken";\njwt.verify(token, secret);\n'),
            "CWE-347",
        ),
        (
            "src/jwt_alg_none.ts",
            (
                'import jwt from "jsonwebtoken";\n'
                'jwt.verify(token, secret, { algorithms: ["none"] });\n'
            ),
            "CWE-347",
        ),
        (
            "src/jwt_public_key.ts",
            ('import jwt from "jsonwebtoken";\njwt.verify(token, publicKey);\n'),
            "CWE-347",
        ),
        (
            "src/jwt_hardcoded_secret.ts",
            (
                'import jwt from "jsonwebtoken";\n'
                'jwt.sign(payload, "GITHUB_TOKEN_REDACTED");\n'
            ),
            "CWE-347",
        ),
        (
            "src/pyjwt_decode.py",
            ("import jwt\n\nclaims = jwt.decode(token, secret)\n"),
            "CWE-347",
        ),
        (
            "src/go_jwt_parse.go",
            (
                "package main\n\n"
                'import "github.com/golang-jwt/jwt/v5"\n\n'
                "func parse(tokenString string) {\n"
                "    _, _ = jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {\n"
                '        return []byte("secret"), nil\n'
                "    })\n"
                "}\n"
            ),
            "CWE-347",
        ),
        (
            "src/java_alg_none.java",
            (
                "import com.auth0.jwt.algorithms.Algorithm;\n\n"
                "class Tokens {\n"
                "  Object alg() {\n"
                "    return Algorithm.none();\n"
                "  }\n"
                "}\n"
            ),
            "CWE-347",
        ),
        (
            "src/java_hmac_secret.java",
            (
                "import com.auth0.jwt.algorithms.Algorithm;\n\n"
                "class Tokens {\n"
                "  Object alg() {\n"
                '    return Algorithm.HMAC256("hardcoded-secret");\n'
                "  }\n"
                "}\n"
            ),
            "CWE-347",
        ),
    ],
)
def test_extract_crypto_transport_findings_detects_expected_cases(
    tmp_path: Path,
    relative_path: str,
    source: str,
    expected_cwe: str,
) -> None:
    findings = _extract(tmp_path, relative_path, source)

    assert any(finding.vuln_class == expected_cwe for finding in findings)


@pytest.mark.parametrize(
    ("relative_path", "source"),
    [
        (
            "src/sha1_checksum.ts",
            (
                'import crypto from "crypto";\n'
                "// checksum for file hash\n"
                'const checksum = crypto.createHash("sha1").update(fileContents).digest("hex");\n'
            ),
        ),
        (
            "src/aes_gcm_safe.ts",
            (
                'import crypto from "crypto";\n'
                'const cipher = crypto.createCipheriv("aes-256-gcm", key, iv);\n'
            ),
        ),
        (
            "src/js_rsa_2048.ts",
            (
                'import crypto from "crypto";\n'
                "crypto.generateKeyPairSync('rsa', { modulusLength: 2048 });\n"
            ),
        ),
        (
            "src/js_ec_safe.ts",
            (
                'import crypto from "crypto";\n'
                "crypto.generateKeyPairSync('ec', { namedCurve: 'prime256v1' });\n"
            ),
        ),
        (
            "src/java_aes_128.java",
            (
                "import javax.crypto.KeyGenerator;\n\n"
                "class AesConfig {\n"
                "  void safe() throws Exception {\n"
                '    KeyGenerator keyGen = KeyGenerator.getInstance("AES");\n'
                "    keyGen.init(128);\n"
                "  }\n"
                "}\n"
            ),
        ),
        (
            "src/fetch_localhost.ts",
            'await fetch("http://localhost:3000/health");\n',
        ),
        (
            "src/math_random_animation.ts",
            "const colorIndex = Math.floor(Math.random() * colors.length);\n",
        ),
        (
            "src/jwt_verify_with_alg.ts",
            (
                'import jwt from "jsonwebtoken";\n'
                'jwt.verify(token, secret, { algorithms: ["HS256"] });\n'
            ),
        ),
        (
            "src/pyjwt_decode_safe.py",
            ('import jwt\n\nclaims = jwt.decode(token, secret, algorithms=["HS256"])\n'),
        ),
        (
            "src/go_jwt_parse_safe.go",
            (
                "package main\n\n"
                'import "github.com/golang-jwt/jwt/v5"\n\n'
                "func parse(tokenString string) {\n"
                "    _, _ = jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {\n"
                "        if token.Method != jwt.SigningMethodHS256 {\n"
                "            return nil, jwt.ErrTokenSignatureInvalid\n"
                "        }\n"
                '        return []byte("secret"), nil\n'
                "    })\n"
                "}\n"
            ),
        ),
    ],
)
def test_extract_crypto_transport_findings_skips_safe_cases(
    tmp_path: Path,
    relative_path: str,
    source: str,
) -> None:
    findings = _extract(tmp_path, relative_path, source)

    assert findings == ()


def test_extract_crypto_transport_findings_marks_jwt_subtypes(tmp_path: Path) -> None:
    findings = _extract(
        tmp_path,
        "src/jwt_combo.ts",
        (
            'import jwt from "jsonwebtoken";\n'
            'jwt.verify(token, publicKey, { algorithms: ["none"] });\n'
            'jwt.sign(payload, "GITHUB_TOKEN_REDACTED");\n'
        ),
    )

    subtypes = {
        finding.metadata.get("sub_type") for finding in findings if finding.vuln_class == "CWE-347"
    }
    assert "alg_none" in subtypes
    assert "hardcoded_jwt_secret" in subtypes


def test_detect_stage_deduplicates_jwt_secret_findings(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    target_dir = tmp_path / "target"
    source_file = target_dir / "src" / "auth.ts"
    source_file.parent.mkdir(parents=True, exist_ok=True)
    source_file.write_text(
        (
            'import jwt from "jsonwebtoken";\n'
            'const token = jwt.sign(payload, "GITHUB_TOKEN_REDACTED");\n'
        ),
        encoding="utf-8",
    )

    context = PipelineContext(
        target_dir=target_dir,
        output_dir=tmp_path / "out",
        provider=None,  # type: ignore[arg-type]
        router=None,  # type: ignore[arg-type]
        cost_tracker=SimpleNamespace(total_usd=0.0),  # type: ignore[arg-type]
        trace_writer=None,  # type: ignore[arg-type]
    )
    config = PiranesiConfig(
        output=OutputConfig(output_dir=str(context.output_dir)),
        scan=ScanConfig(include_tests=False),
    )

    monkeypatch.setattr(pipeline_module, "resolve_frameworks", lambda *_args, **_kwargs: [])
    monkeypatch.setattr(pipeline_module, "get_source_specs", lambda *_args, **_kwargs: [])
    monkeypatch.setattr(pipeline_module, "get_sink_specs", lambda *_args, **_kwargs: [])
    monkeypatch.setattr(pipeline_module, "get_sanitizer_specs", lambda *_args, **_kwargs: [])
    monkeypatch.setattr(pipeline_module, "extract_candidate_findings", lambda *_args, **_kwargs: ())
    monkeypatch.setattr(
        pipeline_module,
        "extract_misconfiguration_findings",
        lambda *_args, **_kwargs: (),
    )
    monkeypatch.setattr(
        pipeline_module,
        "scan_dependency_findings",
        lambda *_args, **_kwargs: SimpleNamespace(findings=[]),
    )
    monkeypatch.setattr(
        pipeline_module,
        "_annotate_reachability_for_findings",
        lambda _context, _config, findings: (findings, None),
    )

    @contextmanager
    def _fake_scan_session(
        *_args: object,
        **_kwargs: object,
    ) -> Generator[tuple[None, SimpleNamespace], None, None]:
        yield None, SimpleNamespace(joern_project_root=target_dir, source_map=None)

    monkeypatch.setattr(pipeline_module, "_scan_session", _fake_scan_session)

    result = pipeline_module._run_detect_stage(context, config, None)

    assert isinstance(result.artifact, DetectArtifact)
    jwt_findings = [
        finding for finding in result.artifact.findings if finding.vuln_class == "CWE-347"
    ]
    secret_findings = [
        finding for finding in result.artifact.findings if finding.vuln_class == "CWE-798"
    ]
    assert len(jwt_findings) == 1
    assert secret_findings == []
    assert jwt_findings[0].metadata.get("suppressed_cwe_798") is True


def _extract(tmp_path: Path, relative_path: str, source: str):
    path = tmp_path / relative_path
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(source, encoding="utf-8")
    return extract_crypto_transport_findings(tmp_path, files=(path,), include_tests=True)
