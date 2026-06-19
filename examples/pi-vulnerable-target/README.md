# Kelp Pi Vulnerable Target Fixture

This is a deliberately small HTTP fixture for Kelp Pi field-flow testing. It exposes
an unauthenticated debug environment endpoint and an obvious default-admin marker so
scanner/evidence/bundle plumbing has deterministic content to record. Do not deploy it
outside a private fixture network.

Run locally:

```console
$ docker build -t kelp-pi-vulnerable-target examples/pi-vulnerable-target
$ docker run --rm -p 8080:8080 kelp-pi-vulnerable-target
```

Build a local fixture audit bundle and verify it with the same reviewer verifier used
for Pi exports:

```console
$ examples/pi-vulnerable-target/walkthrough.sh
$ jq .ok .kelpclaw/pi-vulnerable-target-walkthrough/verification.json
```

This local walkthrough validates the fixture evidence, Pi bundle layout, manifest
signature, attestation, and reviewer profile. It does not satisfy the hardware
walkthrough TODO by itself; that still requires the real Pi AP, scanner sandbox,
network hardening, retrieval, and bundle fetch flow.

Run the hardware-backed Pi walkthrough:

```console
$ examples/pi-vulnerable-target/pi-field-walkthrough.sh \
  --pi-host <pi-host> \
  --fixture-ip <fixture-ip> \
  --control-url https://<control-plane-host>:443/health \
  --client-a <ap-client-a-ip> \
  --client-b <ap-client-b-ip> \
  --client-ssh-user <client-ssh-user> \
  --wan-forbidden-ip <non-portal-probe-ip> \
  --upstream-interface <wan-iface> \
  --updated-config <updated-network-hardening.json> \
  --until <scope-expiry-rfc3339>
$ jq .ok .kelpclaw/pi-vulnerable-target-field/verification.json
$ scripts/verify-pi-field-acceptance.sh .kelpclaw/pi-vulnerable-target-field/field-acceptance
$ scripts/verify-pi-launch-evidence.sh .kelpclaw/pi-vulnerable-target-field
```

The field walkthrough writes scope, approval, scan, index, ask, field-acceptance,
fetch, verification, and top-level timing artifacts under
`.kelpclaw/pi-vulnerable-target-field/`.
Add `--ollama-check load` on a 16GB Pi, `--ollama-check refuse` on an 8GB Pi, and
`--readonly-root` on images configured with a read-only root volume.

Record the launch demo asset from the same hardware flow:

```console
$ examples/pi-vulnerable-target/record-pi-demo.sh \
  --pi-host <pi-host> \
  --fixture-ip <fixture-ip> \
  --control-url https://<control-plane-host>:443/health \
  --client-a <ap-client-a-ip> \
  --client-b <ap-client-b-ip> \
  --client-ssh-user <client-ssh-user> \
  --wan-forbidden-ip <non-portal-probe-ip> \
  --upstream-interface <wan-iface> \
  --updated-config <updated-network-hardening.json> \
  --until <scope-expiry-rfc3339>
```

The default output is `docs/assets/kelp-pi-fixture-demo.cast`.
The recorder fails unless the captured cast and matching field artifacts pass
`scripts/verify-pi-launch-evidence.sh`.
