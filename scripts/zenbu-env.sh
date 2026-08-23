#!/bin/sh
# Print the environment needed for direct Dune commands after make bootstrap.
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
exec "$script_dir/wasmtime_platform.sh" env
