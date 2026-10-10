#!/bin/sh
set -eu

repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# keep FILE NAME copies a document into $SBOMB_DETERMINISM_KEEP when it is set.
# The hashes say that two platforms disagree; only the documents say where, and
# a Windows document cannot be reproduced on any other host to find out.
keep() {
  if [ -n "${SBOMB_DETERMINISM_KEEP:-}" ]; then
    mkdir -p "$SBOMB_DETERMINISM_KEEP"
    cp "$1" "$SBOMB_DETERMINISM_KEEP/$2"
  fi
}

# Work on a copy: generate writes an evidence dump into the build directory,
# and the committed corpus is a golden artifact that must stay unchanged.
fixture="$tmp/fixture"
mkdir -p "$fixture"
cp -r "$repo/testdata/fixtures/gcc-ninja/p02-static/build" "$fixture/build"

# run FORMAT VERSION OUTPUT. SOURCE_DATE_EPOCH is pinned for SPDX only: an
# SPDX document has to state when it was created, and under --reproducible
# that time is the pin or nothing (section 29) -- and since the creation time
# is part of the document's identity, the pin is what makes two platforms
# agree. A reproducible CycloneDX document states no time, so its runs are
# left exactly as they were before SPDX existed.
run() {
  if [ "$1" = spdx-json ]; then
    epoch=1700000000
  else
    epoch=
  fi
  SOURCE_DATE_EPOCH=$epoch go run ./cmd/sbomb generate \
    --build-dir "$fixture/build" \
    --config "$repo/testdata/config/portable.json" \
    --output "$3" \
    --format "$1" \
    --spec-version "$2" \
    --reproducible \
    --path-flavor posix \
    --policy lenient
}

# Every version the tool writes, in every format, because the claim is about
# the tool and not about one of its outputs: the same evidence must produce the
# same bytes on every platform, whichever format and revision was asked for.
for version in 1.6 1.7 3.0.1; do
  case "$version" in
    3.0.1) format=spdx-json extension=spdx.json ;;
    *) format=cyclonedx-json extension=cdx.json ;;
  esac
  run "$format" "$version" "$tmp/first.$extension"
  run "$format" "$version" "$tmp/second.$extension"
  cmp "$tmp/first.$extension" "$tmp/second.$extension"
  printf 'portable SBOM %s SHA-256: ' "$version"
  sha256sum "$tmp/first.$extension" | cut -d ' ' -f 1
  keep "$tmp/first.$extension" "portable-$version.$extension"
done

# A Windows build read under --path-flavor windows, and more than twice.
# msvc-ninja/p06-unity names its unity source three ways -- the Ninja graph's
# escaped absolute path, a path relative to the build directory and the
# compilation database's own -- and only one of them can be read on a POSIX
# host. Which one was kept depended on the order the evidence was consulted in,
# which is not fixed, so the file was hashed on some runs and missing on others,
# and the document identity changed with it. Two runs agree by chance often
# enough to miss that; six do not. A fresh copy each time, because generate
# writes its evidence dump into the build directory.
#
# In both formats. Each states things the other does not -- SPDX names the
# adapters that contributed, CycloneDX keeps its own serial derivation -- so an
# order dependence can surface in one and not in the other.
#
# Built once rather than through go run, which would compile and link the tool
# again for every one of the twelve runs. GOEXE, because Windows will not run
# it without the extension.
unity_sbomb="$tmp/sbomb$(go env GOEXE)"
go build -o "$unity_sbomb" ./cmd/sbomb
# unity_run FORMAT VERSION OUTPUT, with SOURCE_DATE_EPOCH pinned for SPDX only,
# as in run above.
unity_run() {
  rm -rf "$tmp/unity"
  mkdir -p "$tmp/unity"
  cp -r "$repo/testdata/fixtures/msvc-ninja/p06-unity/build" "$tmp/unity/build"
  if [ "$1" = spdx-json ]; then
    epoch=1700000000
  else
    epoch=
  fi
  SOURCE_DATE_EPOCH=$epoch "$unity_sbomb" generate \
    --build-dir "$tmp/unity/build" \
    --output "$3" \
    --format "$1" \
    --spec-version "$2" \
    --reproducible \
    --path-flavor windows \
    --policy lenient
}
for pair in "cyclonedx-json 1.6 cdx.json" "spdx-json 3.0.1 spdx.json"; do
  set -- $pair
  unity_run "$1" "$2" "$tmp/unity-first.$3"
  for attempt in 2 3 4 5 6; do
    unity_run "$1" "$2" "$tmp/unity-again.$3"
    cmp "$tmp/unity-first.$3" "$tmp/unity-again.$3"
  done
  printf 'windows-flavor unity SBOM %s SHA-256: ' "$2"
  sha256sum "$tmp/unity-first.$3" | cut -d ' ' -f 1
  keep "$tmp/unity-first.$3" "windows-flavor-unity-$2.$3"
done

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
