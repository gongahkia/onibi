# TODO

## Raspberry Pi Hardware Verification

- [ ] Run fresh Raspberry Pi 5 4GB / 56GB+ storage acceptance from Raspberry Pi OS Lite 64-bit → success:
  - `curl -fsSL https://raw.githubusercontent.com/gongahkia/kelp/main/scripts/install-kelp-pi.sh | sudo sh -s -- --build-from-source` completes on real hardware.
  - `kelp-pi` reports the 4GB profile, service status, storage, Nuclei status, network-hardening status, and model catalog.
  - `kelp-pi doctor` exits 0.
  - `kelp-pi models install qwen2.5:0.5b` installs Ollama if missing and pulls the model.
  - `kelp-pi models local` lists `qwen2.5:0.5b`.
  - `sudo kelp-pi validate-node` exits 0 after network hardening is rendered/applied with a recovery path available.
  - Record exact Pi model, RAM, storage free space, OS release, installer duration, and any failed command output.
