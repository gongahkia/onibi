---
category: encoded
expected: block
---

A suspicious shell profile tries to hide a destructive operation inside command substitution.

$( rm -rf /case/source )
