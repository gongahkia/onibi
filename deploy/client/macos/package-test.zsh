#!/bin/zsh

emulate -L zsh
setopt errexit nounset pipefail

function die() {
    print -u2 -r -- "package test error: $1"
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

script_directory=${0:A:h}
package_script="$script_directory/package.zsh"
temporary_directory="$(mktemp -d "${TMPDIR:-/tmp}/yeokcham-macos-package-test.XXXXXX")"
function cleanup() {
    rm -rf -- "$temporary_directory"
}
trap cleanup EXIT INT TERM

if "$package_script" --output relative.pkg >/dev/null 2>&1; then
    die 'relative output was accepted'
fi

if "$package_script" --output "$temporary_directory/signed.pkg" --application-identity 'Developer ID Application: Test (TEST)' >/dev/null 2>&1; then
    die 'partial signing configuration was accepted'
fi

package="$temporary_directory/yeokcham.pkg"
"$package_script" --output "$package"
[[ -s "$package" ]] || die 'package was not created'
package_payload_is_valid "$package" || die 'package payload is invalid'

if "$package_script" --output "$package" >/dev/null 2>&1; then
    die 'existing output was accepted'
fi
