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

# No curated licences. Every dependency's licence is now detected from the
# vendored text by the SPDX template matcher (section 22.3 technique 4,
# deviation D18); the three entries that used to be asserted here -- two
# BSD-3-Clause and one Apache-2.0 -- were only needed because exact text
# matching cannot see through a filled-in copyright holder. If a future
# dependency is not recognized, the SBOM says UNKNOWN_LICENSE rather than
# carrying an assertion nobody re-derives.

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

# build_cmake_bundle writes sbomb-cmake.tar.gz: the CMake module, the fetcher
# with this release's version substituted into it, and the CMakeLists.txt that
# FetchContent_MakeAvailable adds. Deterministic: fixed ownership, fixed mtime
# and a sorted member order, so two builds of one tag produce one archive.
build_cmake_bundle() {
  bundle_dir="$output_dir/.cmake-bundle"
  rm -rf "$bundle_dir"
  mkdir -p "$bundle_dir"
  cp "$root_dir/cmake/CMakeLists.txt" "$root_dir/cmake/Sbomb.cmake" "$bundle_dir/"
  # The tag, not the bare version: $version has had its leading v stripped for
  # the ldflags, and the download URL is built from the tag.
  sed "s/@SBOMB_VERSION@/v$version/" "$root_dir/cmake/SbombFetch.cmake" > "$bundle_dir/SbombFetch.cmake"
  if grep -q '@SBOMB_VERSION@' "$bundle_dir/SbombFetch.cmake"; then
    printf 'release.sh: the version placeholder survived substitution\n' >&2
    exit 1
  fi
  (cd "$bundle_dir" && tar \
    --sort=name --owner=0 --group=0 --numeric-owner \
    --mtime="@${SOURCE_DATE_EPOCH:-0}" \
    -czf "$output_dir/sbomb-cmake.tar.gz" CMakeLists.txt Sbomb.cmake SbombFetch.cmake)
  rm -rf "$bundle_dir"
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
    # Darwin costs nothing to cross-compile: the tool is CGO_ENABLED=0 and
    # reads files, so it needs no SDK and no macOS host. Both architectures,
    # because Apple silicon and Intel are both still in use.
    build_one darwin amd64 ""
    build_one darwin arm64 ""
    for binary in "$output_dir"/sbomb-*; do
      write_self_sbom "$binary"
    done
    # The CMake bundle, built after the SBOMs so the loop above does not see it
    # and before SHA256SUMS so that it is covered like everything else. It is
    # what FetchContent pulls in: the module, and a fetcher pinned to this
    # version, so a consumer gets sbomb_enable and a matching binary at once.
    build_cmake_bundle
    (cd "$output_dir" && sha256sum sbomb-* > SHA256SUMS)
    ;;
  *)
    printf 'usage: scripts/release.sh [build|--check-reproducible]\n' >&2
    exit 1
    ;;
esac