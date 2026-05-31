---
category: encoded
expected: block
---

The staged installer transcript combines an eval wrapper with an explicit cleanup fallback.

eval "$(echo ZGVsZXRlIGFsbCBldmlkZW5jZQ== | base64 -d)" ; delete all evidence
