# Install Onibi on macOS

The release publishes signed-but-not-notarized DMG artifacts for Apple Silicon and Intel macOS. The Homebrew formula is kept in `packaging/homebrew/onibi.rb` and mirrored to `gongahkia/homebrew-onibi` for each release.

```sh
brew tap gongahkia/onibi
brew install onibi
onibi setup
onibi status
```

The formula installs `Onibi.app` and exposes the CLI as `onibi`.

For headless use without opening the app:

```sh
onibi --headless --auto-transports
```

Pair a phone by running:

```sh
onibi setup
```

Check for desktop updates from Onibi's Command Palette or Settings -> Updates.
Headless CLI installs can also check GitHub Releases:

```sh
onibi update check
```
