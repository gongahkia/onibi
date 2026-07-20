#!/bin/sh

set -eu

die() {
    printf '%s\n' "package test error: $1" >&2
    exit 1
}

script_directory=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
package_script="$script_directory/package.sh"
command -v systemd-analyze >/dev/null 2>&1 || die 'systemd-analyze is required'
temporary_directory=$(mktemp -d "${TMPDIR:-/tmp}/yeokcham-systemd-package-test.XXXXXX")
cleanup() {
    if [ -n "${relay_pid:-}" ]; then
        kill -TERM "$relay_pid" >/dev/null 2>&1 || true
        wait "$relay_pid" >/dev/null 2>&1 || true
    fi
    rm -rf -- "$temporary_directory"
}
trap cleanup EXIT HUP INT TERM

if "$package_script" --output relative.deb >/dev/null 2>&1; then
    die 'relative output was accepted'
fi

if "$package_script" --output "$temporary_directory/invalid.deb" --target invalid-target >/dev/null 2>&1; then
    die 'invalid target was accepted'
fi

package="$temporary_directory/yeokcham-relay.deb"
"$package_script" --output "$package"
[ -s "$package" ] || die 'package was not created'
[ "$(dpkg-deb -f "$package" Package)" = yeokcham-relay ] || die 'package name is invalid'
[ "$(dpkg-deb -f "$package" Depends)" = 'systemd (>= 247)' ] || die 'package dependency is invalid'

package_root="$temporary_directory/root"
dpkg-deb -x "$package" "$package_root"
[ -x "$package_root/usr/bin/yeokcham-relay" ] || die 'relay binary is missing'
cmp "$package_root/etc/yeokcham/relay.conf" "$script_directory/../relay.conf.example" >/dev/null || die 'relay config is invalid'
cmp "$package_root/lib/systemd/system/yeokcham-relay.service" "$script_directory/yeokcham-relay.service" >/dev/null || die 'systemd unit is invalid'
[ -d /lib/systemd/system ] || die '/lib/systemd/system is required'
cp -a /lib/systemd/system/. "$package_root/lib/systemd/system/"
systemd-analyze verify --root="$package_root" yeokcham-relay.service >/dev/null

identity="$temporary_directory/relay.identity"
relay_binary="$package_root/usr/bin/yeokcham-relay"
"$relay_binary" generate-identity --output "$identity"
[ "$(stat -c '%a' "$identity")" = 400 ] || die 'generated identity is not read-only'
relay_port=$((20000 + ($$ % 10000)))
health_port=$((relay_port + 1))
metrics_port=$((relay_port + 2))
runtime_config="$temporary_directory/relay.conf"
sed "s|^listen_address = .*|listen_address = \"127.0.0.1:$relay_port\"|;s|^database_path = .*|database_path = \"$temporary_directory/relay.sqlite\"|" "$package_root/etc/yeokcham/relay.conf" > "$runtime_config"
"$relay_binary" run --config "$runtime_config" --identity "$identity" --health-address "127.0.0.1:$health_port" --metrics-address "127.0.0.1:$metrics_port" >"$temporary_directory/relay.log" 2>&1 &
relay_pid=$!
attempt=0
until "$relay_binary" healthcheck --address "127.0.0.1:$health_port" >/dev/null 2>&1; do
    attempt=$((attempt + 1))
    [ "$attempt" -lt 20 ] || die 'packaged relay did not become healthy'
    kill -0 "$relay_pid" >/dev/null 2>&1 || die 'packaged relay exited before becoming healthy'
    sleep 1
done
kill -TERM "$relay_pid"
wait "$relay_pid"
relay_pid=''

if "$package_script" --output "$package" >/dev/null 2>&1; then
    die 'existing output was accepted'
fi
