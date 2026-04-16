# Prompt: Add NodeGoat Known-Miss Regression Coverage

You are working in Piranesi, an AppSec CLI. The repository needs stronger regression coverage against realistic vulnerable applications. NodeGoat is a common intentionally vulnerable Node.js app and should be used as an inspiration or fixture source for known vulnerability shapes, without vendoring unnecessary large external code.

Goal: add focused NodeGoat-style regression fixtures for known detection misses, especially source-to-sink shapes that are realistic but currently under-covered.

Implementation requirements:

- Inspect existing evaluation and synthetic fixtures under `eval/` and tests under `tests/`.
- Identify small, self-contained Node.js fixture snippets that represent NodeGoat-like patterns, such as SQL/NoSQL injection, command injection, SSRF, insecure redirects, weak crypto, or path traversal.
- Add minimal fixture files rather than copying a full external repository.
- Add tests that assert Piranesi detects the expected vulnerability class and does not overmatch safe neighbor code.
- If a fixture represents a current miss, mark the test clearly and implement the minimum detector fix needed to make it pass if feasible within this task.
- Document why each fixture exists and what behavior it locks.

Acceptance criteria:

- The repo has named NodeGoat-style regression coverage.
- Tests are small, deterministic, and not dependent on network access.
- Each fixture ties to a specific detection behavior.
- If any known miss remains unresolved, it is documented as an expected limitation rather than silently ignored.
