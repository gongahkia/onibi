#!/bin/zsh

emulate -L zsh
setopt errexit nounset pipefail

function usage() {
    print -r -- 'Usage: deploy/client/macos/package.zsh --output /absolute/path/yeokcham.pkg [--target <aarch64-apple-darwin|x86_64-apple-darwin>] [--application-identity "Developer ID Application: …"] [--installer-identity "Developer ID Installer: …"]'
}

function die() {
    print -u2 -r -- "package error: $1"
    exit 1
}

function package_payload_is_valid() {
    local payload_path
    local found=0
    while IFS= read -r payload_path; do
        if [[ "$payload_path" == ./usr/local/bin/yeokcham ]]; then
            found=1
        fi
    done < <(pkgutil --payload-files "$1")
    (( found == 1 ))
}

output=''
target=''
application_identity=''
installer_identity=''

while (( $# > 0 )); do
    case "$1" in
        --output)
            (( $# >= 2 )) || die '--output requires a value'
            output="$2"
            shift 2
            ;;
        --target)
            (( $# >= 2 )) || die '--target requires a value'
            target="$2"
            shift 2
            ;;
        --application-identity)
            (( $# >= 2 )) || die '--application-identity requires a value'
            application_identity="$2"
            shift 2
            ;;
        --installer-identity)
            (( $# >= 2 )) || die '--installer-identity requires a value'
            installer_identity="$2"
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

[[ "$(uname -s)" == Darwin ]] || die 'macOS is required'
[[ -n "$output" ]] || die '--output is required'
[[ "$output" == /* ]] || die '--output must be absolute'
[[ ! -e "$output" ]] || die '--output must not already exist'
[[ -d "${output:h}" ]] || die '--output parent directory must exist'

if [[ -n "$application_identity" || -n "$installer_identity" ]]; then
    [[ -n "$application_identity" && -n "$installer_identity" ]] || die 'both Developer ID identities are required for a signed package'
    [[ "$application_identity" == Developer\ ID\ Application:\ * ]] || die '--application-identity must be a Developer ID Application identity'
    [[ "$installer_identity" == Developer\ ID\ Installer:\ * ]] || die '--installer-identity must be a Developer ID Installer identity'
fi

script_directory=${0:A:h}
repository_root=${script_directory:h:h:h}
[[ -f "$repository_root/Cargo.toml" ]] || die 'repository root could not be located'

if [[ -z "$target" ]]; then
    target="$(rustc -vV | awk '$1 == "host:" { host = $2 } END { print host }')"
fi
case "$target" in
    aarch64-apple-darwin|x86_64-apple-darwin) ;;
    *) die '--target must be a supported macOS Rust target' ;;
esac

temporary_directory="$(mktemp -d "${TMPDIR:-/tmp}/yeokcham-macos-package.XXXXXX")"
function cleanup() {
    rm -rf -- "$temporary_directory"
}
trap cleanup EXIT INT TERM

cd "$repository_root"
cargo build --release --locked --package yeokcham-cli --bin yeokcham --target "$target"

binary="$repository_root/target/$target/release/yeokcham"
[[ -f "$binary" && -x "$binary" ]] || die 'release binary was not produced'
version="$("$binary" --version | awk 'NR == 1 { package_version = $2 } END { print package_version }')"
[[ "$version" == <->.<->.<-> ]] || die 'binary version must be numeric semantic versioning'

payload_root="$temporary_directory/root"
packaged_binary="$payload_root/usr/local/bin/yeokcham"
mkdir -p "$payload_root/usr/local/bin"
install -m 0755 "$binary" "$packaged_binary"

if [[ -n "$application_identity" ]]; then
    codesign --force --options runtime --timestamp --sign "$application_identity" "$packaged_binary"
    codesign --verify --strict --verbose=2 "$packaged_binary"
fi

package="$temporary_directory/yeokcham-$version-$target.pkg"
typeset -a package_arguments
package_arguments=(
    --root "$payload_root"
    --identifier com.yeokcham.cli
    --version "$version"
    --install-location /
    --ownership recommended
)
if [[ -n "$installer_identity" ]]; then
    package_arguments+=(--sign "$installer_identity" --timestamp)
fi
pkgbuild "${package_arguments[@]}" "$package"
package_payload_is_valid "$package" || die 'package payload is invalid'
if [[ -n "$installer_identity" ]]; then
    pkgutil --check-signature "$package"
fi
mv "$package" "$output"
print -r -- "$output"
