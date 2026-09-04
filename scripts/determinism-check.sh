#!/bin/sh
set -eu

fixture=$(CDPATH= cd -- "$(dirname -- "$0")/../testdata/fixtures/portable" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

run() {
  go run ./cmd/sbomb generate \
    --build-dir "$fixture/build" \
    --config "$fixture/config.json" \
    --output "$1" \
    --reproducible \
    --path-flavor posix
}

run "$tmp/first.cdx.json"
run "$tmp/second.cdx.json"
cmp "$tmp/first.cdx.json" "$tmp/second.cdx.json"
printf 'portable SBOM SHA-256: '
sha256sum "$tmp/first.cdx.json" | cut -d ' ' -f 1
