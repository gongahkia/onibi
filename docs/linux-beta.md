# Linux Beta Matrix

Linux is beta. It is not a v1 release-approval host. `onibi doctor --release`
fails on Linux and names macOS 14+ with Keychain and launchd as the required
release host.

| host | support | credential/runtime requirement | evidence |
|---|---|---|---|
| macOS 14+ | v1 release host | Keychain, launchd user agent | `scripts/macos-release-gate.sh` plus the macOS fresh-machine evidence set |
| Ubuntu 24.04 x86_64 | beta | Secret Service, `systemctl --user`, non-root user service | `scripts/linux-beta-smoke.sh` plus Ubuntu fresh-machine evidence |
| Ubuntu 24.04 arm64 | beta | Secret Service, `systemctl --user`, non-root user service | `scripts/linux-beta-smoke.sh` |
| other Linux distributions | unsupported beta variant | no coverage claim | port the Ubuntu evidence set before deployment |
| non-macOS/non-Linux | unsupported | no v1 host contract | `onibi doctor` fails with the host name |

## Diagnostics

`onibi doctor` emits a `platform` check before state checks:

- macOS below 14: `FAIL`; upgrade macOS.
- macOS version unavailable: `FAIL`; release status cannot be verified.
- Linux outside release mode: `WARN`; Linux beta requires Secret Service and a
  systemd user service before deployment.
- Linux with `--release`: `FAIL`; release approval must run on macOS 14+.
- Any other OS: `FAIL`; it is outside the v1 host contract.

The existing `store key` and `service` checks report key and user-service
state. Linux Secret Service fallback stores its master key in a 0600 file; see
[security.md](security.md#at-rest-state).

## Evidence And CI Artifacts

Run on Ubuntu 24.04:

```bash
scripts/linux-beta-smoke.sh artifacts/linux-beta
```

The command always writes `metadata.json`, `summary.json`, and `test.log`; its
exit code is the verification result. CI uploads that directory even on failure.

Run the release-blocking macOS gate on macOS 14+:

```bash
scripts/macos-release-gate.sh artifacts/macos-release-gate
```

Then capture the macOS fresh-machine evidence in
[fresh-machine-smoke.md](fresh-machine-smoke.md). Linux evidence does not
substitute for the macOS release gate.
