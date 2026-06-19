#!/usr/bin/env sh
set -eu

[ "${1:-}" = "--" ] && shift
script_dir="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"
repo_root="$(CDPATH= cd -- "$script_dir/../../.." && pwd)"

root="${KELP_PI_IMAGE_ROOT:-}"
if [ $# -gt 0 ] && [ "${1#--}" = "$1" ]; then
  root="$1"
  shift
fi

ssid=""
ap_interface=""
ap_address=""
dhcp_start=""
dhcp_end=""
wpa3_passphrase=""
allow_file="$(mktemp)"
trap 'rm -f "$allow_file"' EXIT

while [ $# -gt 0 ]; do
  case "$1" in
    --ssid)
      [ $# -ge 2 ] || {
        printf '%s\n' "--ssid requires a value" >&2
        exit 64
      }
      ssid="$2"
      shift 2
      ;;
    --ap-interface)
      [ $# -ge 2 ] || {
        printf '%s\n' "--ap-interface requires a value" >&2
        exit 64
      }
      ap_interface="$2"
      shift 2
      ;;
    --ap-address)
      [ $# -ge 2 ] || {
        printf '%s\n' "--ap-address requires a value" >&2
        exit 64
      }
      ap_address="$2"
      shift 2
      ;;
    --dhcp-start)
      [ $# -ge 2 ] || {
        printf '%s\n' "--dhcp-start requires a value" >&2
        exit 64
      }
      dhcp_start="$2"
      shift 2
      ;;
    --dhcp-end)
      [ $# -ge 2 ] || {
        printf '%s\n' "--dhcp-end requires a value" >&2
        exit 64
      }
      dhcp_end="$2"
      shift 2
      ;;
    --wpa3-passphrase)
      [ $# -ge 2 ] || {
        printf '%s\n' "--wpa3-passphrase requires a value" >&2
        exit 64
      }
      wpa3_passphrase="$2"
      shift 2
      ;;
    --allow-outbound)
      [ $# -ge 2 ] || {
        printf '%s\n' "--allow-outbound requires a value" >&2
        exit 64
      }
      printf '%s\n' "$2" >> "$allow_file"
      shift 2
      ;;
    *)
      printf 'unknown argument: %s\n' "$1" >&2
      exit 64
      ;;
  esac
done

[ -n "$root" ] || {
  printf 'usage: %s IMAGE_ROOT --wpa3-passphrase PASS [--allow-outbound HOST:PORT...]\n' "$0" >&2
  exit 64
}

[ -n "$wpa3_passphrase" ] || {
  printf '%s\n' "stage-image-root requires --wpa3-passphrase" >&2
  exit 64
}

set -- --output "$root" --wpa3-passphrase "$wpa3_passphrase"
[ -z "$ssid" ] || set -- "$@" --ssid "$ssid"
[ -z "$ap_interface" ] || set -- "$@" --ap-interface "$ap_interface"
[ -z "$ap_address" ] || set -- "$@" --ap-address "$ap_address"
[ -z "$dhcp_start" ] || set -- "$@" --dhcp-start "$dhcp_start"
[ -z "$dhcp_end" ] || set -- "$@" --dhcp-end "$dhcp_end"
while IFS= read -r endpoint; do
  [ -z "$endpoint" ] || set -- "$@" --allow-outbound "$endpoint"
done < "$allow_file"

"$script_dir/install-agent-binary.sh" "$root"
"$script_dir/install-systemd.sh" "$root"
cargo run --manifest-path "$repo_root/packages/pi-agent/Cargo.toml" --quiet -- hardening render-network "$@"
"$script_dir/apply-boot-fragment.sh" "$root"
"$script_dir/fetch-nuclei-arm64.sh" "$root"
