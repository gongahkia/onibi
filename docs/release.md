# Release

Onibi is release-ready when local artifacts, checksums, install paths, signing,
notarization, and Homebrew install pass. Public macOS release tags must fail if
Apple signing secrets are unavailable.

## Local Snapshot

```sh
go test -race -count=1 ./...
go vet ./...
staticcheck ./...
make build
goreleaser release --snapshot --clean
scripts/release-smoke.sh dist
scripts/reproducible-build.sh
```

Expected artifacts:

- `dist/checksums.txt`
- `dist/checksums.txt.sig` on signed releases
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

No release notes may claim notarization passed unless the tagged release was
built with those values and verified on a clean Mac using
`scripts/manual-e2e-release.md`.
