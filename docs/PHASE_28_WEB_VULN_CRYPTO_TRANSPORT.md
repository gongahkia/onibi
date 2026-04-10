# Phase 28: Cryptographic Failures & Transport Security (OWASP A02)

**Estimated effort: 55-70 ideal hours**
**Blocked by: Phase 16 (OWASP coverage — CWE-798 in `detect/secrets.py`), Phase 22 (advanced taint — PRNG context needs taint)**
**Blocks: Nothing (incremental value)**
**Target milestone: v0.5.0**

---

## 1. Overview

OWASP A02 (Cryptographic Failures) is the #2 most critical web application security risk. Piranesi currently detects CWE-798 (Hardcoded Credentials) via `src/piranesi/detect/secrets.py`, but has no coverage for broader cryptographic and transport-layer weaknesses.

### 1.1 Gap Analysis

| OWASP A02 Sub-Category | CWE | Current Coverage | This Phase |
|-------------------------|-----|------------------|------------|
| Hardcoded credentials | CWE-798 | `detect/secrets.py` | Overlap integration |
| Broken/risky crypto algorithm | CWE-327 | None | New |
| Weak hash (non-HMAC) | CWE-328 | None | New |
| Cleartext transmission | CWE-319 | Partial (HSTS header only in `misconfigurations.py`) | Extended |
| Inadequate key length | CWE-326 | None | New |
| Improper certificate validation | CWE-295 | None | New |
| Weak PRNG for security | CWE-338 | None | New |
| Improper JWT verification | CWE-347 | None | New |

### 1.2 Detection Strategy

Most CWEs in this phase are **pattern-based** (regex on source text), not taint-based. The exceptions:
- **CWE-338** (Weak PRNG): requires taint analysis to determine if `Math.random()` output flows to a security-sensitive sink (token/key/nonce generation).
- **CWE-319** (Cleartext Transmission): pattern-based for `http://` URLs, but taint-based for user-controlled URL schemes.

### 1.3 New Severity Mappings

Add to `_SEVERITY_BY_CWE` in `src/piranesi/detect/flows.py`:

```python
"CWE-327": "high",     # broken crypto
"CWE-328": "medium",   # weak hash
"CWE-319": "medium",   # cleartext transmission
"CWE-326": "high",     # inadequate key length
"CWE-295": "high",     # improper cert validation
"CWE-338": "medium",   # weak PRNG
"CWE-347": "high",     # improper JWT verification
```

---

## 2. Weak Cryptographic Algorithm Detection (CWE-327, CWE-328)

**Estimated effort: 12-15h**

### 2.1 Weak Algorithm Catalog

| Algorithm | CWE | Risk | Recommended Alternative |
|-----------|-----|------|------------------------|
| MD5 | CWE-328 | Collision attacks, rainbow tables | SHA-256+ for integrity, bcrypt/argon2 for passwords |
| SHA-1 (security context) | CWE-328 | Collision attacks (SHAttered) | SHA-256+ |
| DES | CWE-327 | 56-bit key, brute-forceable | AES-256-GCM |
| 3DES / DESede | CWE-327 | Sweet32, 64-bit block | AES-256-GCM |
| RC4 | CWE-327 | Biased keystream | AES-256-GCM / ChaCha20-Poly1305 |
| ECB mode | CWE-327 | Deterministic, pattern-leaking | CBC with random IV, or GCM |
| Static/hardcoded IV | CWE-327 | IV reuse breaks semantic security | Random IV per encryption |
| Blowfish (for passwords) | CWE-327 | 64-bit block, Sweet32 | AES-256-GCM (encryption), bcrypt (passwords, OK) |

### 2.2 Language-Specific Patterns

#### 2.2.1 JavaScript / TypeScript

```python
_JS_WEAK_HASH_PATTERNS = (
    # Node crypto module
    re.compile(r"""crypto\.createHash\s*\(\s*['"](?:md5|sha1|md4|ripemd160)['"]\s*\)"""),
    # CryptoJS
    re.compile(r"""CryptoJS\.(?:MD5|SHA1|MD4|RIPEMD160)\s*\("""),
    # Web Crypto API (subtle.digest with weak alg)
    re.compile(r"""subtle\.digest\s*\(\s*['"](?:MD5|SHA-1)['"]\s*,"""),
)

_JS_WEAK_CIPHER_PATTERNS = (
    # DES, 3DES, RC4, ECB mode
    re.compile(r"""crypto\.createCipher(?:iv)?\s*\(\s*['"](?:des|des3|des-ede3?|rc4|des-ecb|des-cbc|aes-\d+-ecb)['"]\s*,"""),
    # CryptoJS ciphers
    re.compile(r"""CryptoJS\.(?:DES|TripleDES|RC4|Rabbit)\.(?:encrypt|decrypt)\s*\("""),
    # CryptoJS ECB mode
    re.compile(r"""mode\s*:\s*CryptoJS\.mode\.ECB"""),
)

_JS_STATIC_IV_PATTERNS = (
    # Buffer.from('...') as IV argument — IV should be random
    re.compile(r"""crypto\.createCipheriv\s*\([^,]+,\s*[^,]+,\s*Buffer\.from\s*\(\s*['"][^'"]+['"]\s*\)"""),
    # Hardcoded hex/base64 IV
    re.compile(r"""crypto\.createCipheriv\s*\([^,]+,\s*[^,]+,\s*['"][0-9a-fA-F]{16,}['"]\s*\)"""),
)
```

#### 2.2.2 Python

```python
_PY_WEAK_HASH_PATTERNS = (
    # hashlib
    re.compile(r"""hashlib\.(?:md5|sha1|new\s*\(\s*['"](?:md5|sha1)['"])\s*\("""),
    # Crypto.Hash
    re.compile(r"""(?:Crypto|Cryptodome)\.Hash\.(?:MD5|SHA|MD4|MD2|RIPEMD)\.new\s*\("""),
)

_PY_WEAK_CIPHER_PATTERNS = (
    # PyCryptodome DES/3DES/RC4
    re.compile(r"""(?:Crypto|Cryptodome)\.Cipher\.(?:DES|DES3|ARC4)\.new\s*\("""),
    # PyCryptodome ECB mode
    re.compile(r"""(?:Crypto|Cryptodome)\.Cipher\.\w+\.new\s*\([^)]*MODE_ECB"""),
    # cryptography library with weak algo
    re.compile(r"""algorithms\.(?:TripleDES|Blowfish|ARC4|IDEA|CAST5)\s*\("""),
)
```

#### 2.2.3 Go

```python
_GO_WEAK_CRYPTO_IMPORTS = (
    # import path matching
    re.compile(r"""["']crypto/(?:md5|sha1|des|rc4)["']"""),
)

_GO_WEAK_HASH_CALLS = (
    re.compile(r"""md5\.(?:New|Sum)\s*\("""),
    re.compile(r"""sha1\.(?:New|Sum)\s*\("""),
)

_GO_WEAK_CIPHER_CALLS = (
    re.compile(r"""des\.NewCipher\s*\("""),
    re.compile(r"""des\.NewTripleDESCipher\s*\("""),
    re.compile(r"""rc4\.NewCipher\s*\("""),
)
```

#### 2.2.4 Java

```python
_JAVA_WEAK_HASH_PATTERNS = (
    re.compile(r"""MessageDigest\.getInstance\s*\(\s*["'](?:MD5|MD2|SHA-1|SHA1)["']\s*\)"""),
    re.compile(r"""DigestUtils\.(?:md5|sha1|md5Hex|sha1Hex)\s*\("""),
)

_JAVA_WEAK_CIPHER_PATTERNS = (
    re.compile(r"""Cipher\.getInstance\s*\(\s*["'](?:DES|DESede|RC4|RC2|Blowfish)(?:/[^"']*)?["']\s*\)"""),
    # ECB mode explicitly
    re.compile(r"""Cipher\.getInstance\s*\(\s*["'][^"']*/ECB/[^"']*["']\s*\)"""),
    # AES without mode (defaults to ECB in Java)
    re.compile(r"""Cipher\.getInstance\s*\(\s*["']AES["']\s*\)"""),
)
```

### 2.3 Context Sensitivity (FP Reduction)

MD5/SHA-1 for non-security purposes (checksums, cache keys, ETags) should NOT trigger. Implement context heuristics:

```python
_NON_SECURITY_CONTEXT_INDICATORS = (
    re.compile(r"""#\s*(?:checksum|etag|cache|fingerprint|content.?hash|file.?hash|integrity)""", re.IGNORECASE),
    re.compile(r"""//\s*(?:checksum|etag|cache|fingerprint|content.?hash|file.?hash|integrity)""", re.IGNORECASE),
    re.compile(r"""(?:checksum|etag|cache_key|content_hash|file_hash)\s*=""", re.IGNORECASE),
)

_SECURITY_CONTEXT_INDICATORS = (
    re.compile(r"""(?:password|passwd|pwd|credential|secret|token|auth|session|hmac|signature|sign|verify)\s*""", re.IGNORECASE),
)
```

**Logic:**
1. For each weak hash match, extract the surrounding block (same `containing_block()` approach as `misconfigurations.py`).
2. If block text matches any `_NON_SECURITY_CONTEXT_INDICATORS` AND does NOT match `_SECURITY_CONTEXT_INDICATORS` → skip (confidence 0.0).
3. If block text matches `_SECURITY_CONTEXT_INDICATORS` → confidence 0.95.
4. Default (ambiguous context): confidence 0.7 for CWE-328, 0.85 for CWE-327 (cipher is almost always security-relevant).

### 2.4 Metadata

Each finding's `metadata` dict includes:

```python
{
    "weak_algorithm": "MD5",
    "recommended_alternative": "SHA-256 (integrity) or bcrypt/argon2 (passwords)",
    "security_context": "password_hashing",  # or "checksum", "unknown"
    "library": "crypto",
}
```

---

## 3. Inadequate Key Length (CWE-326)

**Estimated effort: 6-8h**

### 3.1 Minimum Key Length Requirements

| Algorithm | Minimum Acceptable | Common Weak Values |
|-----------|-------------------|-------------------|
| RSA | 2048 bits | 512, 1024 |
| EC (ECDSA/ECDH) | 256 bits (P-256) | 160, 192 |
| AES | 128 bits | N/A (AES is always 128/192/256) |
| HMAC key | 256 bits (32 bytes) | Short string literals |

### 3.2 Language-Specific Patterns

#### 3.2.1 JavaScript / TypeScript

```python
_JS_RSA_KEY_LENGTH = re.compile(
    r"""(?:generateKeyPair(?:Sync)?|generateKey)\s*\(\s*['"]rsa['"]\s*,\s*\{[^}]*modulusLength\s*:\s*(\d+)""",
    re.DOTALL,
)
# Match: crypto.generateKeyPairSync('rsa', { modulusLength: 1024 })
# Capture group 1: key length integer

_JS_EC_KEY_LENGTH = re.compile(
    r"""(?:generateKeyPair(?:Sync)?|generateKey)\s*\(\s*['"]ec['"]\s*,\s*\{[^}]*namedCurve\s*:\s*['"](?:secp192[kr]1|secp160[kr]1|prime192v1)['"]""",
    re.DOTALL,
)
# Weak curves: secp192k1, secp192r1, secp160k1, secp160r1, prime192v1

_JS_WEB_CRYPTO_RSA = re.compile(
    r"""subtle\.generateKey\s*\(\s*\{[^}]*name\s*:\s*['"]RSA[^'"]*['"][^}]*modulusLength\s*:\s*(\d+)""",
    re.DOTALL,
)
```

#### 3.2.2 Python

```python
_PY_RSA_KEY_LENGTH = re.compile(
    r"""rsa\.generate_private_key\s*\([^)]*key_size\s*=\s*(\d+)""",
    re.DOTALL,
)
# Match: rsa.generate_private_key(public_exponent=65537, key_size=1024)

_PY_EC_WEAK_CURVE = re.compile(
    r"""ec\.(?:SECP192R1|SECP192K1|SECP160R1|SECT163K1)\s*\(""",
)

_PY_DSA_KEY_LENGTH = re.compile(
    r"""dsa\.generate_private_key\s*\([^)]*key_size\s*=\s*(\d+)""",
    re.DOTALL,
)
```

#### 3.2.3 Go

```python
_GO_RSA_KEY_LENGTH = re.compile(
    r"""rsa\.GenerateKey\s*\(\s*[^,]+,\s*(\d+)\s*\)""",
)
# Match: rsa.GenerateKey(rand.Reader, 1024)

_GO_EC_WEAK_CURVE = re.compile(
    r"""elliptic\.(?:P192|P160)\s*\(""",
)
```

#### 3.2.4 Java

```python
_JAVA_RSA_KEY_LENGTH = re.compile(
    r"""KeyPairGenerator\.getInstance\s*\(\s*["']RSA["']\s*\)[\s\S]{0,200}?\.initialize\s*\(\s*(\d+)""",
    re.DOTALL,
)
# Match: kpg.initialize(1024)

_JAVA_EC_KEY_LENGTH = re.compile(
    r"""ECGenParameterSpec\s*\(\s*["'](?:secp192r1|prime192v1|secp160r1)["']\s*\)""",
)
```

### 3.3 Validation Logic

```python
_MIN_RSA_BITS = 2048
_MIN_EC_BITS = 256  # P-256 equivalent

def _is_weak_rsa_key(bits: int) -> bool:
    return bits < _MIN_RSA_BITS

def _is_weak_ec_curve(curve_name: str) -> bool:
    _WEAK_CURVES = {"secp192r1", "secp192k1", "secp160r1", "secp160k1", "prime192v1", "sect163k1"}
    return curve_name.lower() in _WEAK_CURVES
```

Confidence: 0.95 (key length is explicit and unambiguous).

---

## 4. Cleartext Transmission (CWE-319)

**Estimated effort: 8-10h**

### 4.1 HTTP URL in API Calls

Detect `http://` string literals (excluding localhost/loopback) in outbound HTTP call contexts.

```python
_HTTP_URL_PATTERN = re.compile(
    r"""['"`]http://(?!localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1\]|localhost:)([^'"`\s]+)['"`]""",
)

# Call context: only flag when URL appears inside a fetch/axios/request/http.get call
_HTTP_CALL_CONTEXT = re.compile(
    r"""(?:fetch|axios\.(?:get|post|put|delete|patch|request)|http\.(?:get|request)|https?\.(?:get|request)|got(?:\.(?:get|post|put|delete|patch))?|superagent\.(?:get|post|put|delete)|request(?:\.(?:get|post|put|delete))?|urllib\.request\.urlopen|requests\.(?:get|post|put|delete|patch|head|options)|http\.(?:Get|Post|Head|NewRequest))\s*\(""",
)
```

**Logic:**
1. Find all `http://` string literals (non-localhost).
2. Check if the literal appears within an HTTP call context (same line or containing expression).
3. Confidence: 0.85 for matches in call context, 0.5 for standalone string assignments.

### 4.2 TLS Version Downgrade

```python
_WEAK_TLS_PATTERNS = (
    # Node.js: tls.createServer with weak minVersion
    re.compile(r"""minVersion\s*:\s*['"]TLS(?:v1|v1\.0|v1\.1)['"]"""),
    # Node.js: secureProtocol with weak protocol
    re.compile(r"""secureProtocol\s*:\s*['"](?:TLSv1_method|SSLv3_method|SSLv23_method)['"]"""),
    # Python: ssl.PROTOCOL_TLSv1 or ssl.PROTOCOL_SSLv3
    re.compile(r"""ssl\.PROTOCOL_(?:TLSv1|TLSv1_1|SSLv3|SSLv23)\b"""),
    # Python: ssl context minimum version
    re.compile(r"""minimum_version\s*=\s*ssl\.TLSVersion\.(?:TLSv1|TLSv1_1|SSLv3)\b"""),
    # Go: tls.VersionTLS10, tls.VersionTLS11, tls.VersionSSL30
    re.compile(r"""MinVersion\s*:\s*tls\.(?:VersionTLS10|VersionTLS11|VersionSSL30)\b"""),
    # Java: SSLContext.getInstance with weak protocol
    re.compile(r"""SSLContext\.getInstance\s*\(\s*["'](?:TLSv1|TLSv1\.1|SSLv3|SSL)["']\s*\)"""),
)
```

### 4.3 Framework-Specific Missing HTTPS Redirect

```python
# Express: no HTTPS redirect middleware
_EXPRESS_HTTPS_REDIRECT_PATTERN = re.compile(
    r"""(?:app\.use|router\.use)\s*\([^)]*(?:redirect.*https|enforce.*https|requireHTTPS|ssl)""",
    re.IGNORECASE,
)

# Django settings
_DJANGO_SSL_REDIRECT_FALSE = re.compile(
    r"""SECURE_SSL_REDIRECT\s*=\s*False""",
)

# Flask: no Talisman / no SSLify
_FLASK_TALISMAN_PATTERN = re.compile(r"""Talisman\s*\(""")
_FLASK_SSLIFY_PATTERN = re.compile(r"""SSLify\s*\(""")
```

Confidence: 0.80 (framework heuristic, may be behind a reverse proxy).

---

## 5. Improper Certificate Validation (CWE-295)

**Estimated effort: 6-8h**

### 5.1 Language-Specific Patterns

#### 5.1.1 JavaScript / TypeScript

```python
_JS_CERT_VALIDATION_DISABLED = (
    # TLS options
    re.compile(r"""rejectUnauthorized\s*:\s*false"""),
    # Environment variable override
    re.compile(r"""NODE_TLS_REJECT_UNAUTHORIZED\s*=\s*['"]?0['"]?"""),
    re.compile(r"""process\.env\.NODE_TLS_REJECT_UNAUTHORIZED\s*=\s*['"]0['"]"""),
    re.compile(r"""process\.env\s*\[\s*['"]NODE_TLS_REJECT_UNAUTHORIZED['"]\s*\]\s*=\s*['"]0['"]"""),
    # Agent with disabled check
    re.compile(r"""new\s+https?\.Agent\s*\(\s*\{[^}]*rejectUnauthorized\s*:\s*false""", re.DOTALL),
)
```

#### 5.1.2 Python

```python
_PY_CERT_VALIDATION_DISABLED = (
    # requests library
    re.compile(r"""requests\.(?:get|post|put|delete|patch|head|options|request)\s*\([^)]*verify\s*=\s*False""", re.DOTALL),
    # urllib3
    re.compile(r"""urllib3\.disable_warnings\s*\("""),
    re.compile(r"""InsecureRequestWarning"""),
    # ssl context
    re.compile(r"""ssl\._create_unverified_context\s*\("""),
    re.compile(r"""ssl\.create_default_context\s*\([^)]*\)[\s\S]{0,100}?check_hostname\s*=\s*False""", re.DOTALL),
    re.compile(r"""ssl\.create_default_context\s*\([^)]*\)[\s\S]{0,100}?verify_mode\s*=\s*ssl\.CERT_NONE""", re.DOTALL),
    # httpx
    re.compile(r"""httpx\.(?:Client|AsyncClient)\s*\([^)]*verify\s*=\s*False""", re.DOTALL),
)
```

#### 5.1.3 Go

```python
_GO_CERT_VALIDATION_DISABLED = (
    re.compile(r"""InsecureSkipVerify\s*:\s*true"""),
)
```

#### 5.1.4 Java

```python
_JAVA_CERT_VALIDATION_DISABLED = (
    # Custom TrustManager that accepts all (heuristic: implements X509TrustManager with empty checkServerTrusted)
    re.compile(r"""implements\s+X509TrustManager[\s\S]{0,500}?checkServerTrusted[^{]*\{[\s\S]{0,20}?\}""", re.DOTALL),
    # HostnameVerifier that returns true
    re.compile(r"""HostnameVerifier[\s\S]{0,200}?return\s+true""", re.DOTALL),
    # SSLContext with permissive TrustManager
    re.compile(r"""setDefaultSSLSocketFactory"""),
)
```

### 5.2 Confidence

- `rejectUnauthorized: false` → 0.95 (direct and unambiguous).
- `NODE_TLS_REJECT_UNAUTHORIZED = '0'` → 0.95.
- `verify=False` in Python → 0.90 (could be intentional for internal service, but still a finding).
- `InsecureSkipVerify: true` in Go → 0.95.
- Java custom TrustManager → 0.80 (heuristic pattern matching, could have FPs with more complex implementations).

---

## 6. Weak PRNG for Security (CWE-338)

**Estimated effort: 8-10h**

### 6.1 Insecure PRNG Sources

| Language | Insecure | Secure Alternative |
|----------|----------|-------------------|
| JS/TS | `Math.random()` | `crypto.randomBytes()`, `crypto.getRandomValues()`, `crypto.randomUUID()` |
| Python | `random.random()`, `random.randint()`, `random.choice()`, `random.getrandbits()` | `secrets.token_hex()`, `secrets.token_urlsafe()`, `secrets.token_bytes()`, `os.urandom()` |
| Go | `math/rand.Intn()`, `math/rand.Int()`, `math/rand.Float64()` | `crypto/rand.Read()`, `crypto/rand.Int()` |
| Java | `java.util.Random` | `java.security.SecureRandom` |

### 6.2 Taint-Based Detection (JS/TS)

Unlike other CWEs in this phase, CWE-338 requires **context analysis** — `Math.random()` for UI jitter or animation is not a vulnerability. Only flag when the PRNG output flows to a security-sensitive sink.

**Security-sensitive sinks for PRNG:**

```python
_PRNG_SECURITY_SINKS = (
    # Token/session generation
    re.compile(r"""(?:token|session[_-]?id|csrf|nonce|otp|api[_-]?key|secret|salt)\s*=""", re.IGNORECASE),
    # Password/key generation
    re.compile(r"""(?:password|passwd|key|iv|initialization.?vector)\s*=""", re.IGNORECASE),
    # Cryptographic operations
    re.compile(r"""crypto\.|encrypt|decrypt|sign|hmac""", re.IGNORECASE),
    # Auth-related
    re.compile(r"""(?:reset[_-]?token|verification[_-]?code|auth[_-]?code|invite[_-]?code)\s*=""", re.IGNORECASE),
)
```

**Detection approach:**

1. **Pattern-only (high confidence):** `Math.random()` directly assigned to a variable matching `_PRNG_SECURITY_SINKS` patterns. Confidence: 0.85.
2. **Taint-based (medium confidence):** `Math.random()` as source, any `_PRNG_SECURITY_SINKS` identifier as sink. Uses existing taint infrastructure from `detect/flows.py`. Confidence: 0.7.
3. **Pattern-only (skip):** `Math.random()` in context matching `_NON_SECURITY_PRNG_CONTEXT`. Confidence: 0.0.

```python
_NON_SECURITY_PRNG_CONTEXT = (
    re.compile(r"""(?:animation|color|position|width|height|opacity|delay|jitter|shuffle|sample|random.?color|placeholder)""", re.IGNORECASE),
    re.compile(r"""(?:Math\.floor\s*\(\s*Math\.random\(\)\s*\*\s*(?:colors|items|options|elements|list))""", re.IGNORECASE),
    re.compile(r"""\.style\."""),
    re.compile(r"""(?:test|spec|mock|fixture|seed|example)""", re.IGNORECASE),
)
```

### 6.3 Python-Specific

```python
_PY_WEAK_PRNG_PATTERNS = (
    re.compile(r"""(?<!\w)random\.(?:random|randint|choice|getrandbits|uniform|randrange|sample|shuffle)\s*\("""),
)

_PY_WEAK_PRNG_IMPORT = re.compile(r"""(?:from\s+random\s+import|import\s+random)""")
```

Same context logic: check if the random output is assigned to a security-sensitive variable or flows to a crypto/auth context.

---

## 7. JWT Verification Issues (CWE-347)

**Estimated effort: 10-12h**

### 7.1 alg: "none" Acceptance

Libraries that accept `alg: "none"` in JWT headers allow forged tokens.

```python
# jsonwebtoken: verify() without algorithms option
_JWT_NO_ALG_RESTRICTION = re.compile(
    r"""jwt\.verify\s*\(\s*[^,]+,\s*[^,]+\s*\)""",
)
# Negative match: jwt.verify(token, secret, { algorithms: [...] })
_JWT_WITH_ALG_OPTION = re.compile(
    r"""jwt\.verify\s*\(\s*[^,]+,\s*[^,]+,\s*\{[^}]*algorithms\s*:""",
    re.DOTALL,
)

# jose: jwtVerify without algorithms
_JOSE_NO_ALG_RESTRICTION = re.compile(
    r"""jwtVerify\s*\(\s*[^,]+,\s*[^,]+\s*\)""",
)

# PyJWT: decode() without algorithms
_PYJWT_NO_ALG_RESTRICTION = re.compile(
    r"""jwt\.decode\s*\(\s*[^,]+,\s*[^,]+(?:,\s*(?:options|verify)\s*=)?[^)]*\)""",
)
_PYJWT_WITH_ALG = re.compile(
    r"""jwt\.decode\s*\([^)]*algorithms\s*=""",
    re.DOTALL,
)

# Go: jwt.Parse without method validation
_GO_JWT_NO_ALG = re.compile(
    r"""jwt\.Parse\s*\(\s*[^,]+,\s*func""",
)
_GO_JWT_WITH_METHOD_CHECK = re.compile(
    r"""token\.Method\s*!=|jwt\.SigningMethod""",
)
```

**Logic:** Match `_JWT_NO_ALG_RESTRICTION` AND NOT `_JWT_WITH_ALG_OPTION`. For Go, match `_GO_JWT_NO_ALG` AND check the callback body does NOT contain `_GO_JWT_WITH_METHOD_CHECK`.

### 7.2 Symmetric/Asymmetric Key Confusion

When an HMAC secret is set to a public key value, an attacker who knows the public key can forge tokens signed with HMAC.

```python
# Detect: jwt.verify(token, publicKey) where publicKey was loaded from PEM/cert
_JWT_PUBKEY_AS_HMAC = re.compile(
    r"""jwt\.verify\s*\(\s*[^,]+,\s*(?:publicKey|pubKey|cert|certificate|rsaPublicKey)\s*\)""",
)
# Without explicit algorithm restriction, jsonwebtoken defaults to accepting HMAC too
```

Confidence: 0.70 (heuristic based on variable naming; may need manual review).

### 7.3 Missing Claims Validation

```python
# jsonwebtoken: verify without issuer/audience/expiresIn checks
_JWT_MISSING_CLAIMS = {
    "issuer": re.compile(r"""issuer\s*:|iss\s*:"""),
    "audience": re.compile(r"""audience\s*:|aud\s*:"""),
    "expiration": re.compile(r"""(?:expiresIn|maxAge|exp)\s*:"""),
}
```

**Logic:** After finding `jwt.verify()` or `jwt.decode()`, extract the options object and check for presence of claims validation. Missing `issuer` or `audience` → INFO severity (advisory, not always required). Missing `exp` check → medium severity.

### 7.4 Hardcoded JWT Secret

Overlaps with CWE-798 (`detect/secrets.py`). Cross-reference:

```python
_JWT_HARDCODED_SECRET = re.compile(
    r"""jwt\.sign\s*\(\s*[^,]+,\s*['"][^'"]{1,100}['"]\s*[,)]""",
)
# jwt.sign(payload, 'my-secret-key')
# Also: jwt.verify(token, 'my-secret-key')
```

If `detect/secrets.py` already flags the same string as CWE-798, deduplicate by adding `metadata.overlapping_cwe = "CWE-798"` to the CWE-347 finding and suppressing the CWE-798 duplicate (prefer the more specific CWE-347 classification).

### 7.5 Library Coverage Matrix

| Library | Language | alg:none | Key Confusion | Claims | Hardcoded Secret |
|---------|----------|----------|---------------|--------|-----------------|
| `jsonwebtoken` | JS/TS | `verify()` no `algorithms` | pubKey as HMAC | options obj | `sign(payload, 'literal')` |
| `jose` | JS/TS | `jwtVerify()` no alg | N/A (strict by default) | N/A | N/A |
| `passport-jwt` | JS/TS | `secretOrKey` config | N/A | `issuer`/`audience` in opts | literal in opts |
| `PyJWT` | Python | `decode()` no `algorithms` | `decode(t, pubkey)` | `options={"verify_exp": False}` | `encode(p, 'literal')` |
| `go-jwt` (`golang-jwt`) | Go | `Parse()` callback | method check in callback | `Valid()` override | N/A |
| `java-jwt` (Auth0) | Java | `Algorithm.none()` | `Algorithm.HMAC256(rsaPubKey)` | `withIssuer()`/`withAudience()` | `Algorithm.HMAC256("literal")` |

---

## 8. Implementation Architecture

### 8.1 New Module: `src/piranesi/detect/crypto_transport.py`

Follow the same structural pattern as `detect/misconfigurations.py`:

```python
# src/piranesi/detect/crypto_transport.py
from __future__ import annotations

import hashlib
import re
from bisect import bisect_right
from collections.abc import Sequence
from dataclasses import dataclass
from pathlib import Path

from piranesi.detect.flows import severity_for_cwe
from piranesi.models import CandidateFinding, SourceLocation, TaintSink, TaintSource

_SOURCE_FILE_EXTENSIONS = frozenset({
    ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs",  # JS/TS
    ".py",                                           # Python
    ".go",                                           # Go
    ".java",                                         # Java
})

def extract_crypto_transport_findings(
    project_root: str | Path,
    *,
    frameworks: Sequence[str] | None = None,
    files: Sequence[Path] | None = None,
    include_tests: bool = False,
) -> tuple[CandidateFinding, ...]:
    """Main entry point. Returns all crypto/transport findings."""
    root = Path(project_root).resolve(strict=False)
    scanned_files = _load_scanned_files(root, files=files, include_tests=include_tests)
    findings: list[CandidateFinding] = []
    for sf in scanned_files:
        findings.extend(_detect_weak_hash(sf))
        findings.extend(_detect_weak_cipher(sf))
        findings.extend(_detect_static_iv(sf))
        findings.extend(_detect_weak_key_length(sf))
        findings.extend(_detect_cleartext_http(sf))
        findings.extend(_detect_weak_tls(sf))
        findings.extend(_detect_cert_validation_disabled(sf))
        findings.extend(_detect_weak_prng(sf))
        findings.extend(_detect_jwt_issues(sf))
    return tuple(_dedupe(findings))
```

### 8.2 Integration Points

#### 8.2.1 `detect/__init__.py`

Add import:
```python
from piranesi.detect.crypto_transport import extract_crypto_transport_findings
```

#### 8.2.2 `pipeline.py`

Call `extract_crypto_transport_findings()` in the detect stage, parallel to `extract_misconfiguration_findings()` and `extract_secret_findings()`:

```python
crypto_findings = extract_crypto_transport_findings(
    project_root=config.project_root,
    frameworks=scan_result.frameworks,
    files=changed_files,
    include_tests=config.include_tests,
)
all_findings.extend(crypto_findings)
```

#### 8.2.3 `detect/flows.py` — Severity Map

Extend `_SEVERITY_BY_CWE`:
```python
"CWE-327": "high",
"CWE-328": "medium",
"CWE-326": "high",
"CWE-295": "high",
"CWE-338": "medium",
"CWE-347": "high",
# CWE-319 already present as "medium"
```

#### 8.2.4 `detect/secrets.py` — Dedup Coordination

When `crypto_transport.py` produces a CWE-347 finding for a hardcoded JWT secret, and `secrets.py` produces a CWE-798 finding for the same line, the pipeline deduplicates by keeping the CWE-347 finding (more specific) and setting `metadata["suppressed_cwe_798"] = True`.

Dedup logic in pipeline:

```python
def _deduplicate_jwt_secrets(
    crypto_findings: Sequence[CandidateFinding],
    secret_findings: Sequence[CandidateFinding],
) -> tuple[list[CandidateFinding], list[CandidateFinding]]:
    jwt_lines = {
        (f.source.location.file, f.source.location.line)
        for f in crypto_findings
        if f.vuln_class == "CWE-347" and f.metadata.get("sub_type") == "hardcoded_jwt_secret"
    }
    filtered_secrets = [
        f for f in secret_findings
        if not (f.vuln_class == "CWE-798" and (f.source.location.file, f.source.location.line) in jwt_lines)
    ]
    return list(crypto_findings), filtered_secrets
```

### 8.3 `_ScannedFile` Reuse

The `_ScannedFile` dataclass from `misconfigurations.py` (with `path`, `text`, `line_starts`, `brace_pairs`, `location_for_index()`, `containing_block()`) should be extracted to a shared utility module `detect/_scan_util.py` or imported from misconfigurations. For this phase, initially duplicate (consistent with misconfigurations.py pattern), then refactor in a follow-up.

### 8.4 Finding Construction

Reuse the same `_build_static_finding()` pattern from `misconfigurations.py`:

```python
_STATIC_SOURCE_TYPE = "cryptographic_configuration"
_STATIC_SINK_TYPE = "cryptographic_weakness"

def _build_crypto_finding(
    *,
    cwe_id: str,
    location: SourceLocation,
    api_name: str,
    parameter_name: str | None = None,
    confidence: float = 0.85,
    metadata: dict[str, object] | None = None,
) -> CandidateFinding:
    return CandidateFinding(
        id=_crypto_finding_id(cwe_id=cwe_id, file=location.file, line=location.line, column=location.column, api_name=api_name),
        vuln_class=cwe_id,
        source=TaintSource(
            location=location,
            source_type=_STATIC_SOURCE_TYPE,
            data_categories=["unknown"],
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
```

---

## 9. Testing Strategy

### 9.1 Ground Truth Entries

**Target: 45+ ground truth YAML entries across all 7 CWEs.**

| CWE | True Positives | True Negatives (FP tests) | Total |
|-----|---------------|--------------------------|-------|
| CWE-327 (Broken Crypto) | 6 | 2 | 8 |
| CWE-328 (Weak Hash) | 5 | 3 | 8 |
| CWE-319 (Cleartext) | 4 | 2 | 6 |
| CWE-326 (Key Length) | 4 | 2 | 6 |
| CWE-295 (Cert Validation) | 5 | 2 | 7 |
| CWE-338 (Weak PRNG) | 4 | 3 | 7 |
| CWE-347 (JWT) | 5 | 2 | 7 |
| **Total** | **33** | **16** | **49** |

### 9.2 Fixture Files

Create under `eval/synthetic/` and `tests/fixtures/`:

```
eval/synthetic/crypto-weak-hash-md5.ts
eval/synthetic/crypto-weak-hash-sha1-checksum.ts       # FP: non-security context
eval/synthetic/crypto-weak-cipher-des.ts
eval/synthetic/crypto-ecb-mode.ts
eval/synthetic/crypto-static-iv.ts
eval/synthetic/crypto-weak-rsa-1024.ts
eval/synthetic/crypto-weak-ec-curve.ts
eval/synthetic/cleartext-http-fetch.ts
eval/synthetic/cleartext-http-localhost.ts              # FP: localhost excluded
eval/synthetic/cert-validation-disabled.ts
eval/synthetic/cert-validation-disabled.py
eval/synthetic/cert-insecure-skip-verify.go
eval/synthetic/prng-math-random-token.ts
eval/synthetic/prng-math-random-animation.ts            # FP: non-security context
eval/synthetic/prng-random-module-password.py
eval/synthetic/jwt-no-algorithm-restriction.ts
eval/synthetic/jwt-hardcoded-secret.ts
eval/synthetic/jwt-missing-claims.ts
eval/synthetic/jwt-pubkey-hmac-confusion.ts
eval/synthetic/crypto-weak-hash-md5.py
eval/synthetic/crypto-weak-cipher-des.py
eval/synthetic/crypto-weak-hash-md5.go
eval/synthetic/crypto-weak-cipher-des.java
eval/synthetic/crypto-weak-rsa-1024.java
```

### 9.3 Test Module

Create `tests/test_detect/test_crypto_transport.py`:

```python
class TestWeakHash:
    def test_md5_in_password_context(self): ...     # CWE-328, TP
    def test_sha1_in_hmac_context(self): ...        # CWE-328, TP
    def test_md5_for_checksum_no_finding(self): ... # CWE-328, FP check
    def test_cryptojs_md5(self): ...                # CWE-328, TP

class TestWeakCipher:
    def test_des_ecb_mode(self): ...                # CWE-327, TP
    def test_rc4_cipher(self): ...                  # CWE-327, TP
    def test_aes_256_gcm_no_finding(self): ...      # CWE-327, FP check
    def test_static_iv(self): ...                   # CWE-327, TP

class TestKeyLength:
    def test_rsa_1024_bits(self): ...               # CWE-326, TP
    def test_rsa_2048_no_finding(self): ...         # CWE-326, FP check
    def test_weak_ec_curve(self): ...               # CWE-326, TP
    def test_p256_no_finding(self): ...             # CWE-326, FP check

class TestCleartextTransmission:
    def test_http_url_in_fetch(self): ...           # CWE-319, TP
    def test_http_localhost_no_finding(self): ...   # CWE-319, FP check
    def test_weak_tls_version(self): ...            # CWE-319, TP

class TestCertValidation:
    def test_reject_unauthorized_false(self): ...   # CWE-295, TP
    def test_node_tls_env_zero(self): ...           # CWE-295, TP
    def test_python_verify_false(self): ...         # CWE-295, TP
    def test_go_insecure_skip_verify(self): ...     # CWE-295, TP

class TestWeakPRNG:
    def test_math_random_for_token(self): ...       # CWE-338, TP
    def test_math_random_for_animation(self): ...   # CWE-338, FP check
    def test_python_random_for_password(self): ...  # CWE-338, TP

class TestJWTVerification:
    def test_verify_no_algorithms(self): ...        # CWE-347, TP
    def test_verify_with_algorithms(self): ...      # CWE-347, FP check
    def test_hardcoded_jwt_secret(self): ...        # CWE-347, TP
    def test_missing_exp_claim(self): ...           # CWE-347, TP
    def test_pyjwt_no_algorithms(self): ...         # CWE-347, TP

class TestDedup:
    def test_jwt_secret_dedup_with_cwe798(self): ...  # dedup logic
```

### 9.4 Language Coverage Matrix

Each CWE must have at least one TP fixture per supported language:

| CWE | JS/TS | Python | Go | Java |
|-----|-------|--------|----|------|
| CWE-327 | `createCipheriv('des-ecb')` | `Crypto.Cipher.DES.new()` | `des.NewCipher()` | `Cipher.getInstance("DES")` |
| CWE-328 | `crypto.createHash('md5')` | `hashlib.md5()` | `md5.New()` | `MessageDigest.getInstance("MD5")` |
| CWE-319 | `fetch('http://api.example.com')` | `requests.get('http://...')` | `http.Get("http://...")` | `new URL("http://...")` |
| CWE-326 | `generateKeyPairSync('rsa', {modulusLength:1024})` | `rsa.generate_private_key(key_size=1024)` | `rsa.GenerateKey(rand,1024)` | `kpg.initialize(1024)` |
| CWE-295 | `rejectUnauthorized: false` | `verify=False` | `InsecureSkipVerify: true` | `X509TrustManager` stub |
| CWE-338 | `Math.random()` → token | `random.randint()` → password | `rand.Intn()` → token | `new Random()` → key |
| CWE-347 | `jwt.verify(t, s)` no alg | `jwt.decode(t, s)` no alg | `jwt.Parse(t, fn)` no method check | `Algorithm.none()` |

---

## 10. Acceptance Criteria

- [ ] `detect/crypto_transport.py` module implemented with `extract_crypto_transport_findings()` entry point
- [ ] CWE-327: Detect DES, 3DES, RC4, ECB mode, static IV across JS/TS, Python, Go, Java
- [ ] CWE-328: Detect MD5, SHA-1 in security contexts; skip checksums/ETags (context-sensitive)
- [ ] CWE-326: Detect RSA < 2048, EC < P-256 across all 4 languages
- [ ] CWE-319: Detect `http://` (non-localhost) in API calls, weak TLS versions, missing HTTPS redirect
- [ ] CWE-295: Detect disabled certificate validation across all 4 languages
- [ ] CWE-338: Detect `Math.random()` / `random.random()` flowing to security sinks; skip UI/animation context
- [ ] CWE-347: Detect JWT `verify()` without `algorithms`, hardcoded secrets, missing claims, key confusion
- [ ] Severity map in `flows.py` updated for all 7 CWEs
- [ ] CWE-798 / CWE-347 dedup logic in pipeline
- [ ] 45+ ground truth entries in `eval/ground_truth/`
- [ ] `tests/test_detect/test_crypto_transport.py` with 30+ test cases
- [ ] FP rate < 15% on synthetic fixtures (context-sensitive detections)
- [ ] All existing tests pass (no regressions)
