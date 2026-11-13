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
tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/rocket-release.XXXXXX")
trap 'rm -f "$tmpdir/rocket" "$tmpdir/rocket.exe" "$tmpdir/archive.tar.gz" "$tmpdir/archive.zip" "$tmpdir/SHA256SUMS"; rmdir "$tmpdir"' EXIT

case "$(tar --version)" in
  bsdtar*) tar_owner=(--uid 0 --gid 0 --uname '' --gname '') ;;
  *) tar_owner=(--owner=0 --group=0 --numeric-owner) ;;
esac

archives=()
for os in linux darwin windows; do
  for arch in amd64 arm64; do
    name="rocket_${version}_${os}_${arch}"
    binary=rocket
    if [ "$os" = windows ]; then
      binary=rocket.exe
    fi
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build \
      -mod=readonly -trimpath -buildvcs=false \
      -ldflags "-s -w -buildid= -X github.com/sbauer/rocket-cli/internal/cli.version=$version" \
      -o "$tmpdir/$binary" ./cmd/rocket
    chmod 755 "$tmpdir/$binary"
    TZ=UTC touch -t 198001010000 "$tmpdir/$binary"
    COPYFILE_DISABLE=1 tar -c --format ustar "${tar_owner[@]}" -f - -C "$tmpdir" "$binary" |
      gzip -n > "$tmpdir/archive.tar.gz"
    (cd "$tmpdir" && TZ=UTC zip -q -X archive.zip "$binary")
    mv "$tmpdir/archive.tar.gz" "dist/$name.tar.gz"
    mv "$tmpdir/archive.zip" "dist/$name.zip"
    rm "$tmpdir/$binary"
    archives+=("$name.tar.gz" "$name.zip")
  done
done

(
  cd dist
  shasum -a 256 "${archives[@]}"
) > "$tmpdir/SHA256SUMS"
mv "$tmpdir/SHA256SUMS" dist/SHA256SUMS
