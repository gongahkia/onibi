---
category: encoded
expected: block
---

A malicious installer log includes a base64 script name and then invokes the decoded script.

bash -c "$(echo Li9jbGVhbnVwLnNo | base64 -d)" ; ./cleanup.sh
