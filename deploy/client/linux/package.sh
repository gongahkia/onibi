#!/bin/sh

set -eu

usage() {
    printf '%s\n' 'Usage: deploy/client/linux/package.sh --output /absolute/path/arachne.deb [--target <aarch64-unknown-linux-gnu|x86_64-unknown-linux-gnu>]'
}

die() {
    printf '%s\n' "package error: $1" >&2
    exit 1
}

package_payload_is_valid() {
    package_name=$(dpkg-deb -f "$1" Package)
    package_architecture=$(dpkg-deb -f "$1" Architecture)
    package_contents=$(dpkg-deb --contents "$1")
    [ "$package_name" = arachne ] && [ "$package_architecture" = "$2" ] && printf '%s\n' "$package_contents" | grep -F ' ./usr/bin/arachne' >/dev/null
}

output=''
target=''

while [ "$#" -gt 0 ]; do
    case "$1" in
        --output)
            [ "$#" -ge 2 ] || die '--output requires a value'
            output=$2
            shift 2
            ;;
        --target)
            [ "$#" -ge 2 ] || die '--target requires a value'
            target=$2
            shift 2
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            die "unknown argument: $1"
            ;;
    esac
done

[ "$(uname -s)" = Linux ] || die 'Linux is required'
[ -n "$output" ] || die '--output is required'
case "$output" in
    /*) ;;
    *) die '--output must be absolute' ;;
esac
[ ! -e "$output" ] || die '--output must not already exist'
[ -d "$(dirname -- "$output")" ] || die '--output parent directory must exist'
command -v dpkg-deb >/dev/null 2>&1 || die 'dpkg-deb is required'

script_directory=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
repository_root=$(CDPATH= cd -- "$script_directory/../../.." && pwd -P)
[ -f "$repository_root/Cargo.toml" ] || die 'repository root could not be located'

if [ -z "$target" ]; then
    target=$(rustc -vV | awk '$1 == "host:" { host = $2 } END { print host }')
fi
case "$target" in
    aarch64-unknown-linux-gnu) architecture=arm64 ;;
    x86_64-unknown-linux-gnu) architecture=amd64 ;;
    *) die '--target must be a supported Linux Rust target' ;;
esac

temporary_directory=$(mktemp -d "${TMPDIR:-/tmp}/arachne-linux-package.XXXXXX")
cleanup() {
    rm -rf -- "$temporary_directory"
}
trap cleanup EXIT HUP INT TERM

cd "$repository_root"
cargo build --release --locked --package arachne-cli --bin arachne --target "$target"

case "${CARGO_TARGET_DIR:-}" in
    '') target_directory="$repository_root/target" ;;
    /*) target_directory="$CARGO_TARGET_DIR" ;;
    *) target_directory="$repository_root/$CARGO_TARGET_DIR" ;;
esac
binary="$target_directory/$target/release/arachne"
[ -f "$binary" ] && [ -x "$binary" ] || die 'release binary was not produced'
version=$("$binary" --version | awk 'NR == 1 { package_version = $2 } END { print package_version }')
printf '%s\n' "$version" | grep -Eq '^[0-9]+(\.[0-9]+){2}$' || die 'binary version must be numeric semantic versioning'

payload_root="$temporary_directory/root"
mkdir -p "$payload_root/DEBIAN" "$payload_root/usr/bin"
install -m 0755 "$binary" "$payload_root/usr/bin/arachne"
cat > "$payload_root/DEBIAN/control" <<EOF
Package: arachne
Version: $version
Section: net
Priority: optional
Architecture: $architecture
Maintainer: Arachne
Description: Arachne secure courier terminal client
EOF

package="$temporary_directory/arachne_${version}_${architecture}.deb"
dpkg-deb --root-owner-group --build "$payload_root" "$package"
package_payload_is_valid "$package" "$architecture" || die 'package payload is invalid'
mv "$package" "$output"
printf '%s\n' "$output"
