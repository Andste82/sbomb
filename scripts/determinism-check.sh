#!/bin/sh
set -eu

repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# Work on a copy: generate writes an evidence dump into the build directory,
# and the committed corpus is a golden artifact that must stay unchanged.
fixture="$tmp/fixture"
mkdir -p "$fixture"
cp -r "$repo/testdata/fixtures/gcc-ninja/p02-static/build" "$fixture/build"

run() {
  go run ./cmd/sbomb generate \
    --build-dir "$fixture/build" \
    --config "$repo/testdata/config/portable.json" \
    --output "$1" \
    --reproducible \
    --path-flavor posix \
    --policy lenient
}

run "$tmp/first.cdx.json"
run "$tmp/second.cdx.json"
cmp "$tmp/first.cdx.json" "$tmp/second.cdx.json"
printf 'portable SBOM SHA-256: '
sha256sum "$tmp/first.cdx.json" | cut -d ' ' -f 1
