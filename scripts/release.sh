#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 1 ]; then
  printf 'Usage: bash scripts/release.sh vMAJOR.MINOR.PATCH[-prerelease]\n' >&2
  exit 2
fi
version=$1
if [[ ! "$version" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$ ]]; then
  printf 'Invalid release version: %s\n' "$version" >&2
  exit 2
fi

mkdir -p dist
binaries=()
for os in linux darwin windows; do
  for arch in amd64 arm64; do
    name="rocket-cli_${version}_${os}_${arch}"
    if [ "$os" = windows ]; then
      name="${name}.exe"
    fi
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build \
      -mod=readonly -trimpath -buildvcs=false \
      -ldflags "-s -w -buildid= -X github.com/sbauer/rocket-cli/internal/cli.version=$version" \
      -o "dist/$name" .
    binaries+=("$name")
  done
done

(
  cd dist
  shasum -a 256 "${binaries[@]}" > SHA256SUMS
)
