#!/usr/bin/env sh
set -eu

repo="${ONIBI_REPO:-gongahkia/onibi}"
version="${ONIBI_VERSION:-latest}"
install_dir="${ONIBI_INSTALL_DIR:-$HOME/.local/bin}"
base_url="${ONIBI_INSTALL_BASE_URL:-}"
release_key_b64="${ONIBI_RELEASE_GPG_KEY_B64:-__ONIBI_RELEASE_GPG_KEY_B64__}"

fail() {
  echo "onibi install: $*" >&2
  exit 1
}

need() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

download() {
  url="$1"
  out="$2"
  if [ -n "${GITHUB_TOKEN:-}" ]; then
    curl -fsSL -H "Authorization: Bearer $GITHUB_TOKEN" -o "$out" "$url"
  else
    curl -fsSL -o "$out" "$url"
  fi
}

download_stdout() {
  url="$1"
  if [ -n "${GITHUB_TOKEN:-}" ]; then
    curl -fsSL -H "Authorization: Bearer $GITHUB_TOKEN" "$url"
  else
    curl -fsSL "$url"
  fi
}

decode_key() {
  if printf '' | base64 --decode >/dev/null 2>&1; then
    printf '%s' "$release_key_b64" | base64 --decode
  elif printf '' | base64 -d >/dev/null 2>&1; then
    printf '%s' "$release_key_b64" | base64 -d
  else
    printf '%s' "$release_key_b64" | base64 -D
  fi
}

detect_os() {
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$os" in
    darwin|linux) printf '%s\n' "$os" ;;
    *) fail "unsupported os: $os" ;;
  esac
}

detect_arch() {
  machine="$(uname -m)"
  case "$machine" in
    x86_64|amd64) printf '%s\n' "x86_64" ;;
    arm64|aarch64) printf '%s\n' "arm64" ;;
    armv6*|armhf) printf '%s\n' "armv6" ;;
    armv7*) printf '%s\n' "armv7" ;;
    *) fail "unsupported arch: $machine" ;;
  esac
}

latest_tag() {
  api="https://api.github.com/repos/$repo/releases/latest"
  body="$(download_stdout "$api")"
  tag="$(printf '%s\n' "$body" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
  [ -n "$tag" ] || fail "unable to parse latest release tag"
  printf '%s\n' "$tag"
}

verify_signature() {
  checksums="$1"
  signature="$2"
  case "$release_key_b64" in
    ""|"__ONIBI_RELEASE_GPG_KEY_B64__") fail "release public key not embedded" ;;
  esac
  gnupg="$tmp/gnupg"
  mkdir -p "$gnupg"
  chmod 700 "$gnupg"
  key="$tmp/onibi-release.asc"
  decode_key >"$key" || fail "decode release public key"
  GNUPGHOME="$gnupg" gpg --batch --quiet --import "$key" >/dev/null 2>&1 || fail "import release public key"
  GNUPGHOME="$gnupg" gpg --batch --verify "$signature" "$checksums" >/dev/null 2>&1 || fail "verify checksums.txt.sig"
}

verify_checksum() {
  asset="$1"
  awk -v asset="$asset" 'NF >= 2 && $NF == asset { print; found = 1; exit } END { if (!found) exit 1 }' "$tmp/checksums.txt" >"$tmp/checksum.line" || fail "checksums.txt missing $asset"
  if command -v shasum >/dev/null 2>&1; then
    (cd "$tmp" && shasum -a 256 -c checksum.line >/dev/null) || fail "checksum mismatch for $asset"
  elif command -v sha256sum >/dev/null 2>&1; then
    (cd "$tmp" && sha256sum -c checksum.line >/dev/null) || fail "checksum mismatch for $asset"
  else
    fail "missing required command: shasum or sha256sum"
  fi
}

install_bins() {
  mkdir -p "$install_dir"
  mkdir -p "$tmp/extract"
  tar -tzf "$tmp/$asset" >"$tmp/archive.list"
  while IFS= read -r name; do
    case "$name" in
      ""|/*|../*|*/../*) fail "unsafe archive path: $name" ;;
    esac
  done <"$tmp/archive.list"
  tar -xzf "$tmp/$asset" -C "$tmp/extract"
  [ -f "$tmp/extract/onibi" ] || fail "archive missing onibi"
  [ -f "$tmp/extract/onibi-notify" ] || fail "archive missing onibi-notify"
  install -m 0755 "$tmp/extract/onibi" "$install_dir/onibi"
  install -m 0755 "$tmp/extract/onibi-notify" "$install_dir/onibi-notify"
}

path_profile() {
  if [ -n "${ONIBI_INSTALL_PROFILE:-}" ]; then
    printf '%s\n' "$ONIBI_INSTALL_PROFILE"
    return
  fi
  shell_name="$(basename "${SHELL:-}")"
  case "$shell_name" in
    zsh) printf '%s\n' "$HOME/.zshrc" ;;
    bash) printf '%s\n' "$HOME/.bashrc" ;;
    *) printf '%s\n' "$HOME/.profile" ;;
  esac
}

ensure_path() {
  case ":$PATH:" in
    *":$install_dir:"*) return ;;
  esac
  [ "${ONIBI_INSTALL_NO_PATH_UPDATE:-0}" = "1" ] && {
    echo "onibi installed to $install_dir; add it to PATH" >&2
    return
  }
  profile="$(path_profile)"
  mkdir -p "$(dirname "$profile")"
  touch "$profile"
  if ! grep -F "$install_dir" "$profile" >/dev/null 2>&1; then
    {
      printf '\n# onibi\n'
      printf '%s\n' "export PATH=\"$install_dir:\$PATH\""
    } >>"$profile"
    echo "added $install_dir to $profile"
  fi
}

need curl
need tar
need gpg
need install
need base64
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

os="$(detect_os)"
arch="$(detect_arch)"
if [ "$version" = "latest" ]; then
  tag="$(latest_tag)"
else
  tag="$version"
fi
case "$tag" in
  v*) version_no_v="${tag#v}" ;;
  *) version_no_v="$tag" ;;
esac
asset="onibi_${version_no_v}_${os}_${arch}.tar.gz"
if [ -z "$base_url" ]; then
  base_url="https://github.com/$repo/releases/download/$tag"
fi

download "$base_url/$asset" "$tmp/$asset"
download "$base_url/checksums.txt" "$tmp/checksums.txt"
download "$base_url/checksums.txt.sig" "$tmp/checksums.txt.sig"
verify_signature "$tmp/checksums.txt" "$tmp/checksums.txt.sig"
verify_checksum "$asset"
install_bins
ensure_path
"$install_dir/onibi" version
echo "installed onibi to $install_dir/onibi"
