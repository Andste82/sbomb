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

# A Windows build read under --path-flavor windows, and more than twice.
# msvc-ninja/p06-unity names its unity source three ways -- the Ninja graph's
# escaped absolute path, a path relative to the build directory and the
# compilation database's own -- and only one of them can be read on a POSIX
# host. Which one was kept depended on the order the evidence was consulted in,
# which is not fixed, so the file was hashed on some runs and missing on others,
# and the reproducible serial number changed with it. Two runs agree by chance
# often enough to miss that; six do not. A fresh copy each time, because
# generate writes its evidence dump into the build directory.
#
# Built once rather than through go run, which would compile and link the tool
# again for every one of the six runs. GOEXE, because Windows will not run it
# without the extension.
unity_sbomb="$tmp/sbomb$(go env GOEXE)"
go build -o "$unity_sbomb" ./cmd/sbomb
unity_run() {
  rm -rf "$tmp/unity"
  mkdir -p "$tmp/unity"
  cp -r "$repo/testdata/fixtures/msvc-ninja/p06-unity/build" "$tmp/unity/build"
  "$unity_sbomb" generate \
    --build-dir "$tmp/unity/build" \
    --output "$1" \
    --reproducible \
    --path-flavor windows \
    --policy lenient
}
unity_run "$tmp/unity-first.cdx.json"
for attempt in 2 3 4 5 6; do
  unity_run "$tmp/unity-again.cdx.json"
  cmp "$tmp/unity-first.cdx.json" "$tmp/unity-again.cdx.json"
done
printf 'windows-flavor unity SBOM 1.6 SHA-256: '
sha256sum "$tmp/unity-first.cdx.json" | cut -d ' ' -f 1

# The FOSS documents of section 32.6, which are a further rendering of the same
# discovery and have to be as reproducible as the SBOM: a notices document that
# churns cannot be reviewed, and a release-to-release diff is how a new copyleft
# component gets noticed (requirement R12).
#
# p14-foss, because it is the only project whose sources the corpus carries --
# and its source tree is read where it is rather than where it was built, which
# is a relocated run (section 7.9).
foss_fixture="$tmp/foss"
mkdir -p "$foss_fixture"
cp -r "$repo/testdata/fixtures/gcc-ninja/p14-foss/build" "$foss_fixture/build"

# Under the default profile, and deliberately not under the lenient one the
# goldens are captured with: the default is what a user who passes no flags
# gets, its includeToolchainRuntime=separate-component reports the toolchain
# runtime as a component, and that is the component whose narrowed headers the
# union licence view actually reads. So this is the more demanding path -- the
# one where the notices document depends on the order twenty-five header files
# were read in.
run_foss() {
  go run ./cmd/sbomb foss \
    --build-dir "$foss_fixture/build" \
    --source-dir "$repo/testdata/fixtures/p14-foss-src" \
    --out "$1" \
    --reproducible
}

run_foss "$tmp/foss-first"
run_foss "$tmp/foss-second"
for document in THIRD-PARTY-NOTICES.txt foss-review.txt foss-review.json source-obligations.txt; do
  cmp "$tmp/foss-first/$document" "$tmp/foss-second/$document"
  printf 'FOSS %s SHA-256: ' "$document"
  sha256sum "$tmp/foss-first/$document" | cut -d ' ' -f 1
done

# And the guarantee that the output is not a source offer, checked here as well
# as in the test suite: this script is what runs on three platforms.
if find "$tmp/foss-first" -type f \( -name '*.c' -o -name '*.h' -o -name '*.cpp' -o -name '*.hpp' \
  -o -name '*.S' -o -name '*.tar*' -o -name '*.zip' -o -name '*.patch' \) | grep -q .; then
  echo 'the FOSS output directory contains source, an archive or a patch' >&2
  exit 1
fi
