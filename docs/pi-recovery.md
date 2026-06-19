# Kelp Pi Recovery Runbook

Status: operational draft for the current repo state on 2026-06-19.

Current implementation caveat: `kelp-pi-agent keygen` stores `pi-ed25519.key.json`
as private key material protected by filesystem mode `0600`, not encrypted
passphrase custody. Treat a copied private key file as sensitive secret material.

## Use Cases

Use this runbook when `/var/lib/kelp-pi` is corrupted, the Pi will not pass
preflight, or the last bundle must be recovered from a damaged field unit.

Do not start scanners or ingest new evidence during recovery. Work from a copied data
directory when possible.

## Boot A Rescue System

1. Power off the Pi.
2. Boot from a known-good recovery SD card or NVMe image.
3. Mount the damaged data volume read-only when the OS supports it:

   ```sh
   sudo mkdir -p /mnt/kelp-recover
   sudo mount -o ro /dev/<old-data-partition> /mnt/kelp-recover
   ```

4. Copy the data directory to recovery media:

   ```sh
   rsync -aHAX --numeric-ids /mnt/kelp-recover/var/lib/kelp-pi/ /recovery/kelp-pi/
   ```

5. Run verification against the copy, not the damaged mount.

## Check Data Directory Integrity

Run the agent preflight against the copied directory:

```sh
kelp-pi-agent check-data-dir --data-dir /recovery/kelp-pi
```

Verify the hash-chained audit log:

```sh
kelp-pi-agent verify-audit-log --log-file /recovery/kelp-pi/audit/agent.jsonl
```

If audit verification fails, preserve the failed copy unchanged and treat it as
forensic evidence. Start recovery from the newest earlier copy or bundle that still
verifies.

## Recover Keys

Copy both key files together:

```sh
install -d -m 0700 /new/kelp-pi/keys
install -m 0600 /recovery/kelp-pi/keys/pi-ed25519.key.json /new/kelp-pi/keys/
install -m 0644 /recovery/kelp-pi/keys/pi-ed25519.pub.json /new/kelp-pi/keys/
```

Confirm the private key material and public metadata still match:

```sh
kelp-pi-agent keygen --key-dir /new/kelp-pi/keys --label recovery-check
```

If the Pi was powered on when captured or the key file may have been copied by an
attacker, revoke the old key ID on the control plane and bootstrap a fresh key. Do
not reuse a suspect key for new bundles.

## Replay The Last Good Bundle

Find the newest staged bundle:

```sh
find /recovery/kelp-pi/bundles -maxdepth 2 -type f -name manifest.json -print
```

Copy that whole bundle directory to a laptop and verify it:

```sh
kelp-claw verify-audit-bundle /path/to/audit-bundle
```

If the control plane already has a copy of the bundle, prefer that copy and compare
its manifest hashes against the recovered local copy. `bundle.fetch` is still a TODO
in `docs/pi-todo.md`; until it exists, recovery uses the staged bundle directory or
the control-plane artifact store.

## Rebuild A Clean Data Directory

Create a fresh directory with required children:

```sh
install -d -m 0750 /new/kelp-pi
for dir in corpus evidence bundles index audit keys policy scope; do
  install -d -m 0750 "/new/kelp-pi/$dir"
done
```

Restore only the minimum trusted state:

- `keys/` when the key is not suspected compromised.
- `policy/current-policy.json` only if it came from a trusted signed control-plane
  push.
- `scope/` only if the engagement scope is still active and signed.
- `bundles/` for verified completed bundles.
- `audit/agent.jsonl` only as recovered history; do not append to a damaged chain.

Run:

```sh
kelp-pi-agent check-data-dir --data-dir /new/kelp-pi
kelp-pi-agent verify-audit-log --log-file /new/kelp-pi/audit/agent.jsonl
```

If either command fails, keep the recovered copy read-only and re-bootstrap instead
of repairing records in place.

## Re-Bootstrap

Re-bootstrap when key custody is uncertain, the audit chain is unrecoverable, or the
data directory cannot pass preflight.

1. Reflash the pinned Pi OS image.
2. Recreate `/var/lib/kelp-pi` and required child directories.
3. Run `kelp-pi-agent keygen --key-dir /var/lib/kelp-pi/keys --label <device-id>`.
4. Enroll the new public key on the control plane.
5. Mark the old key revoked before accepting new bundles from the replacement Pi.
