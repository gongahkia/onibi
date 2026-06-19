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

Pi flow sketch:

```console
$ kelp-claw pi scope set --host fixture.local --port 8080 --until 2026-06-20T00:00:00Z
$ ssh kelp-pi@<pi-host> \
  kelp-pi-agent scan nuclei --sandbox --target http://fixture.local:8080 --run-id fixture-target
$ ssh kelp-pi@<pi-host> \
  kelp-pi-agent bundle assemble \
  --run-id fixture-target \
  --workspace /var/lib/kelp-pi/evidence/fixture-target \
  --output /var/lib/kelp-pi/bundles/fixture-target
$ kelp-claw pi bundle fetch --bundle-id fixture-target --out .kelpclaw/pi/fixture-target
$ kelp-claw verify-audit-bundle .kelpclaw/pi/fixture-target --profile reviewer
```
