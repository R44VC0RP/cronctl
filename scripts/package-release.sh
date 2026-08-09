#!/usr/bin/env bash
set -euo pipefail

version=${1:?usage: package-release.sh VERSION [OUTPUT_DIR]}
output_dir=${2:-dist}
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
work_dir=$(mktemp -d)
trap 'rm -rf "$work_dir"' EXIT

case "$output_dir" in
  /*) ;;
  *) output_dir="$root/$output_dir" ;;
esac

rm -rf "$output_dir"
mkdir -p "$output_dir"

ldflags="-s -w -X github.com/R44VC0RP/cronctl/internal/cronctl.version=$version"

build_unix() {
  os=$1
  arch=$2
  stage="$work_dir/cronctl-$os-$arch"
  mkdir -p "$stage"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags "$ldflags" -o "$stage/cronctl" ./cmd/cronctl
  tar -C "$stage" -czf "$output_dir/cronctl-$os-$arch.tar.gz" cronctl
}

build_windows() {
  arch=$1
  stage="$work_dir/cronctl-windows-$arch"
  mkdir -p "$stage"
  CGO_ENABLED=0 GOOS=windows GOARCH="$arch" go build -trimpath -ldflags "$ldflags" -o "$stage/cronctl.exe" ./cmd/cronctl
  CGO_ENABLED=0 GOOS=windows GOARCH="$arch" go build -trimpath -ldflags "$ldflags -H=windowsgui" -o "$stage/cronctl-daemon.exe" ./cmd/cronctl-daemon
  (
    cd "$stage"
    zip -q "$output_dir/cronctl-windows-$arch.zip" cronctl.exe cronctl-daemon.exe
  )
}

cd "$root"
build_unix darwin arm64
build_unix darwin amd64
build_unix linux arm64
build_unix linux amd64
build_windows arm64
build_windows amd64

(
  cd "$output_dir"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum cronctl-* > SHA256SUMS
  else
    shasum -a 256 cronctl-* > SHA256SUMS
  fi
)
