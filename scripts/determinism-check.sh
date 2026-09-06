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
    --output "$2" \
    --spec-version "$1" \
    --reproducible \
    --path-flavor posix \
    --policy lenient
}

# Every version the tool writes, because the claim is about the tool and not
# about one of its outputs: the same evidence must produce the same bytes on
# every platform, whichever revision was asked for.
for version in 1.6 1.7; do
  run "$version" "$tmp/first.cdx.json"
  run "$version" "$tmp/second.cdx.json"
  cmp "$tmp/first.cdx.json" "$tmp/second.cdx.json"
  printf 'portable SBOM %s SHA-256: ' "$version"
  sha256sum "$tmp/first.cdx.json" | cut -d ' ' -f 1
done
