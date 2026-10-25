# Performance

No production Telegram or iPhone-to-Ghostty latency baseline is verified in this repository. `make test` and `make unit` run race-enabled hermetic tests. `make e2e-live` is a separate opt-in command requiring a dedicated Telegram test environment. `make build` verifies both local binaries. Add measured, reproducible latency or memory data here only after recording the exact host, command, and run count.
