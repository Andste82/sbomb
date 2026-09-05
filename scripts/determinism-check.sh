#!/bin/sh
set -eu

repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
fixture="$repo/testdata/fixtures/gcc-ninja/p02-static"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

run() {
  go run ./cmd/sbomb generate \
    --build-dir "$fixture/build" \
    --config "$repo/testdata/config/portable.json" \
    --output "$1" \
    --reproducible \
    --path-flavor posix
}

run "$tmp/first.cdx.json"
run "$tmp/second.cdx.json"
cmp "$tmp/first.cdx.json" "$tmp/second.cdx.json"
printf 'portable SBOM SHA-256: '
sha256sum "$tmp/first.cdx.json" | cut -d ' ' -f 1
