from __future__ import annotations

import hashlib
import re
from bisect import bisect_right
from collections.abc import Callable, Iterable, Sequence
from dataclasses import dataclass
from pathlib import Path

from piranesi.detect.flows import severity_for_cwe
from piranesi.models import CandidateFinding, SourceLocation, TaintSink, TaintSource
from piranesi.scan.specs import (
    CRYPTO_TRANSPORT_JWT_PUBLIC_KEY_HINTS,
    CRYPTO_TRANSPORT_NON_SECURITY_HASH_HINTS,
    CRYPTO_TRANSPORT_NON_SECURITY_PRNG_HINTS,
    CRYPTO_TRANSPORT_SECURITY_CONTEXT_HINTS,
    CRYPTO_TRANSPORT_SECURITY_IDENTIFIER_HINTS,
    CRYPTO_TRANSPORT_WEAK_EC_CURVES,
)

_SOURCE_FILE_EXTENSIONS = frozenset(
    {".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".py", ".go", ".java"}
)
_DIRECTORY_EXCLUSIONS = frozenset(
    {"node_modules", "vendor", ".git", "__pycache__", ".venv", "venv"}
)
_TEST_FILENAME_PATTERNS = (
    re.compile(r".*\.test\.[^.]+$", re.IGNORECASE),
    re.compile(r".*\.spec\.[^.]+$", re.IGNORECASE),
    re.compile(r"^test_[^.]+", re.IGNORECASE),
)
_TEST_DIRECTORY_NAMES = frozenset({"tests", "__tests__", "test"})
_DEFAULT_DATA_CATEGORIES = ["unknown"]
_STATIC_SOURCE_TYPE = "cryptographic_configuration"
_STATIC_SINK_TYPE = "cryptographic_weakness"
_MIN_RSA_BITS = 2048
_MIN_EC_BITS = 256
_MIN_AES_BITS = 128

_NON_SECURITY_CONTEXT_INDICATORS = (
    re.compile(
        r"\b(?:"
        + "|".join(
            re.escape(hint).replace("_", ".?") for hint in CRYPTO_TRANSPORT_NON_SECURITY_HASH_HINTS
        )
        + r")\b",
        re.IGNORECASE,
    ),
    re.compile(r"^\s*(?:#|//).*(?:checksum|etag|cache|fingerprint|integrity)", re.IGNORECASE),
)
_SECURITY_CONTEXT_INDICATORS = (
    re.compile(
        r"\b(?:"
        + "|".join(
            re.escape(hint).replace("_", "[_-]?")
            for hint in CRYPTO_TRANSPORT_SECURITY_CONTEXT_HINTS
        )
        + r")\b",
        re.IGNORECASE,
    ),
)
_PRNG_SECURITY_SINKS = (
    re.compile(
        r"\b(?:token|session[_-]?id|csrf|nonce|otp|api[_-]?key|secret|salt|password|passwd|key|iv|initialization.?vector|reset[_-]?token|verification[_-]?code|auth[_-]?code|invite[_-]?code)\b",
        re.IGNORECASE,
    ),
    re.compile(
        r"\b(?:encrypt|decrypt|sign|hmac|jwt[.]sign|jwt[.]verify|createHmac)\b", re.IGNORECASE
    ),
)
_NON_SECURITY_PRNG_CONTEXT = (
    re.compile(
        r"\b(?:"
        + "|".join(
            re.escape(hint)
            for hint in CRYPTO_TRANSPORT_NON_SECURITY_PRNG_HINTS
            if hint not in {"test", "spec"}
        )
        + r")\b",
        re.IGNORECASE,
    ),
    re.compile(r"\b(?:test|spec)\b", re.IGNORECASE),
    re.compile(r"\.style\.", re.IGNORECASE),
)
_LOCALHOST_HTTP_PATTERN = re.compile(
    r"^http://(?:localhost|127[.]0[.]0[.]1|0[.]0[.]0[.]0|\[::1\]|::1)(?::\d+)?(?:[/?#]|$)",
    re.IGNORECASE,
)
_WEAK_EC_CURVES = frozenset(CRYPTO_TRANSPORT_WEAK_EC_CURVES)
_JWT_PUBLIC_KEY_NAMES = re.compile(
    r"\b(?:" + "|".join(re.escape(hint) for hint in CRYPTO_TRANSPORT_JWT_PUBLIC_KEY_HINTS) + r")\b"
)
_JWT_NONE_ALG_PATTERN = re.compile(
    r"\b(?:algorithms?|algorithm|alg)\s*[:=]\s*(?:\[[^\]]*['\"]none['\"]|['\"]none['\"])",
    re.IGNORECASE | re.DOTALL,
)


def _normalize_identifier_hint(value: str) -> str:
    expanded = re.sub(r"([a-z0-9])([A-Z])", r"\1_\2", value)
    return re.sub(r"[^A-Za-z0-9]+", "_", expanded).strip("_").lower()


_SECURITY_IDENTIFIER_TERMS = tuple(
    _normalize_identifier_hint(hint) for hint in CRYPTO_TRANSPORT_SECURITY_IDENTIFIER_HINTS
)


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
class _WeakHashPattern:
    pattern: re.Pattern[str]
    algorithm: str
    library: str
    api_name: str


@dataclass(frozen=True, slots=True)
class _WeakCipherPattern:
    pattern: re.Pattern[str]
    algorithm: str
    library: str
    api_name: str


@dataclass(frozen=True, slots=True)
class _KeyLengthPattern:
    pattern: re.Pattern[str]
    algorithm: str
    api_name: str
    extractor: Callable[[re.Match[str]], tuple[str, int | str, int]]


@dataclass(frozen=True, slots=True)
class _WeakPrngAssignmentPattern:
    pattern: re.Pattern[str]
    prng_name: str
    api_name: str


_WEAK_HASH_PATTERNS: tuple[_WeakHashPattern, ...] = (
    _WeakHashPattern(
        pattern=re.compile(
            r"""crypto\.createHash\s*\(\s*['"](?P<alg>md5|sha1|md4|ripemd160)['"]\s*\)"""
        ),
        algorithm="dynamic",
        library="crypto",
        api_name="crypto.createHash",
    ),
    _WeakHashPattern(
        pattern=re.compile(r"""CryptoJS\.(?P<alg>MD5|SHA1|MD4|RIPEMD160)\s*\("""),
        algorithm="dynamic",
        library="CryptoJS",
        api_name="CryptoJS.hash",
    ),
    _WeakHashPattern(
        pattern=re.compile(r"""subtle\.digest\s*\(\s*['"](?P<alg>MD5|SHA-1)['"]\s*,"""),
        algorithm="dynamic",
        library="subtle",
        api_name="subtle.digest",
    ),
    _WeakHashPattern(
        pattern=re.compile(
            r"""hashlib\.(?P<alg>md5|sha1)\s*\(|hashlib\.new\s*\(\s*['"](?P<alg_new>md5|sha1)['"]\s*[,)]""",
            re.IGNORECASE,
        ),
        algorithm="dynamic",
        library="hashlib",
        api_name="hashlib",
    ),
    _WeakHashPattern(
        pattern=re.compile(
            r"""(?:Crypto|Cryptodome)\.Hash\.(?P<alg>MD5|SHA|MD4|MD2|RIPEMD)\.new\s*\("""
        ),
        algorithm="dynamic",
        library="Crypto.Hash",
        api_name="Crypto.Hash.new",
    ),
    _WeakHashPattern(
        pattern=re.compile(r"""(?<![\w/])md5\.(?:New|Sum)\s*\("""),
        algorithm="MD5",
        library="crypto/md5",
        api_name="md5.New",
    ),
    _WeakHashPattern(
        pattern=re.compile(r"""(?<![\w/])sha1\.(?:New|Sum)\s*\("""),
        algorithm="SHA-1",
        library="crypto/sha1",
        api_name="sha1.New",
    ),
    _WeakHashPattern(
        pattern=re.compile(
            r"""MessageDigest\.getInstance\s*\(\s*["'](?P<alg>MD5|MD2|SHA-1|SHA1)["']\s*\)"""
        ),
        algorithm="dynamic",
        library="MessageDigest",
        api_name="MessageDigest.getInstance",
    ),
    _WeakHashPattern(
        pattern=re.compile(
            r"""DigestUtils\.(?P<alg>md5|sha1|md5Hex|sha1Hex)\s*\(""", re.IGNORECASE
        ),
        algorithm="dynamic",
        library="DigestUtils",
        api_name="DigestUtils",
    ),
)

_WEAK_CIPHER_PATTERNS: tuple[_WeakCipherPattern, ...] = (
    _WeakCipherPattern(
        pattern=re.compile(
            r"""crypto\.createCipher(?:iv)?\s*\(\s*['"](?P<alg>des|des3|des-ede3?|rc4|des-ecb|des-cbc|aes-\d+-ecb)['"]\s*,""",
            re.IGNORECASE,
        ),
        algorithm="dynamic",
        library="crypto",
        api_name="crypto.createCipher",
    ),
    _WeakCipherPattern(
        pattern=re.compile(
            r"""CryptoJS\.(?P<alg>DES|TripleDES|RC4|Rabbit)\.(?:encrypt|decrypt)\s*\("""
        ),
        algorithm="dynamic",
        library="CryptoJS",
        api_name="CryptoJS.cipher",
    ),
    _WeakCipherPattern(
        pattern=re.compile(r"""mode\s*:\s*CryptoJS\.mode\.ECB"""),
        algorithm="ECB",
        library="CryptoJS",
        api_name="CryptoJS.mode.ECB",
    ),
    _WeakCipherPattern(
        pattern=re.compile(r"""(?:Crypto|Cryptodome)\.Cipher\.(?P<alg>DES|DES3|ARC4)\.new\s*\("""),
        algorithm="dynamic",
        library="Crypto.Cipher",
        api_name="Crypto.Cipher.new",
    ),
    _WeakCipherPattern(
        pattern=re.compile(r"""\b(?P<alg>DES|DES3|ARC4)\.new\s*\("""),
        algorithm="dynamic",
        library="Cipher",
        api_name="Cipher.new",
    ),
    _WeakCipherPattern(
        pattern=re.compile(r"""(?:Crypto|Cryptodome)\.Cipher\.\w+\.new\s*\([^)]*MODE_ECB"""),
        algorithm="ECB",
        library="Crypto.Cipher",
        api_name="Crypto.Cipher.MODE_ECB",
    ),
    _WeakCipherPattern(
        pattern=re.compile(r"""algorithms\.(?P<alg>TripleDES|Blowfish|ARC4|IDEA|CAST5)\s*\("""),
        algorithm="dynamic",
        library="cryptography",
        api_name="algorithms",
    ),
    _WeakCipherPattern(
        pattern=re.compile(r"""(?<![\w/])des\.NewCipher\s*\("""),
        algorithm="DES",
        library="crypto/des",
        api_name="des.NewCipher",
    ),
    _WeakCipherPattern(
        pattern=re.compile(r"""(?<![\w/])des\.NewTripleDESCipher\s*\("""),
        algorithm="3DES",
        library="crypto/des",
        api_name="des.NewTripleDESCipher",
    ),
    _WeakCipherPattern(
        pattern=re.compile(r"""(?<![\w/])rc4\.NewCipher\s*\("""),
        algorithm="RC4",
        library="crypto/rc4",
        api_name="rc4.NewCipher",
    ),
    _WeakCipherPattern(
        pattern=re.compile(
            r"""Cipher\.getInstance\s*\(\s*["'](?P<alg>DES|DESede|RC4|RC2|Blowfish)(?:/[^"']*)?["']\s*\)""",
            re.IGNORECASE,
        ),
        algorithm="dynamic",
        library="Cipher",
        api_name="Cipher.getInstance",
    ),
    _WeakCipherPattern(
        pattern=re.compile(r"""Cipher\.getInstance\s*\(\s*["'][^"']*/ECB/[^"']*["']\s*\)"""),
        algorithm="ECB",
        library="Cipher",
        api_name="Cipher.getInstance",
    ),
    _WeakCipherPattern(
        pattern=re.compile(r"""Cipher\.getInstance\s*\(\s*["']AES["']\s*\)"""),
        algorithm="AES/ECB",
        library="Cipher",
        api_name="Cipher.getInstance",
    ),
)

_STATIC_IV_PATTERNS: tuple[_WeakCipherPattern, ...] = (
    _WeakCipherPattern(
        pattern=re.compile(
            r"""crypto\.createCipheriv\s*\([^,]+,\s*[^,]+,\s*Buffer\.from\s*\(\s*['"][^'"]+['"]\s*\)""",
            re.DOTALL,
        ),
        algorithm="Static IV",
        library="crypto",
        api_name="crypto.createCipheriv",
    ),
    _WeakCipherPattern(
        pattern=re.compile(
            r"""crypto\.createCipheriv\s*\([^,]+,\s*[^,]+,\s*['"][0-9a-fA-F]{16,}['"]\s*\)""",
            re.DOTALL,
        ),
        algorithm="Static IV",
        library="crypto",
        api_name="crypto.createCipheriv",
    ),
)

_KEY_LENGTH_PATTERNS: tuple[_KeyLengthPattern, ...] = (
    _KeyLengthPattern(
        pattern=re.compile(
            r"""(?:generateKeyPair(?:Sync)?|generateKey)\s*\(\s*['"]rsa['"]\s*,\s*\{[^}]*modulusLength\s*:\s*(\d+)""",
            re.DOTALL,
        ),
        algorithm="RSA",
        api_name="generateKeyPair",
        extractor=lambda match: ("RSA", int(match.group(1)), _MIN_RSA_BITS),
    ),
    _KeyLengthPattern(
        pattern=re.compile(
            r"""subtle\.generateKey\s*\(\s*\{[^}]*name\s*:\s*['"]RSA[^'"]*['"][^}]*modulusLength\s*:\s*(\d+)""",
            re.DOTALL,
        ),
        algorithm="RSA",
        api_name="subtle.generateKey",
        extractor=lambda match: ("RSA", int(match.group(1)), _MIN_RSA_BITS),
    ),
    _KeyLengthPattern(
        pattern=re.compile(
            r"""(?:generateKeyPair(?:Sync)?|generateKey)\s*\(\s*['"]ec['"]\s*,\s*\{[^}]*namedCurve\s*:\s*['"]([^'"]+)['"]""",
            re.DOTALL | re.IGNORECASE,
        ),
        algorithm="EC",
        api_name="generateKeyPair",
        extractor=lambda match: ("EC", match.group(1), _MIN_EC_BITS),
    ),
    _KeyLengthPattern(
        pattern=re.compile(
            r"""subtle\.generateKey\s*\(\s*\{[^}]*name\s*:\s*['"]ECDSA['"][^}]*namedCurve\s*:\s*['"]([^'"]+)['"]""",
            re.DOTALL | re.IGNORECASE,
        ),
        algorithm="EC",
        api_name="subtle.generateKey",
        extractor=lambda match: ("EC", match.group(1), _MIN_EC_BITS),
    ),
    _KeyLengthPattern(
        pattern=re.compile(
            r"""(?:generateKey(?:Sync)?)\s*\(\s*['"]aes['"]\s*,\s*\{[^}]*length\s*:\s*(\d+)""",
            re.DOTALL,
        ),
        algorithm="AES",
        api_name="generateKey",
        extractor=lambda match: ("AES", int(match.group(1)), _MIN_AES_BITS),
    ),
    _KeyLengthPattern(
        pattern=re.compile(
            r"""subtle\.generateKey\s*\(\s*\{[^}]*name\s*:\s*['"]AES-[^'"]+['"][^}]*length\s*:\s*(\d+)""",
            re.DOTALL,
        ),
        algorithm="AES",
        api_name="subtle.generateKey",
        extractor=lambda match: ("AES", int(match.group(1)), _MIN_AES_BITS),
    ),
    _KeyLengthPattern(
        pattern=re.compile(
            r"""rsa\.generate_private_key\s*\([^)]*key_size\s*=\s*(\d+)""",
            re.DOTALL,
        ),
        algorithm="RSA",
        api_name="rsa.generate_private_key",
        extractor=lambda match: ("RSA", int(match.group(1)), _MIN_RSA_BITS),
    ),
    _KeyLengthPattern(
        pattern=re.compile(r"""ec\.(SECP192R1|SECP192K1|SECP160R1|SECT163K1)\s*\("""),
        algorithm="EC",
        api_name="ec.curve",
        extractor=lambda match: ("EC", match.group(1), _MIN_EC_BITS),
    ),
    _KeyLengthPattern(
        pattern=re.compile(r"""AESGCM\.generate_key\s*\(\s*bit_length\s*=\s*(\d+)"""),
        algorithm="AES",
        api_name="AESGCM.generate_key",
        extractor=lambda match: ("AES", int(match.group(1)), _MIN_AES_BITS),
    ),
    _KeyLengthPattern(
        pattern=re.compile(r"""rsa\.GenerateKey\s*\(\s*[^,]+,\s*(\d+)\s*\)"""),
        algorithm="RSA",
        api_name="rsa.GenerateKey",
        extractor=lambda match: ("RSA", int(match.group(1)), _MIN_RSA_BITS),
    ),
    _KeyLengthPattern(
        pattern=re.compile(r"""elliptic\.(P192|P160)\s*\("""),
        algorithm="EC",
        api_name="elliptic.curve",
        extractor=lambda match: ("EC", match.group(1), _MIN_EC_BITS),
    ),
    _KeyLengthPattern(
        pattern=re.compile(
            r"""KeyPairGenerator\.getInstance\s*\(\s*["']RSA["']\s*\)[\s\S]{0,200}?\.initialize\s*\(\s*(\d+)""",
            re.DOTALL,
        ),
        algorithm="RSA",
        api_name="KeyPairGenerator.initialize",
        extractor=lambda match: ("RSA", int(match.group(1)), _MIN_RSA_BITS),
    ),
    _KeyLengthPattern(
        pattern=re.compile(r"""ECGenParameterSpec\s*\(\s*["']([^"']+)["']\s*\)""", re.IGNORECASE),
        algorithm="EC",
        api_name="ECGenParameterSpec",
        extractor=lambda match: ("EC", match.group(1), _MIN_EC_BITS),
    ),
    _KeyLengthPattern(
        pattern=re.compile(
            r"""KeyGenerator\.getInstance\s*\(\s*["']AES["']\s*\)[\s\S]{0,200}?\.init(?:ialize)?\s*\(\s*(\d+)""",
            re.DOTALL,
        ),
        algorithm="AES",
        api_name="KeyGenerator.init",
        extractor=lambda match: ("AES", int(match.group(1)), _MIN_AES_BITS),
    ),
)

_HTTP_LITERAL_PATTERNS = (
    re.compile(
        r"""(?:fetch|request)\s*\(\s*['"`](http://[^'"`\s]+)['"`]""",
        re.IGNORECASE,
    ),
    re.compile(
        r"""axios\.(?:get|post|put|delete|patch|request)\s*\(\s*['"`](http://[^'"`\s]+)['"`]""",
        re.IGNORECASE,
    ),
    re.compile(
        r"""(?:requests|httpx)\.(?:get|post|put|delete|patch|head|options|request)\s*\(\s*['"](http://[^'"\s]+)['"]""",
        re.IGNORECASE,
    ),
    re.compile(
        r"""urllib\.request\.urlopen\s*\(\s*['"](http://[^'"\s]+)['"]""",
        re.IGNORECASE,
    ),
    re.compile(
        r"""http\.(?:Get|Post)\s*\(\s*"(http://[^"\s]+)"|"""
        r"""http\.New(?:Request|RequestWithContext)\s*\(\s*"[^"]+"\s*,\s*"(http://[^"\s]+)" """,
        re.IGNORECASE,
    ),
    re.compile(
        r"""(?:new\s+(?:[A-Za-z_][\w$]*\.)*URL|URI\.create)\s*\(\s*["'](http://[^"'\s]+)["']\s*\)""",
        re.IGNORECASE,
    ),
)

_WEAK_TLS_PATTERNS = (
    (re.compile(r"""minVersion\s*:\s*['"]TLS(?:v1|v1[.]0|v1[.]1)['"]"""), "minVersion"),
    (
        re.compile(r"""secureProtocol\s*:\s*['"](?:TLSv1_method|SSLv3_method|SSLv23_method)['"]"""),
        "secureProtocol",
    ),
    (re.compile(r"""ssl\.PROTOCOL_(?:TLSv1|TLSv1_1|SSLv3|SSLv23)\b"""), "ssl.PROTOCOL"),
    (
        re.compile(r"""minimum_version\s*=\s*ssl\.TLSVersion\.(?:TLSv1|TLSv1_1|SSLv3)\b"""),
        "ssl.minimum_version",
    ),
    (
        re.compile(r"""MinVersion\s*:\s*tls\.(?:VersionTLS10|VersionTLS11|VersionSSL30)\b"""),
        "tls.MinVersion",
    ),
    (
        re.compile(
            r"""SSLContext\.getInstance\s*\(\s*["'](?:TLSv1|TLSv1[.]1|SSLv3|SSL)["']\s*\)"""
        ),
        "SSLContext.getInstance",
    ),
)

_CERT_VALIDATION_PATTERNS = (
    (re.compile(r"""rejectUnauthorized\s*:\s*false"""), "rejectUnauthorized"),
    (
        re.compile(r"""NODE_TLS_REJECT_UNAUTHORIZED\s*=\s*['"]?0['"]?"""),
        "NODE_TLS_REJECT_UNAUTHORIZED",
    ),
    (
        re.compile(
            r"""process\.env(?:\[['"]NODE_TLS_REJECT_UNAUTHORIZED['"]\]|\.NODE_TLS_REJECT_UNAUTHORIZED)\s*=\s*['"]0['"]"""
        ),
        "NODE_TLS_REJECT_UNAUTHORIZED",
    ),
    (
        re.compile(
            r"""new\s+https?\.Agent\s*\(\s*\{[^}]*rejectUnauthorized\s*:\s*false""", re.DOTALL
        ),
        "https.Agent",
    ),
    (
        re.compile(
            r"""requests\.(?:get|post|put|delete|patch|head|options|request)\s*\([^)]*verify\s*=\s*False""",
            re.DOTALL,
        ),
        "requests.verify",
    ),
    (
        re.compile(r"""httpx\.(?:Client|AsyncClient)\s*\([^)]*verify\s*=\s*False""", re.DOTALL),
        "httpx.verify",
    ),
    (re.compile(r"""ssl\._create_unverified_context\s*\("""), "ssl._create_unverified_context"),
    (
        re.compile(
            r"""ssl\.create_default_context\s*\([^)]*\)[\s\S]{0,120}?check_hostname\s*=\s*False""",
            re.DOTALL,
        ),
        "ssl.check_hostname",
    ),
    (
        re.compile(
            r"""ssl\.create_default_context\s*\([^)]*\)[\s\S]{0,120}?verify_mode\s*=\s*ssl\.CERT_NONE""",
            re.DOTALL,
        ),
        "ssl.verify_mode",
    ),
    (re.compile(r"""urllib3\.disable_warnings\s*\("""), "urllib3.disable_warnings"),
    (re.compile(r"""InsecureRequestWarning"""), "InsecureRequestWarning"),
    (re.compile(r"""InsecureSkipVerify\s*:\s*true"""), "InsecureSkipVerify"),
    (
        re.compile(
            r"""implements\s+X509TrustManager[\s\S]{0,500}?checkServerTrusted[^{]*\{[\s\S]{0,40}?\}""",
            re.DOTALL,
        ),
        "X509TrustManager.checkServerTrusted",
    ),
    (
        re.compile(r"""HostnameVerifier[\s\S]{0,200}?return\s+true""", re.DOTALL),
        "HostnameVerifier",
    ),
)

_WEAK_PRNG_ASSIGNMENTS: tuple[_WeakPrngAssignmentPattern, ...] = (
    _WeakPrngAssignmentPattern(
        pattern=re.compile(
            r"""(?P<lhs>[A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*)?)\s*=\s*(?P<rhs>[^;\n]*Math\.random\s*\(\)[^;\n]*)""",
            re.IGNORECASE,
        ),
        prng_name="Math.random()",
        api_name="Math.random",
    ),
    _WeakPrngAssignmentPattern(
        pattern=re.compile(
            r"""(?:const|let|var)\s+(?P<lhs>[A-Za-z_$][\w$]*)\s*=\s*(?P<rhs>[^;\n]*Math\.random\s*\(\)[^;\n]*)""",
            re.IGNORECASE,
        ),
        prng_name="Math.random()",
        api_name="Math.random",
    ),
    _WeakPrngAssignmentPattern(
        pattern=re.compile(
            r"""(?P<lhs>[A-Za-z_][\w.]*)\s*=\s*(?P<rhs>[^#\n]*\brandom\.(?:random|randint|choice|getrandbits|uniform|randrange|sample|shuffle)\s*\([^#\n]*)""",
            re.IGNORECASE,
        ),
        prng_name="random.*",
        api_name="random",
    ),
    _WeakPrngAssignmentPattern(
        pattern=re.compile(
            r"""(?:(?P<lhs>[A-Za-z_][\w]*)\s*:=|var\s+(?P<lhs_decl>[A-Za-z_][\w]*)\s*=)\s*(?P<rhs>[^\n]*\brand\.(?:Intn|Int|Float64)\s*\([^\n]*)""",
            re.IGNORECASE,
        ),
        prng_name="math/rand",
        api_name="rand",
    ),
    _WeakPrngAssignmentPattern(
        pattern=re.compile(
            r"""(?:(?:final|private|public|protected|static)\s+)*(?:int|long|double|float|String|byte\[\]|var)\s+(?P<lhs>[A-Za-z_][\w]*)\s*=\s*(?P<rhs>[^;\n]*(?:Math\.random\s*\(\)|new\s+Random\s*\(\)\s*\.\s*next(?:Int|Long|Float|Double))[^;\n]*)""",
            re.IGNORECASE,
        ),
        prng_name="java.util.Random",
        api_name="Random",
    ),
)

_JWT_CALL_START_PATTERNS = (
    (re.compile(r"""\bjwt\.verify\s*\(""", re.IGNORECASE), "jwt.verify"),
    (re.compile(r"""\bjwtVerify\s*\(""", re.IGNORECASE), "jwtVerify"),
    (re.compile(r"""\bjwt\.decode\s*\(""", re.IGNORECASE), "jwt.decode"),
    (re.compile(r"""\bjwt\.sign\s*\(""", re.IGNORECASE), "jwt.sign"),
    (re.compile(r"""\bjwt\.encode\s*\(""", re.IGNORECASE), "jwt.encode"),
    (re.compile(r"""\bjwt\.Parse\s*\(""", re.IGNORECASE), "jwt.Parse"),
)


def extract_crypto_transport_findings(
    project_root: str | Path,
    *,
    frameworks: Sequence[str] | None = None,
    files: Sequence[Path] | None = None,
    include_tests: bool = False,
) -> tuple[CandidateFinding, ...]:
    del frameworks
    root = Path(project_root).resolve(strict=False)
    scanned_files = tuple(_load_scanned_files(root, files=files, include_tests=include_tests))
    findings: list[CandidateFinding] = []
    for scanned_file in scanned_files:
        findings.extend(_detect_weak_hash(scanned_file))
        findings.extend(_detect_weak_cipher(scanned_file))
        findings.extend(_detect_weak_key_length(scanned_file))
        findings.extend(_detect_cleartext_http(scanned_file))
        findings.extend(_detect_weak_tls(scanned_file))
        findings.extend(_detect_cert_validation_disabled(scanned_file))
        findings.extend(_detect_weak_prng(scanned_file))
        findings.extend(_detect_jwt_issues(scanned_file))
    return tuple(_dedupe_findings(findings))


def _load_scanned_files(
    project_root: Path,
    *,
    files: Sequence[Path] | None,
    include_tests: bool,
) -> list[_ScannedFile]:
    candidate_paths = (
        [Path(path) for path in files]
        if files is not None
        else sorted(path for path in project_root.rglob("*") if path.is_file())
    )
    scanned_files: list[_ScannedFile] = []
    for path in candidate_paths:
        if not _should_scan_file(path, project_root=project_root, include_tests=include_tests):
            continue
        scanned_file = _ScannedFile.load(path)
        if scanned_file is not None:
            scanned_files.append(scanned_file)
    return scanned_files


def _should_scan_file(path: Path, *, project_root: Path, include_tests: bool) -> bool:
    if path.suffix not in _SOURCE_FILE_EXTENSIONS:
        return False
    try:
        relative_path = path.resolve(strict=False).relative_to(project_root)
    except ValueError:
        relative_path = path
    if any(part in _DIRECTORY_EXCLUSIONS for part in relative_path.parts[:-1]):
        return False
    return include_tests or not _is_test_file(relative_path)


def _is_test_file(relative_path: Path) -> bool:
    if any(part.lower() in _TEST_DIRECTORY_NAMES for part in relative_path.parts[:-1]):
        return True
    return any(pattern.match(relative_path.name) for pattern in _TEST_FILENAME_PATTERNS)


def _detect_weak_hash(scanned_file: _ScannedFile) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    for spec in _WEAK_HASH_PATTERNS:
        for match in spec.pattern.finditer(scanned_file.text):
            block_start, block_end = scanned_file.containing_block(match.start())
            block_text = scanned_file.text[block_start:block_end]
            line_text = _line_text(
                scanned_file.text, scanned_file.location_for_index(match.start()).line
            )
            security_context = _classify_security_context("\n".join((line_text, block_text)))
            if security_context == "checksum":
                continue
            algorithm = _normalize_algorithm(
                match.groupdict().get("alg") or match.groupdict().get("alg_new") or spec.algorithm
            )
            findings.append(
                _build_crypto_finding(
                    cwe_id="CWE-328",
                    location=scanned_file.location_for_index(match.start()),
                    api_name=spec.api_name,
                    parameter_name=algorithm,
                    confidence=0.95 if security_context != "unknown" else 0.7,
                    metadata={
                        "weak_algorithm": algorithm,
                        "recommended_alternative": _recommended_hash_alternative(algorithm),
                        "security_context": security_context,
                        "library": spec.library,
                    },
                )
            )
    return findings


def _detect_weak_cipher(scanned_file: _ScannedFile) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    for spec in (*_WEAK_CIPHER_PATTERNS, *_STATIC_IV_PATTERNS):
        for match in spec.pattern.finditer(scanned_file.text):
            algorithm = _normalize_algorithm(match.groupdict().get("alg") or spec.algorithm)
            findings.append(
                _build_crypto_finding(
                    cwe_id="CWE-327",
                    location=scanned_file.location_for_index(match.start()),
                    api_name=spec.api_name,
                    parameter_name=algorithm,
                    confidence=0.9,
                    metadata={
                        "weak_algorithm": algorithm,
                        "recommended_alternative": _recommended_cipher_alternative(algorithm),
                        "security_context": "encryption",
                        "library": spec.library,
                    },
                )
            )
    return findings


def _detect_weak_key_length(scanned_file: _ScannedFile) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    for spec in _KEY_LENGTH_PATTERNS:
        for match in spec.pattern.finditer(scanned_file.text):
            algorithm, raw_value, minimum = spec.extractor(match)
            if algorithm == "EC":
                curve = str(raw_value).lower()
                if curve not in _WEAK_EC_CURVES:
                    continue
                reported_value: int | str = raw_value
            else:
                bits = int(raw_value)
                if bits >= minimum:
                    continue
                reported_value = bits
            findings.append(
                _build_crypto_finding(
                    cwe_id="CWE-326",
                    location=scanned_file.location_for_index(match.start()),
                    api_name=spec.api_name,
                    parameter_name=algorithm,
                    confidence=0.95,
                    metadata={
                        "algorithm": algorithm,
                        "configured_value": reported_value,
                        "minimum_bits": minimum,
                    },
                )
            )
    return findings


def _detect_cleartext_http(scanned_file: _ScannedFile) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    for pattern in _HTTP_LITERAL_PATTERNS:
        for match in pattern.finditer(scanned_file.text):
            url = next((group for group in match.groups() if group), None)
            if url is None or _LOCALHOST_HTTP_PATTERN.match(url):
                continue
            findings.append(
                _build_crypto_finding(
                    cwe_id="CWE-319",
                    location=scanned_file.location_for_index(match.start()),
                    api_name="http://",
                    parameter_name=url,
                    confidence=0.85,
                    metadata={
                        "transport": "http",
                        "url": url,
                    },
                )
            )
    return findings


def _detect_weak_tls(scanned_file: _ScannedFile) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    for pattern, api_name in _WEAK_TLS_PATTERNS:
        for match in pattern.finditer(scanned_file.text):
            findings.append(
                _build_crypto_finding(
                    cwe_id="CWE-319",
                    location=scanned_file.location_for_index(match.start()),
                    api_name=api_name,
                    parameter_name="tls_version",
                    confidence=0.9,
                    metadata={
                        "transport": "tls",
                        "issue": "weak_tls_version",
                    },
                )
            )
    return findings


def _detect_cert_validation_disabled(scanned_file: _ScannedFile) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    for pattern, api_name in _CERT_VALIDATION_PATTERNS:
        for match in pattern.finditer(scanned_file.text):
            confidence = (
                0.8
                if api_name in {"X509TrustManager.checkServerTrusted", "HostnameVerifier"}
                else 0.95
            )
            findings.append(
                _build_crypto_finding(
                    cwe_id="CWE-295",
                    location=scanned_file.location_for_index(match.start()),
                    api_name=api_name,
                    parameter_name="certificate_validation",
                    confidence=confidence,
                    metadata={
                        "validation_disabled": True,
                        "pattern": api_name,
                    },
                )
            )
    return findings


def _detect_weak_prng(scanned_file: _ScannedFile) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    for spec in _WEAK_PRNG_ASSIGNMENTS:
        for match in spec.pattern.finditer(scanned_file.text):
            lhs = match.groupdict().get("lhs") or match.groupdict().get("lhs_decl") or ""
            rhs = match.groupdict().get("rhs") or ""
            block_start, block_end = scanned_file.containing_block(match.start())
            block_text = scanned_file.text[block_start:block_end]
            line_number = scanned_file.location_for_index(match.start()).line
            line_text = _line_text(scanned_file.text, line_number)
            context_text = "\n".join((line_text, rhs, block_text))
            has_security_context = (
                _text_has_security_term(lhs)
                or _text_has_security_term(context_text)
                or _matches_any(context_text, _PRNG_SECURITY_SINKS)
            )
            if _matches_any(context_text, _NON_SECURITY_PRNG_CONTEXT) and not has_security_context:
                continue
            if has_security_context:
                findings.append(
                    _build_crypto_finding(
                        cwe_id="CWE-338",
                        location=scanned_file.location_for_index(match.start()),
                        api_name=spec.api_name,
                        parameter_name=lhs or spec.prng_name,
                        confidence=0.85,
                        metadata={
                            "weak_prng": spec.prng_name,
                            "flow_kind": "direct_assignment",
                            "security_context": _classify_prng_context(context_text),
                        },
                    )
                )
                continue
            if not lhs:
                continue
            propagated_match = _find_prng_sink_use(block_text[match.end() - block_start :], lhs)
            if propagated_match is None:
                continue
            findings.append(
                _build_crypto_finding(
                    cwe_id="CWE-338",
                    location=scanned_file.location_for_index(match.start()),
                    api_name=spec.api_name,
                    parameter_name=lhs,
                    confidence=0.7,
                    metadata={
                        "weak_prng": spec.prng_name,
                        "flow_kind": "local_variable_flow",
                        "security_context": _classify_prng_context(propagated_match.group(0)),
                    },
                )
            )
    return findings


def _detect_jwt_issues(scanned_file: _ScannedFile) -> list[CandidateFinding]:
    findings: list[CandidateFinding] = []
    seen: set[tuple[str, int]] = set()

    for match in re.finditer(r"""Algorithm\.none\s*\(""", scanned_file.text):
        findings.append(
            _build_crypto_finding(
                cwe_id="CWE-347",
                location=scanned_file.location_for_index(match.start()),
                api_name="Algorithm.none",
                parameter_name="alg",
                confidence=0.95,
                metadata={"sub_type": "alg_none"},
            )
        )

    for match in _JWT_NONE_ALG_PATTERN.finditer(scanned_file.text):
        findings.append(
            _build_crypto_finding(
                cwe_id="CWE-347",
                location=scanned_file.location_for_index(match.start()),
                api_name="jwt.algorithms",
                parameter_name="alg",
                confidence=0.95,
                metadata={"sub_type": "alg_none"},
            )
        )

    for pattern, api_name in _JWT_CALL_START_PATTERNS:
        for match in pattern.finditer(scanned_file.text):
            open_paren = scanned_file.text.find("(", match.start())
            call_text = _extract_balanced_segment(scanned_file.text, open_paren)
            if not call_text:
                continue
            args = _split_top_level_args(call_text[call_text.find("(") + 1 : -1])
            key = (api_name, match.start())
            if key in seen:
                continue
            seen.add(key)

            if api_name in {"jwt.verify", "jwtVerify"}:
                options = args[2] if len(args) >= 3 else ""
                if not _options_has_algorithms(options):
                    findings.append(
                        _build_crypto_finding(
                            cwe_id="CWE-347",
                            location=scanned_file.location_for_index(match.start()),
                            api_name=api_name,
                            parameter_name="algorithms",
                            confidence=0.9,
                            metadata={"sub_type": "no_algorithm_restriction"},
                        )
                    )
                if (
                    len(args) >= 2
                    and _JWT_PUBLIC_KEY_NAMES.search(args[1])
                    and not _options_has_algorithms(options)
                ):
                    findings.append(
                        _build_crypto_finding(
                            cwe_id="CWE-347",
                            location=scanned_file.location_for_index(match.start()),
                            api_name=api_name,
                            parameter_name="verification_key",
                            confidence=0.7,
                            metadata={"sub_type": "symmetric_asymmetric_confusion"},
                        )
                    )
                if len(args) >= 2 and _is_string_literal(args[1]):
                    findings.append(
                        _build_crypto_finding(
                            cwe_id="CWE-347",
                            location=scanned_file.location_for_index(match.start()),
                            api_name=api_name,
                            parameter_name="secret",
                            confidence=0.95,
                            metadata={
                                "sub_type": "hardcoded_jwt_secret",
                                "suppressed_cwe_798": True,
                                "secret_literal": _redacted_literal(args[1]),
                            },
                        )
                    )
            elif api_name == "jwt.decode":
                if not any("algorithms" in argument for argument in args[2:]) and not any(
                    "algorithms" in argument for argument in args
                ):
                    findings.append(
                        _build_crypto_finding(
                            cwe_id="CWE-347",
                            location=scanned_file.location_for_index(match.start()),
                            api_name=api_name,
                            parameter_name="algorithms",
                            confidence=0.9,
                            metadata={"sub_type": "no_algorithm_restriction"},
                        )
                    )
                if len(args) >= 2 and _is_string_literal(args[1]):
                    findings.append(
                        _build_crypto_finding(
                            cwe_id="CWE-347",
                            location=scanned_file.location_for_index(match.start()),
                            api_name=api_name,
                            parameter_name="secret",
                            confidence=0.95,
                            metadata={
                                "sub_type": "hardcoded_jwt_secret",
                                "suppressed_cwe_798": True,
                                "secret_literal": _redacted_literal(args[1]),
                            },
                        )
                    )
            elif api_name in {"jwt.sign", "jwt.encode"}:
                if len(args) >= 2 and _is_string_literal(args[1]):
                    findings.append(
                        _build_crypto_finding(
                            cwe_id="CWE-347",
                            location=scanned_file.location_for_index(match.start()),
                            api_name=api_name,
                            parameter_name="secret",
                            confidence=0.95,
                            metadata={
                                "sub_type": "hardcoded_jwt_secret",
                                "suppressed_cwe_798": True,
                                "secret_literal": _redacted_literal(args[1]),
                            },
                        )
                    )
            elif api_name == "jwt.Parse":
                if "token.Method" not in call_text and "SigningMethod" not in call_text:
                    findings.append(
                        _build_crypto_finding(
                            cwe_id="CWE-347",
                            location=scanned_file.location_for_index(match.start()),
                            api_name=api_name,
                            parameter_name="signing_method",
                            confidence=0.85,
                            metadata={"sub_type": "no_algorithm_restriction"},
                        )
                    )

    for match in re.finditer(
        r"""Algorithm\.HMAC(?:256|384|512)\s*\(\s*['"][^'"]+['"]\s*\)""", scanned_file.text
    ):
        findings.append(
            _build_crypto_finding(
                cwe_id="CWE-347",
                location=scanned_file.location_for_index(match.start()),
                api_name="Algorithm.HMAC",
                parameter_name="secret",
                confidence=0.95,
                metadata={
                    "sub_type": "hardcoded_jwt_secret",
                    "suppressed_cwe_798": True,
                },
            )
        )

    return _dedupe_findings(findings)


def _find_prng_sink_use(text: str, identifier: str) -> re.Match[str] | None:
    identifier_pattern = re.compile(rf"""\b{re.escape(identifier)}\b""")
    for line in text.splitlines():
        if not identifier_pattern.search(line):
            continue
        if _matches_any(line, _NON_SECURITY_PRNG_CONTEXT):
            continue
        if _text_has_security_term(line) or _matches_any(line, _PRNG_SECURITY_SINKS):
            return re.search(r".+", line)
    return None


def _classify_security_context(text: str) -> str:
    if _matches_any(text, _NON_SECURITY_CONTEXT_INDICATORS) and not _matches_any(
        text, _SECURITY_CONTEXT_INDICATORS
    ):
        return "checksum"
    lowered = text.lower()
    if re.search(r"\b(?:password|passwd|pwd)\b", lowered):
        return "password_hashing"
    if re.search(r"\b(?:token|session|csrf|nonce|otp|api[_-]?key|auth)\b", lowered):
        return "token_generation"
    if re.search(r"\b(?:signature|sign|verify|hmac|jwt)\b", lowered):
        return "signature"
    return "unknown"


def _classify_prng_context(text: str) -> str:
    lowered = _normalize_identifier_text(text)
    if "password" in lowered or "passwd" in lowered:
        return "password"
    if any(token in lowered for token in ("token", "session", "csrf", "nonce", "otp")):
        return "token"
    if any(token in lowered for token in ("secret", "salt", "key", "iv")):
        return "key_material"
    if any(token in lowered for token in ("sign", "hmac", "encrypt")):
        return "crypto"
    return "unknown"


def _normalize_algorithm(value: str) -> str:
    normalized = value.replace("_", "-").replace("/", "-").strip().upper()
    normalized = normalized.replace("SHA1", "SHA-1").replace("DES-EDE3", "3DES")
    if normalized == "SHA":
        return "SHA-1"
    if normalized == "DES3":
        return "3DES"
    if normalized == "ARC4":
        return "RC4"
    return normalized


def _recommended_hash_alternative(algorithm: str) -> str:
    if algorithm == "MD5":
        return "SHA-256 (integrity) or bcrypt/argon2 (passwords)"
    return "SHA-256+"


def _recommended_cipher_alternative(algorithm: str) -> str:
    if algorithm in {"STATIC IV", "ECB", "AES/ECB"}:
        return "AES-GCM or ChaCha20-Poly1305 with a random IV/nonce"
    return "AES-GCM or ChaCha20-Poly1305"


def _matches_any(text: str, patterns: Iterable[re.Pattern[str]]) -> bool:
    return any(pattern.search(text) is not None for pattern in patterns)


def _normalize_identifier_text(text: str) -> str:
    expanded = re.sub(r"([a-z0-9])([A-Z])", r"\1_\2", text)
    return re.sub(r"[^A-Za-z0-9]+", "_", expanded).lower()


def _text_has_security_term(text: str) -> bool:
    normalized = _normalize_identifier_text(text)
    return any(term in normalized for term in _SECURITY_IDENTIFIER_TERMS)


def _options_has_algorithms(argument: str) -> bool:
    return bool(re.search(r"""\balgorithms\s*[:=]""", argument, re.IGNORECASE))


def _is_string_literal(argument: str) -> bool:
    stripped = argument.strip()
    if len(stripped) < 2:
        return False
    quote = stripped[0]
    return quote in {'"', "'"} and stripped[-1] == quote


def _redacted_literal(argument: str) -> str:
    stripped = argument.strip()
    if _is_string_literal(stripped):
        return f"{stripped[0]}[REDACTED_SECRET]{stripped[0]}"
    return "[REDACTED_SECRET]"


def _extract_balanced_segment(text: str, open_index: int) -> str:
    if open_index < 0 or open_index >= len(text) or text[open_index] != "(":
        return ""
    depth = 0
    quote: str | None = None
    escape = False
    for index in range(open_index, len(text)):
        char = text[index]
        if quote is not None:
            if escape:
                escape = False
                continue
            if char == "\\":
                escape = True
                continue
            if char == quote:
                quote = None
            continue
        if char in {"'", '"', "`"}:
            quote = char
            continue
        if char == "(":
            depth += 1
        elif char == ")":
            depth -= 1
            if depth == 0:
                return text[text.rfind("\n", 0, open_index) + 1 : index + 1]
    return ""


def _split_top_level_args(text: str) -> list[str]:
    arguments: list[str] = []
    current: list[str] = []
    stack: list[str] = []
    quote: str | None = None
    escape = False
    pairs = {"(": ")", "{": "}", "[": "]"}
    closers = {value: key for key, value in pairs.items()}
    for char in text:
        if quote is not None:
            current.append(char)
            if escape:
                escape = False
                continue
            if char == "\\":
                escape = True
                continue
            if char == quote:
                quote = None
            continue
        if char in {"'", '"', "`"}:
            quote = char
            current.append(char)
            continue
        if char in pairs:
            stack.append(char)
            current.append(char)
            continue
        if char in closers:
            if stack and stack[-1] == closers[char]:
                stack.pop()
            current.append(char)
            continue
        if char == "," and not stack:
            candidate = "".join(current).strip()
            if candidate:
                arguments.append(candidate)
            current = []
            continue
        current.append(char)
    candidate = "".join(current).strip()
    if candidate:
        arguments.append(candidate)
    return arguments


def _build_crypto_finding(
    *,
    cwe_id: str,
    location: SourceLocation,
    api_name: str,
    parameter_name: str | None,
    confidence: float,
    metadata: dict[str, object] | None = None,
) -> CandidateFinding:
    return CandidateFinding(
        id=_crypto_finding_id(
            cwe_id=cwe_id,
            file=location.file,
            line=location.line,
            column=location.column,
            api_name=api_name,
        ),
        vuln_class=cwe_id,
        source=TaintSource(
            location=location,
            source_type=_STATIC_SOURCE_TYPE,
            data_categories=list(_DEFAULT_DATA_CATEGORIES),
            parameter_name=parameter_name,
        ),
        sink=TaintSink(
            location=location,
            sink_type=_STATIC_SINK_TYPE,
            api_name=api_name,
        ),
        taint_path=[],
        path_conditions=[],
        confidence=confidence,
        severity=severity_for_cwe(cwe_id),
        metadata=metadata or {},
    )


def _crypto_finding_id(
    *,
    cwe_id: str,
    file: str,
    line: int,
    column: int,
    api_name: str,
) -> str:
    material = "|".join((cwe_id, file, str(line), str(column), api_name))
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
    for index, char in enumerate(text):
        if char == "{":
            stack.append(index)
        elif char == "}" and stack:
            pairs[stack.pop()] = index
    return pairs


__all__ = ["extract_crypto_transport_findings"]
