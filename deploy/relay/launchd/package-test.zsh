#!/bin/zsh

emulate -L zsh
setopt errexit nounset pipefail

function die() { print -u2 -r -- "package test error: $1"; exit 1 }
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
script_directory=${0:A:h}
package_script="$script_directory/package.zsh"
plist="$script_directory/com.yeokcham.relay.plist"
postinstall="$script_directory/scripts/postinstall"
temporary_directory="$(mktemp -d "${TMPDIR:-/tmp}/yeokcham-launchd-package-test.XXXXXX")"
function cleanup() { rm -rf -- "$temporary_directory" }
trap cleanup EXIT INT TERM
zsh -n "$package_script"
zsh -n "$postinstall"
plutil -lint "$plist" >/dev/null
[[ "$(/usr/libexec/PlistBuddy -c 'Print :Label' "$plist")" == com.yeokcham.relay ]] || die 'launchd label is invalid'
[[ "$(/usr/libexec/PlistBuddy -c 'Print :ProgramArguments:0' "$plist")" == /usr/local/libexec/yeokcham-relay ]] || die 'launchd binary path is invalid'
[[ "$(/usr/libexec/PlistBuddy -c 'Print :UserName' "$plist")" == _yeokcham ]] || die 'launchd user is invalid'
[[ "$(/usr/libexec/PlistBuddy -c 'Print :GroupName' "$plist")" == _yeokcham ]] || die 'launchd group is invalid'
if grep -Eq 'launchctl|generate-identity' "$postinstall"; then die 'postinstall must not start the service or generate identity material'; fi
if "$package_script" --output relative.pkg >/dev/null 2>&1; then die 'relative output was accepted'; fi
if "$package_script" --output "$temporary_directory/signed.pkg" --application-identity 'Developer ID Application: Test (TEST)' >/dev/null 2>&1; then die 'partial signing configuration was accepted'; fi
package="$temporary_directory/yeokcham-relay.pkg"
"$package_script" --output "$package"
[[ -s "$package" ]] || die 'package was not created'
package_payload_is_valid "$package" || die 'package payload is invalid'
expanded="$temporary_directory/expanded"
pkgutil --expand-full "$package" "$expanded"
cmp "$expanded/Scripts/postinstall" "$postinstall" >/dev/null || die 'package postinstall script is invalid'
if "$package_script" --output "$package" >/dev/null 2>&1; then die 'existing output was accepted'; fi
