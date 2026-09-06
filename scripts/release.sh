#!/usr/bin/env bash
set -euo pipefail

root_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
# The "v" belongs to the git tag, not to the version. Without stripping it a
# release binary reported "sbomb v0.8.0" while every development build reported
# "sbomb 0.8.0", and the difference reached metadata.tools in the SBOM.
version=${VERSION:-$(git -C "$root_dir" describe --tags --always --dirty 2>/dev/null || printf 'dev')}
version=${version#v}
output_dir=${OUTPUT_DIR:-"$root_dir/dist/$version"}
ldflags="-s -w -X github.com/example/sbomb/internal/buildinfo.Version=$version"

build_one() {
  local goos=$1 goarch=$2 suffix=$3
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
    -trimpath -buildvcs=false -ldflags "$ldflags" -o "$output_dir/sbomb-${goos}-${goarch}${suffix}" ./cmd/sbomb
}

# self_licences curates what exact licence-text matching cannot recognize.
#
# The matcher of section 22.3 hashes the normalized text and compares it to the
# SPDX list. That finds a verbatim licence and nothing else, and a real file is
# rarely verbatim: these three fill in the copyright holder and renumber the
# clause list, so the text is unmistakable to a person and unmatchable to a
# hash. The identifiers below are each taken from the licence file vendored in
# this repository. They are recorded as curated, never as detected, and
# "sbomb self" reports a conflict if the text ever contradicts one.
self_licences=(
  --license "github.com/google/uuid=BSD-3-Clause"
  --license "golang.org/x/text=BSD-3-Clause"
  --license "github.com/santhosh-tekuri/jsonschema/v6=Apache-2.0"
  --license "std=BSD-3-Clause"
)

# write_self_sbom describes one released binary from the module evidence its
# linker recorded (roadmap phase 8, step 8e). The earlier attempt fabricated a
# compile database naming one Go file beside an empty linker map, which is the
# evidence-free guessing this tool exists to refuse (deviation D16).
write_self_sbom() {
  local binary=$1
  # The tool is built with the release ldflags rather than run from dist,
  # because one of the three artifacts is for another platform and the SBOM has
  # to name the same tool version whichever host writes it. The supplier is the
  # one the tool already publishes for itself in metadata.tools; it is curated,
  # not derived from a repository URL, which section 20.5 forbids.
  (cd "$root_dir" && go run -ldflags "$ldflags" ./cmd/sbomb self "$binary" \
    --output "${binary}.cdx.json" \
    --version "$version" \
    --supplier sbomb \
    --module-dir "$root_dir" \
    --goroot "$(go env GOROOT)" \
    "${self_licences[@]}" \
    --reproducible)
}

check_reproducible() {
  local first second
  first=$(mktemp)
  second=$(mktemp)
  trap 'rm -f "$first" "$second"' RETURN
  (cd "$root_dir" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags "$ldflags" -o "$first" ./cmd/sbomb)
  (cd "$root_dir" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags "$ldflags" -o "$second" ./cmd/sbomb)
  cmp "$first" "$second"
}

case "${1:-build}" in
  --check-reproducible)
    check_reproducible
    ;;
  build)
    mkdir -p "$output_dir"
    build_one linux amd64 ""
    build_one linux arm64 ""
    build_one windows amd64 ".exe"
    for binary in "$output_dir"/sbomb-*; do
      write_self_sbom "$binary"
    done
    (cd "$output_dir" && sha256sum sbomb-* > SHA256SUMS)
    ;;
  *)
    printf 'usage: scripts/release.sh [build|--check-reproducible]\n' >&2
    exit 1
    ;;
esac