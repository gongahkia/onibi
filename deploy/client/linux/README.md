# Linux terminal package

Build and validate a native Debian package on Linux:

```text
deploy/client/linux/package-test.sh
```

The builder uses locked dependencies, supports native `aarch64-unknown-linux-gnu` and `x86_64-unknown-linux-gnu` targets, installs `yeokcham` at `/usr/bin/yeokcham`, sets root-owned package payload metadata, and refuses relative or existing output paths.

On macOS, validate the Linux package in the official Rust Debian image:

```text
docker run --rm -e CARGO_TARGET_DIR=/tmp/target -v "$PWD:/workspace" -w /workspace rust:1.93.0-bookworm deploy/client/linux/package-test.sh
```

The `.deb` is an archive, not a release trust statement. Publish it only through a repository with signed `Release` metadata and a release-owned signing key.
