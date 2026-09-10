#!/usr/bin/env bash
# Cross-compiles the Converge binaries and packages release tarballs.
#
#   VERSION   overrides tools/version.sh
#   PLATFORMS space-separated os/arch list (default "linux/amd64 linux/arm64")
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
version="${VERSION:-$("$root/tools/version.sh")}"
platforms="${PLATFORMS:-linux/amd64 linux/arm64}"
dist="$root/dist"
ldflags="-s -w -X github.com/jtumidanski/converge/internal/buildinfo.Version=${version}"

rm -rf "$dist"
mkdir -p "$dist"

for platform in $platforms; do
  os="${platform%%/*}"
  arch="${platform##*/}"
  outdir="$dist/${os}-${arch}"
  mkdir -p "$outdir"
  for cmd in converge converge-cli; do
    echo "building $cmd for $os/$arch ($version)"
    (cd "$root/apps/backend" && CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
      go build -trimpath -ldflags "$ldflags" -o "$outdir/$cmd" "./cmd/$cmd")
  done
  tar -czf "$dist/converge-${version}-${os}-${arch}.tar.gz" -C "$outdir" converge converge-cli
done

echo "artifacts in $dist:"
ls -1 "$dist"/*.tar.gz
