# Release

Onibi is release-ready when local artifacts, checksums, and install paths pass
without live credentials. Apple notarization remains gated on Apple Developer
secrets.

## Local Snapshot

```sh
go test -race -count=1 ./...
go vet ./...
staticcheck ./...
make build
goreleaser release --snapshot --clean
```

Expected artifacts:

- `dist/checksums.txt`
- `dist/onibi_*_darwin_*`
- `dist/onibi_*_linux_*`

## Homebrew Tap

Use `packaging/homebrew/onibi.rb.template` as the tap formula source. Replace:

- `{{VERSION}}`
- `{{URL}}`
- `{{SHA256}}`

Then test from the tap checkout:

```sh
brew install --build-from-source ./Formula/onibi.rb
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

No release notes may claim notarization passed unless the tagged release was
built with those values and verified on a clean Mac.
