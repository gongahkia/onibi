# Onibi Benchmarks

Current verified baseline: macOS Apple Silicon only. Linux amd64, Raspberry Pi 4, Tailscale, Cloudflare Quick E2E, and browser/provider approval RTT require live runners/devices and are not verified in this repo state.

## Reference Machines

| Machine | OS | CPU | Memory | Go | Status |
| --- | --- | --- | --- | --- | --- |
| local-macos-arm64 | macOS 26.5.1 25F80 | Apple M3, 8 logical CPUs | 16 GiB | go1.26.5 | measured 2026-07-09 |
| linux-amd64 | [Unverified] not run | [Unverified] not run | [Unverified] not run | [Unverified] not run | pending runner |
| raspberry-pi-4 | [Unverified] not run | [Unverified] not run | [Unverified] not run | [Unverified] not run | pending device |

## Results

| Metric | Value | Machine | Notes |
| --- | --- | --- | --- |
| Cold start to HTTPS health ready | median 103 ms, mean 227 ms, min 100 ms, max 728 ms, n=5 | local-macos-arm64 | `scripts/bench-coldstart.sh --iterations 5`; first run includes fresh cert/state setup |
| Idle RSS, daemon only | 27.5 MiB | local-macos-arm64 | `scripts/bench-idle-rss.sh --idle-seconds 5`; foreground `onibi run` |
| Idle RSS, daemon + web + active session | 31.1 MiB | local-macos-arm64 | `scripts/bench-idle-rss.sh --idle-seconds 5`; `onibi up --transport=lan` |
| Idle RSS, daemon + web without active session | [Unverified] not supported by current foreground CLI | local-macos-arm64 | `onibi up` starts a managed session by design |
| PTY throughput over `/ws/pty`, local handler | median 585.1 MiB/s, range 483.4-670.7 MiB/s, n=5 | local-macos-arm64 | `scripts/bench-throughput.sh --bytes 1048576 --count 5`; in-process web benchmark, no relay |
| PTY throughput over LAN | [Unverified] not run | local-macos-arm64 | needs paired browser/client runner |
| PTY throughput over Tailscale | [Unverified] not run | external | needs live tailnet/Funnel runner |
| PTY throughput over Cloudflare Quick with E2E | [Unverified] not run | external | needs live `cloudflared` tunnel and relay E2E |
| Approval-decision round-trip, local queue | median 2.050 ms, mean 2.118 ms, min 1.394 ms, max 3.207 ms, n=10 | local-macos-arm64 | `scripts/bench-approval-rtt.sh --count 10`; request -> decide -> waiter delivery, no browser/provider |
| Approval-decision round-trip, browser/provider | [Unverified] not run | external | needs paired browser/provider approval flow |

## Scripts

```bash
scripts/bench-coldstart.sh --iterations 5
scripts/bench-idle-rss.sh --idle-seconds 5
scripts/bench-throughput.sh --bytes 1048576 --count 5
scripts/bench-approval-rtt.sh --count 10
scripts/bench-tolerance.sh
```

`ONIBI_BENCH_BINARY` points scripts at a specific binary. If unset, scripts use `./bin/onibi` when present or build a temp binary. The shell scripts force ephemeral HOME/XDG paths and `ONIBI_STORE_KEY_BACKEND=dotenv` so runs do not touch the user keychain.

## Tolerance

`scripts/bench-tolerance.sh` reruns the local-only scripts twice and fails when median/value drift exceeds `ONIBI_BENCH_TOLERANCE_PCT` (default 10). RSS metrics also allow `ONIBI_BENCH_RSS_TOLERANCE_MIB` absolute drift (default 5 MiB) for hosted-runner allocator/page-accounting noise. CI config includes this gate on `ubuntu-latest`; current published Linux numbers remain [Unverified] until a CI artifact is recorded in this file.

Do not compare first-run cold-start numbers against warm runs unless certificate/state directories are reset the same way.
