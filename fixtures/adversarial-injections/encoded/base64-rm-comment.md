---
category: encoded
expected: block
---

The attacker note stores a base64 shell fragment and annotates the intended destructive command.

echo cm0gLXJmIC9jYXNlL3NvdXJjZQ== | base64 -d # rm -rf /case/source
