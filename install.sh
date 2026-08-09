#!/bin/sh
set -eu

repo=R44VC0RP/cronctl
version=${CRONCTL_VERSION:-latest}

command -v curl >/dev/null 2>&1 || {
  echo "cronctl installer: curl is required" >&2
  exit 1
}

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *)
    echo "cronctl installer: unsupported operating system: $(uname -s)" >&2
    exit 1
    ;;
esac

case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *)
    echo "cronctl installer: unsupported architecture: $(uname -m)" >&2
    exit 1
    ;;
esac

asset="cronctl-$os-$arch.tar.gz"
if [ -n "${CRONCTL_DOWNLOAD_BASE:-}" ]; then
  download_base=$CRONCTL_DOWNLOAD_BASE
elif [ "$version" = latest ]; then
  download_base="https://github.com/$repo/releases/latest/download"
else
  case "$version" in
    v*) tag=$version ;;
    *) tag="v$version" ;;
  esac
  download_base="https://github.com/$repo/releases/download/$tag"
fi

if [ -n "${CRONCTL_INSTALL_DIR:-}" ]; then
  install_dir=$CRONCTL_INSTALL_DIR
elif [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
  install_dir=/usr/local/bin
else
  install_dir="$HOME/.local/bin"
fi

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT HUP INT TERM

echo "Downloading $asset..."
curl -fL --retry 3 --retry-delay 1 -o "$tmp_dir/$asset" "$download_base/$asset"
curl -fL --retry 3 --retry-delay 1 -o "$tmp_dir/SHA256SUMS" "$download_base/SHA256SUMS"

expected=$(awk -v name="$asset" '$2 == name || $2 == "*" name { print $1; exit }' "$tmp_dir/SHA256SUMS")
if [ -z "$expected" ]; then
  echo "cronctl installer: $asset is missing from SHA256SUMS" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmp_dir/$asset" | awk '{print $1}')
else
  actual=$(shasum -a 256 "$tmp_dir/$asset" | awk '{print $1}')
fi

if [ "$actual" != "$expected" ]; then
  echo "cronctl installer: checksum verification failed for $asset" >&2
  exit 1
fi

tar -xzf "$tmp_dir/$asset" -C "$tmp_dir"
mkdir -p "$install_dir"
install -m 0755 "$tmp_dir/cronctl" "$install_dir/cronctl"

echo "Installed cronctl to $install_dir/cronctl"
case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) echo "Add $install_dir to PATH to run cronctl from any shell." ;;
esac
echo "Run 'cronctl service install' when you are ready to start the scheduler."
