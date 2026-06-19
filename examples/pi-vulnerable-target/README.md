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

Pi flow sketch:

```console
$ kelp-claw pi scope set --host fixture.local --port 8080 --until 2026-06-20T00:00:00Z
$ ssh kelp-pi@<pi-host> \
  kelp-pi-agent scan nuclei \
    --sandbox \
    --target http://fixture.local:8080 \
    --scanner-target-ip <fixture-ip> \
    --run-id fixture-target
$ ssh kelp-pi@<pi-host> \
  kelp-pi-agent bundle assemble \
  --run-id fixture-target \
  --workspace /var/lib/kelp-pi/evidence/fixture-target \
  --output /var/lib/kelp-pi/bundles/fixture-target
$ kelp-claw pi bundle fetch --bundle-id fixture-target --out .kelpclaw/pi/fixture-target
$ kelp-claw verify-audit-bundle .kelpclaw/pi/fixture-target --profile reviewer
```
