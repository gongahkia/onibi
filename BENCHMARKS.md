# Performance

No production Telegram or iPhone-to-Ghostty latency baseline is verified in this repository. `make unit` runs race-enabled hermetic tests; `make test` additionally requires the live Telegram credentials described in the README. `make build` verifies both local binaries. Add measured, reproducible latency or memory data here only after recording the exact host, command, and run count.
