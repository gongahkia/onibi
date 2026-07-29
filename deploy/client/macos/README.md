# macOS terminal package

Build and validate an unsigned local package:

```text
deploy/client/macos/package-test.zsh
```

The package installs `arachne` at `/usr/local/bin/arachne`, is built with the locked Rust dependencies, accepts only the supported Apple Silicon or Intel macOS targets, and refuses relative or existing output paths.

For a release package, supply both a `Developer ID Application` identity for hardened-runtime binary signing and a `Developer ID Installer` identity for installer signing:

```text
deploy/client/macos/package.zsh --output /absolute/path/arachne.pkg --application-identity 'Developer ID Application: Team Name (TEAMID)' --installer-identity 'Developer ID Installer: Team Name (TEAMID)'
```

Unsigned packages are local-only artifacts. A distributable package requires Developer ID identities, notarization with a release-owned `notarytool` keychain profile, and stapling before release:

```text
xcrun notarytool submit /absolute/path/arachne.pkg --keychain-profile <profile> --wait
xcrun stapler staple /absolute/path/arachne.pkg
pkgutil --check-signature /absolute/path/arachne.pkg
spctl -a -vv -t install /absolute/path/arachne.pkg
```
