#!/usr/bin/env sh
set -eu

[ "${1:-}" = "--" ] && shift
root="${1:-/}"
case "$root" in
  /) prefix="" ;;
  *) prefix="$root" ;;
esac

nuclei_version="v3.10.0"
nuclei_asset="nuclei_3.10.0_linux_arm64.zip"
nuclei_asset_sha256="b0ddb1f0cc894b7fa79e45043d00a5ffd2cc9fc15e169bf567d1a384eae51427"
nuclei_binary_sha256="579859c6192abd8204ec22ab88e39de8f138d955c283ed99a59fdb3cea451803"
nuclei_templates_revision="cce82b61d26bed35074cd57bc9d0aebd703a81d3"
zap_image="${KELP_PI_ZAP_IMAGE:-ghcr.io/zaproxy/zaproxy:stable}"
zap_image_digest="${KELP_PI_ZAP_IMAGE_DIGEST:-}"

install -d "$prefix/etc/kelp-pi"
cat > "$prefix/etc/kelp-pi/scanners.json" <<JSON
{
  "schemaVersion": "kelp.pi.scanners.v1",
  "nuclei": {
    "source": "github-release",
    "repo": "projectdiscovery/nuclei",
    "version": "$nuclei_version",
    "asset": "$nuclei_asset",
    "asset_sha256": "$nuclei_asset_sha256",
    "binary_sha256": "$nuclei_binary_sha256",
    "path": "/opt/kelp-pi/bin/nuclei",
    "templates_revision": "$nuclei_templates_revision"
  },
  "nmap": {
    "source": "apt",
    "package": "nmap",
    "path": "/usr/bin/nmap",
    "hash_policy": "Debian/Raspberry Pi OS repository signatures plus runtime version capture"
  },
  "zap": {
    "source": "container",
    "image": "$zap_image",
    "image_digest": "$zap_image_digest",
    "enabled_by_default": false,
    "hash_policy": "set KELP_PI_ZAP_IMAGE_DIGEST to an immutable digest before enabling ZAP"
  }
}
JSON
printf '%s\n' "$prefix/etc/kelp-pi/scanners.json"
