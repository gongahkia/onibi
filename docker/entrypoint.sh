#!/bin/sh
set -eu

# Docker creates named volumes as root. Repair only the application's writable
# data mount, then run the application with its dedicated unprivileged user.
if [ "$(id -u)" = 0 ]; then
  chown --recursive --no-dereference kaypoh:kaypoh /var/lib/kaypoh
  exec setpriv --reuid=kaypoh --regid=kaypoh --init-groups /usr/local/bin/kaypoh "$@"
fi

exec /usr/local/bin/kaypoh "$@"
