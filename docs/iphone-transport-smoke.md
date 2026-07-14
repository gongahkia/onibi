# iPhone transport smoke runner

`scripts/iphone-transport-smoke.sh` records a physical-device transport run without writing pair tokens, owner cookies, approval payloads, or terminal content to the artifact. It does not start Onibi or handle credentials.

Start Onibi with the transport under test, copy the owner pairing URL, then run:

```bash
scripts/iphone-transport-smoke.sh \
  --transport=lan \
  --pair-url='https://192.168.1.42:8443/pair/<token>' \
  --out=/tmp/onibi-iphone-smoke
```

The runner first executes `go test -race ./internal/e2e ./internal/web`, then records `pass`, `fail`, or `skip` for setup/health, pairing, approval, intervention, reconnect, teardown, and failure diagnostics. It exits non-zero unless every check passes. Use `--record <check=status>` for an operator-driven non-interactive run and `--allow-skip` only when collecting incomplete rehearsal evidence.

Each artifact is mode `0600` and contains the transport, origin only, timestamps, local-verification result, completion state, and checkpoint statuses. It never stores the pairing path or fragment.

Physical-device evidence remains required before claiming iPhone transport certification. Attach the generated JSON and any separately reviewed screenshots to the corresponding issue.
