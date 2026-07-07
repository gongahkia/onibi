# Kelp Pi Network Hardening

Network/AP hardening is opt-in because it can disconnect a headless SSH session.
Read `docs/pi-recovery.md` before applying it on a field unit.

Render files for review:

```sh
scripts/apply-kelp-pi-network-hardening.sh --render-dir .kelp-pi/network-preview
```

Apply nftables/sysctl/dnsmasq/NetworkManager files only after recovery media or
console access exists:

```sh
sudo scripts/apply-kelp-pi-network-hardening.sh \
  --apply \
  --i-understand-ssh-risk \
  --ap-passphrase '<field-secret>' \
  --control-plane-host 203.0.113.10
```

The generated nftables input chain keeps TCP/22 open for SSH recovery. The script
does not activate the AP profile unless `--activate-ap` is also passed; activation
requires `--ap-passphrase`.

Recovery helper after an apply:

```sh
sudo /usr/local/sbin/kelp-pi-recover-network.sh
```

Then rerun the integrity checks from `docs/pi-recovery.md`.
