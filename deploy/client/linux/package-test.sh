#!/bin/sh

set -eu

die() {
    printf '%s\n' "package test error: $1" >&2
    exit 1
}

package_payload_is_valid() {
    package_name=$(dpkg-deb -f "$1" Package)
    package_architecture=$(dpkg-deb -f "$1" Architecture)
    package_contents=$(dpkg-deb --contents "$1")
    [ "$package_name" = arachne ] && [ "$package_architecture" = "$2" ] && printf '%s\n' "$package_contents" | grep -F ' ./usr/bin/arachne' >/dev/null
}

script_directory=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
package_script="$script_directory/package.sh"
temporary_directory=$(mktemp -d "${TMPDIR:-/tmp}/arachne-linux-package-test.XXXXXX")
cleanup() {
    rm -rf -- "$temporary_directory"
}
trap cleanup EXIT HUP INT TERM

if "$package_script" --output relative.deb >/dev/null 2>&1; then
    die 'relative output was accepted'
fi

if "$package_script" --output "$temporary_directory/invalid.deb" --target invalid-target >/dev/null 2>&1; then
    die 'unsupported target was accepted'
fi

package="$temporary_directory/arachne.deb"
"$package_script" --output "$package"
[ -s "$package" ] || die 'package was not created'
architecture=$(dpkg-deb -f "$package" Architecture)
package_payload_is_valid "$package" "$architecture" || die 'package payload is invalid'

if "$package_script" --output "$package" >/dev/null 2>&1; then
    die 'existing output was accepted'
fi
