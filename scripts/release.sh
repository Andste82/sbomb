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
    # No self-SBOM. It was produced from a compile database written here on the
    # spot, naming one Go file and an empty linker map -- the evidence-free
    # guessing this tool exists to refuse, and it made every release build fail
    # with exit 3 besides, because the resulting findings tripped the policy.
    # It returns when it is derived from evidence (deviation D16).
    (cd "$output_dir" && sha256sum sbomb-* > SHA256SUMS)
    ;;
  *)
    printf 'usage: scripts/release.sh [build|--check-reproducible]\n' >&2
    exit 1
    ;;
esac