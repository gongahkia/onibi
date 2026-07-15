# Release

Onibi is release-ready when local artifacts, checksums, install paths, signing,
notarization, and Homebrew install pass. Public macOS release tags must fail if
Apple signing secrets are unavailable.

## Local Snapshot

Install local release prerequisites first:

```sh
brew install goreleaser syft cosign
```

CI already installs `syft` and `cosign` before GoReleaser release jobs.

```sh
go test -race -count=1 ./...
go vet ./...
staticcheck ./...
make build
./bin/onibi update-check --repo .
./bin/onibi doctor --after-upgrade --offline
./bin/onibi doctor --release --offline
scripts/macos-release-gate.sh
make upgrade-recovery-gate
goreleaser release --snapshot --clean
scripts/release-smoke.sh dist
scripts/reproducible-build.sh
```

The macOS and upgrade-recovery gates are release-blocking. The upgrade-recovery
gate writes `metadata.json`, `test.log`, and `summary.json` to
`artifacts/upgrade-recovery-gate` for CI upload. Linux is beta-only; run
[`linux-beta.md`](./linux-beta.md) separately and do not substitute Linux
evidence for the macOS gate. The real-machine install walkthrough is
[`fresh-machine-smoke.md`](./fresh-machine-smoke.md). Run it on a stock macOS
14+ user and a stock Ubuntu 24.04 VM before closing release-readiness issues.

CI runs the `reproducible-build` job on every push and pull request. It installs
GoReleaser, runs two fixed-input `goreleaser build --snapshot --single-target`
passes from the same commit, and fails with the differing artifact path and both
SHA256 values if any binary differs.

Expected artifacts:

- `dist/checksums.txt`
- `dist/checksums.txt.sig` on signed releases
- `dist/checksums.txt.sigstore.json` on signed releases
- `dist/*.tar.gz.sigstore.json` on signed releases
- `multiple.intoto.jsonl` on tagged releases after SLSA provenance upload
- `dist/*.sbom.*`
- `dist/onibi_*_darwin_*`
- `dist/onibi_*_linux_*`

## Homebrew Tap

GoReleaser publishes a cask to `gongahkia/homebrew-onibi` when
`HOMEBREW_TAP_TOKEN` is set. Test from the tap checkout:

```sh
brew install --cask ./Casks/onibi.rb
onibi version
onibi doctor --mode preflight --offline
onibi doctor --after-upgrade --offline
onibi hooks --show --all --json >/tmp/onibi-hooks.json
```

## Notarization

GoReleaser is configured to sign/notarize only when these env vars are set:

- `MACOS_SIGN_P12`
- `MACOS_SIGN_PASSWORD`
- `MACOS_NOTARY_ISSUER_ID`
- `MACOS_NOTARY_KEY_ID`
- `MACOS_NOTARY_KEY`
- `GPG_PRIVATE_KEY`
- `GPG_PASSPHRASE`

Release artifacts include SBOMs and SHA256 checksums. Tagged public releases
sign `checksums.txt` with the imported GPG key.
Tagged public releases also sign `checksums.txt` and each release archive with
keyless cosign through the GitHub Actions OIDC identity for
`.github/workflows/release.yml`. GoReleaser publishes those bundles as
`*.sigstore.json` assets. There is no `docs/cosign.pub` for this mode; verify the
bundle against the workflow identity and GitHub OIDC issuer instead.
The release workflow also passes GoReleaser artifact hashes to the SLSA generic
generator on tag pushes. The generator signs provenance with GitHub OIDC and
uploads it to the same GitHub release.

The release workflow exports the imported GPG public key and GoReleaser embeds
it into the `onibi` binary through `buildinfo.ReleasePublicKeyB64`. `onibi
update` refuses to apply a release unless `checksums.txt.sig` verifies with that
embedded key, the selected archive hash matches `checksums.txt`, and, on macOS,
the extracted binary passes `codesign --verify`.

`onibi update-check` prints release-archive upgrade commands that always verify
the selected tarball against `checksums.txt` before install. If
`ONIBI_RELEASE_GPG_KEY` contains the public signing key and `gpg` is available,
the same command also verifies `checksums.txt.sig` before extracting binaries.
The machine-readable `onibi update-check --json` contract is documented in
[`docs/update-check-schema.md`](./update-check-schema.md).

`scripts/install.sh` is the curl installer template. Publish it only after
replacing `__ONIBI_RELEASE_GPG_KEY_B64__` with the same base64 public key used by
the release workflow. The unrendered template fails closed instead of installing
without signature verification.
Use `scripts/prepare-install-pages.sh <pages-dir>` to render the dedicated
`get.onibi.sh` Pages payload with `index.html`, `CNAME`, and `.nojekyll`.

No release notes may claim notarization passed unless the tagged release was
built with those values and verified on a clean Mac using
`scripts/manual-e2e-release.md`.
