# launchd relay package

Build and validate an unsigned local package:

```text
deploy/relay/launchd/package-test.zsh
```

The package installs the relay binary, launchd plist, and a configuration example. Its postinstall creates a dedicated `_arachne` account and copies `relay.conf.example` to `relay.conf` only if no configuration exists. It never starts the service or generates identity material.

Provision and load the service explicitly:

```text
sudo /usr/local/libexec/arachne-relay generate-identity --output '/Library/Application Support/Arachne/relay.identity'
sudo chown root:_arachne '/Library/Application Support/Arachne/relay.identity'
sudo chmod 0440 '/Library/Application Support/Arachne/relay.identity'
sudo launchctl bootstrap system /Library/LaunchDaemons/com.arachne.relay.plist
```

Edit `/Library/Application Support/Arachne/relay.conf` before bootstrapping; upgrades preserve it. Inspect with `sudo launchctl print system/com.arachne.relay`; stop with `sudo launchctl bootout system/com.arachne.relay`.

For distribution, sign both binary and installer:

```text
deploy/relay/launchd/package.zsh --output /absolute/path/arachne-relay.pkg --application-identity 'Developer ID Application: Team Name (TEAMID)' --installer-identity 'Developer ID Installer: Team Name (TEAMID)'
```

Unsigned packages are local-only. A release requires Developer ID identities, notarization, stapling, and install assessment:

```text
xcrun notarytool submit /absolute/path/arachne-relay.pkg --keychain-profile <profile> --wait
xcrun stapler staple /absolute/path/arachne-relay.pkg
pkgutil --check-signature /absolute/path/arachne-relay.pkg
spctl -a -vv -t install /absolute/path/arachne-relay.pkg
```
