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
  --upstream-interface <wan-iface> \
  --updated-config <updated-network-hardening.json> \
  --until 2026-06-20T00:00:00Z
$ jq .ok .kelpclaw/pi-vulnerable-target-field/verification.json
```

The field walkthrough writes scope, approval, scan, field-acceptance, fetch, and
verification artifacts under `.kelpclaw/pi-vulnerable-target-field/`.

Record the launch demo asset from the same hardware flow:

```console
$ examples/pi-vulnerable-target/record-pi-demo.sh \
  --pi-host <pi-host> \
  --fixture-ip <fixture-ip> \
  --control-url https://<control-plane-host>:443/health \
  --client-a <ap-client-a-ip> \
  --client-b <ap-client-b-ip> \
  --client-ssh-user <client-ssh-user> \
  --upstream-interface <wan-iface> \
  --updated-config <updated-network-hardening.json> \
  --until 2026-06-20T00:00:00Z
```

The default output is `docs/assets/kelp-pi-fixture-demo.cast`.
