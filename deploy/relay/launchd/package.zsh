#!/bin/zsh

emulate -L zsh
setopt errexit nounset pipefail

function die() { print -u2 -r -- "package error: $1"; exit 1 }
function usage() { print -r -- 'Usage: deploy/relay/launchd/package.zsh --output /absolute/path/yeokcham-relay.pkg [--target <aarch64-apple-darwin|x86_64-apple-darwin>] [--application-identity "Developer ID Application: …"] [--installer-identity "Developer ID Installer: …"]' }
function package_payload_is_valid() {
    local payload_path binary=0 config=0 plist=0
    while IFS= read -r payload_path; do
        case "$payload_path" in
            ./usr/local/libexec/yeokcham-relay) binary=1 ;;
            './Library/Application Support/Yeokcham/relay.conf.example') config=1 ;;
            ./Library/LaunchDaemons/com.yeokcham.relay.plist) plist=1 ;;
        esac
    done < <(pkgutil --payload-files "$1")
    (( binary == 1 && config == 1 && plist == 1 ))
}

output=''
target=''
application_identity=''
installer_identity=''
while (( $# > 0 )); do
    case "$1" in
        --output) (( $# >= 2 )) || die '--output requires a value'; output="$2"; shift 2 ;;
        --target) (( $# >= 2 )) || die '--target requires a value'; target="$2"; shift 2 ;;
        --application-identity) (( $# >= 2 )) || die '--application-identity requires a value'; application_identity="$2"; shift 2 ;;
        --installer-identity) (( $# >= 2 )) || die '--installer-identity requires a value'; installer_identity="$2"; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        *) die "unknown argument: $1" ;;
    esac
done
[[ "$(uname -s)" == Darwin ]] || die 'macOS is required'
[[ -n "$output" ]] || die '--output is required'
[[ "$output" == /* ]] || die '--output must be absolute'
[[ "${output:e}" == pkg ]] || die '--output must end in .pkg'
[[ ! -e "$output" ]] || die '--output must not already exist'
[[ -d "${output:h}" ]] || die '--output parent directory must exist'
command -v pkgbuild >/dev/null 2>&1 || die 'pkgbuild is required'
command -v pkgutil >/dev/null 2>&1 || die 'pkgutil is required'
command -v plutil >/dev/null 2>&1 || die 'plutil is required'
if [[ -n "$application_identity" || -n "$installer_identity" ]]; then
    [[ -n "$application_identity" && -n "$installer_identity" ]] || die 'both Developer ID identities are required for a signed package'
    [[ "$application_identity" == Developer\ ID\ Application:\ * ]] || die '--application-identity must be a Developer ID Application identity'
    [[ "$installer_identity" == Developer\ ID\ Installer:\ * ]] || die '--installer-identity must be a Developer ID Installer identity'
fi
script_directory=${0:A:h}
repository_root=${script_directory:h:h:h}
[[ -f "$repository_root/Cargo.toml" ]] || die 'repository root could not be located'
plutil -lint "$script_directory/com.yeokcham.relay.plist" >/dev/null
zsh -n "$script_directory/scripts/postinstall"
if [[ -z "$target" ]]; then
    target="$(rustc -vV | awk '$1 == "host:" { host = $2 } END { print host }')"
fi
case "$target" in aarch64-apple-darwin|x86_64-apple-darwin) ;; *) die '--target must be a supported macOS Rust target' ;; esac
temporary_directory="$(mktemp -d "${TMPDIR:-/tmp}/yeokcham-launchd-package.XXXXXX")"
function cleanup() { rm -rf -- "$temporary_directory" }
trap cleanup EXIT INT TERM
cd "$repository_root"
cargo build --release --locked --package yeokcham-relay --bin yeokcham-relay --target "$target"
case "${CARGO_TARGET_DIR:-}" in '') target_directory="$repository_root/target" ;; /*) target_directory="$CARGO_TARGET_DIR" ;; *) target_directory="$repository_root/$CARGO_TARGET_DIR" ;; esac
binary="$target_directory/$target/release/yeokcham-relay"
[[ -f "$binary" && -x "$binary" ]] || die 'release binary was not produced'
version="$("$binary" --version | awk 'NR == 1 { print $2 }')"
[[ "$version" == <->.<->.<-> ]] || die 'binary version must be numeric semantic versioning'
payload_root="$temporary_directory/root"
packaged_binary="$payload_root/usr/local/libexec/yeokcham-relay"
mkdir -p "$payload_root/usr/local/libexec" "$payload_root/Library/Application Support/Yeokcham" "$payload_root/Library/LaunchDaemons"
install -m 0755 "$binary" "$packaged_binary"
install -m 0644 "$script_directory/relay.conf" "$payload_root/Library/Application Support/Yeokcham/relay.conf.example"
install -m 0644 "$script_directory/com.yeokcham.relay.plist" "$payload_root/Library/LaunchDaemons/com.yeokcham.relay.plist"
if [[ -n "$application_identity" ]]; then
    codesign --force --options runtime --timestamp --sign "$application_identity" "$packaged_binary"
    codesign --verify --strict --verbose=2 "$packaged_binary"
fi
package="$temporary_directory/yeokcham-relay-$version-$target.pkg"
typeset -a arguments
arguments=(--root "$payload_root" --scripts "$script_directory/scripts" --identifier com.yeokcham.relay --version "$version" --install-location / --ownership recommended)
[[ -n "$installer_identity" ]] && arguments+=(--sign "$installer_identity" --timestamp)
pkgbuild "${arguments[@]}" "$package"
package_payload_is_valid "$package" || die 'package payload is invalid'
expanded="$temporary_directory/expanded"
pkgutil --expand-full "$package" "$expanded"
cmp "$expanded/Scripts/postinstall" "$script_directory/scripts/postinstall" >/dev/null || die 'package postinstall script is invalid'
[[ -z "$installer_identity" ]] || pkgutil --check-signature "$package"
mv "$package" "$output"
print -r -- "$output"
