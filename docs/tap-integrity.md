# Homebrew Tap Integrity

`gongahkia/homebrew-onibi` is a personal tap. Treat it as a convenience index over GitHub release assets, not as an independent trust root.

## Trust Inputs

Use these inputs before installing from the tap on a sensitive machine:

- GitHub release tag: `vX.Y.Z`
- release assets: `checksums.txt`, `checksums.txt.sig`, and the selected `onibi_*` archive
- release signing fingerprint: the exact `GPG_FINGERPRINT` printed by the release workflow and copied into the release notes
- tap cask: `gongahkia/homebrew-onibi/Casks/onibi.rb`

This repository does not currently commit the release signing fingerprint. If a release does not publish the fingerprint next to `checksums.txt.sig`, I cannot verify that release signing identity from this repo alone.

## Download Release Assets

```bash
tag=vX.Y.Z
repo=gongahkia/onibi

mkdir -p /tmp/onibi-verify
cd /tmp/onibi-verify

gh release download "$tag" --repo "$repo" --pattern 'checksums.txt*'
gh release view "$tag" --repo "$repo" --json assets --jq '.assets[].name'
gh release download "$tag" --repo "$repo" --pattern 'onibi_*_darwin_arm64.tar.gz'
```

Replace the archive pattern with the exact platform asset you plan to install.

## Verify The GPG Signature

Import the release public key from the release notes, a maintainer-published key URL, or the rendered `get.onibi.sh` installer key material. Then compare its fingerprint out of band:

```bash
gpg --show-keys --fingerprint onibi-release.asc
gpg --import onibi-release.asc
gpg --verify checksums.txt.sig checksums.txt
```

The fingerprint must match the release note value exactly. Do not continue if the key is missing, unpublished, or only identified by a short key ID.

## Verify The Archive Checksum

macOS:

```bash
asset=onibi_vX.Y.Z_darwin_arm64.tar.gz
grep "  $asset$" checksums.txt >checksum.line
shasum -a 256 -c checksum.line
```

Linux:

```bash
asset=onibi_vX.Y.Z_linux_amd64.tar.gz
grep "  $asset$" checksums.txt >checksum.line
sha256sum -c checksum.line
```

## Compare The Tap

```bash
brew tap gongahkia/onibi
tap="$(brew --repo gongahkia/onibi)"
sed -n '/url /p;/sha256 /p' "$tap/Casks/onibi.rb"
```

Confirm:

- the cask URL points at the same GitHub release tag
- the cask asset name matches the verified archive
- the cask `sha256` equals the checksum line verified above

Only then install:

```bash
brew install --cask "$tap/Casks/onibi.rb"
onibi version
onibi doctor --mode preflight --offline
```

## Compromise Model

If the tap is compromised but the GitHub release assets and signing key are not, the signed `checksums.txt` verification should catch a changed cask URL or SHA256.

If the GitHub release, signing key, or maintainer account is compromised, tap checksum verification does not protect you. Use [`scripts/reproducible-build.sh`](../scripts/reproducible-build.sh) and the release checklist in [`docs/release.md`](./release.md) as a separate build reproducibility check.

## Follow-Up

The out-of-tree `gongahkia/homebrew-onibi` README should link back to this document the next time the tap repository is updated.
