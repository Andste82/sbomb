#!/usr/bin/env bash
# Regenerate the golden fixture corpus from real toolchain output.
#
# Every fixture is produced by actually configuring and building a small CMake
# project and then harvesting only the build evidence -- never the sources.
# Builds run under the sentinel roots /__fixture_src__ and /__fixture_build__
# so that compile_commands.json, the CMake File API reply, .ninja_deps, the
# linker map, the link depfile and the DWARF inside the artifact all record
# portable paths natively, with no post-hoc text rewriting.
#
# Usage:
#   tools/fixtures/regen.sh            regenerate the corpus
#   tools/fixtures/regen.sh --check    verify the committed corpus is complete
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
projects_dir="$repo_root/tools/fixtures/projects"
toolchains_dir="$repo_root/tools/fixtures/toolchains"
corpus_dir="$repo_root/testdata/fixtures"

SRC_ROOT=${SBOMB_FIXTURE_SRC:-/__fixture_src__}
BUILD_ROOT=${SBOMB_FIXTURE_BUILD:-/__fixture_build__}
# Package managers keep their caches outside both trees and record absolute
# paths into the files they generate, so their root is a sentinel of its own.
PKG_ROOT=${SBOMB_FIXTURE_PKG:-/__fixture_pkg__}

# Fixed so that regenerating an unchanged corpus is a no-op. Bump deliberately
# when the corpus is rebuilt against a new toolchain.
FIXTURE_DATE="2026-09-05"

PROJECTS=(p01-hello p02-static p03-dupnames p04-generated p05-headeronly p06-unity p07-pch p08-gcsections p09-lto p10-fetchcontent p11-conan p12-assets)

# Some projects only make sense for some toolchains. A Conan package is built
# for one target, so linking it into an ARM or Windows binary is not a fixture
# failure but a category error; the corpus records what a real build produces.
declare -A PROJECT_TOOLCHAINS=(
  [p11-conan]="gcc-ninja gcc-make clang-ninja"
)

# name|generator|toolchain file (empty for native)
TOOLCHAINS=(
  "gcc-ninja|Ninja|"
  "gcc-make|Unix Makefiles|"
  "clang-ninja|Ninja|clang.cmake"
  "arm-none-eabi|Ninja|arm-none-eabi.cmake"
  "mingw-w64|Ninja|mingw-w64.cmake"
)

# Files above this size are refused: the corpus stores evidence, not payloads.
MAX_FILE_BYTES=262144

log() { printf '%s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

# --------------------------------------------------------------------------
# --check: the corpus is present and complete, without rebuilding anything.
# --------------------------------------------------------------------------
if [[ "${1:-}" == "--check" ]]; then
  missing=0
  for toolchain_spec in "${TOOLCHAINS[@]}"; do
    toolchain=${toolchain_spec%%|*}
    for project in "${PROJECTS[@]}"; do
      allowed=${PROJECT_TOOLCHAINS[$project]:-}
      if [[ -n "$allowed" && " $allowed " != *" $toolchain "* ]]; then
        continue
      fi
      dir="$corpus_dir/$toolchain/$project"
      for required in manifest.json PROVENANCE.md build/compile_commands.json; do
        if [[ ! -e "$dir/$required" ]]; then
          log "missing $toolchain/$project/$required"
          missing=1
        fi
      done
      if ! compgen -G "$dir/build/.cmake/api/v1/reply/index-*.json" > /dev/null; then
        log "missing $toolchain/$project CMake File API reply"
        missing=1
      fi
    done
  done
  [[ $missing -eq 0 ]] || die "fixture corpus is incomplete; run tools/fixtures/regen.sh"
  log "fixture corpus is complete"
  exit 0
fi

# --------------------------------------------------------------------------
# Preconditions
# --------------------------------------------------------------------------
for tool in cmake ninja make; do
  command -v "$tool" >/dev/null || die "$tool is required to regenerate fixtures"
done

if ! mkdir -p "$SRC_ROOT" "$BUILD_ROOT" 2>/dev/null; then
  die "cannot create sentinel roots $SRC_ROOT and $BUILD_ROOT.
The corpus is built under fixed absolute paths so that binary evidence
(.ninja_deps, DWARF) contains no host paths. Run with sufficient privileges,
or point SBOMB_FIXTURE_SRC / SBOMB_FIXTURE_BUILD at writable absolute paths."
fi

# --------------------------------------------------------------------------
# Harvest: copy one evidence file, refusing anything oversized.
# --------------------------------------------------------------------------
harvest() {
  local source=$1 destination=$2
  [[ -e "$source" ]] || return 0
  local size
  size=$(stat -c %s "$source")
  if (( size > MAX_FILE_BYTES )); then
    log "  skipping $(basename "$source") ($size bytes exceeds the corpus limit)"
    return 0
  fi
  mkdir -p "$(dirname "$destination")"
  cp "$source" "$destination"
}

harvest_glob() {
  local pattern=$1 out_dir=$2
  local match
  for match in $pattern; do
    [[ -e "$match" ]] || continue
    harvest "$match" "$out_dir/${match#"$BUILD_ROOT"/}"
  done
}

toolchain_version() {
  case $1 in
    gcc-ninja|gcc-make) cc --version | head -1 ;;
    clang-ninja)        clang --version | head -1 ;;
    arm-none-eabi)      arm-none-eabi-gcc --version | head -1 ;;
    mingw-w64)          x86_64-w64-mingw32-gcc --version | head -1 ;;
  esac
}

toolchain_available() {
  case $1 in
    gcc-ninja|gcc-make) command -v cc ;;
    clang-ninja)        command -v clang && command -v ld.lld ;;
    arm-none-eabi)      command -v arm-none-eabi-gcc ;;
    mingw-w64)          command -v x86_64-w64-mingw32-gcc ;;
  esac >/dev/null 2>&1
}

# --------------------------------------------------------------------------
# Conan: a local recipe in a cache under the sentinel root, so the generated
# CMakeDeps files name portable paths and no host directory reaches the corpus.
# --------------------------------------------------------------------------
conan_prepare() {
  command -v conan >/dev/null 2>&1 || return 1
  export CONAN_HOME="$PKG_ROOT/conan"
  rm -rf "$CONAN_HOME"
  mkdir -p "$CONAN_HOME/profiles"
  cat > "$CONAN_HOME/profiles/default" <<'PROFILE'
[settings]
arch=x86_64
build_type=Debug
compiler=gcc
compiler.cppstd=gnu17
compiler.libcxx=libstdc++11
compiler.version=13
os=Linux
PROFILE
  local dep
  for dep in "$SRC_ROOT"/dep/*/; do
    [[ -f "$dep/conanfile.py" ]] || continue
    conan create "$dep" --build=missing >/dev/null 2>&1 || return 1
  done
  conan install "$SRC_ROOT" --output-folder="$BUILD_ROOT" --build=missing >/dev/null 2>&1 || return 1
  return 0
}

# --------------------------------------------------------------------------
# Build and harvest one (toolchain, project) pair.
# --------------------------------------------------------------------------
generate_one() {
  local toolchain=$1 generator=$2 toolchain_file=$3 project=$4
  local out_dir="$corpus_dir/$toolchain/$project"

  rm -rf "$SRC_ROOT" "$BUILD_ROOT" "$out_dir"
  mkdir -p "$SRC_ROOT" "$BUILD_ROOT"

  cp "$projects_dir/common.cmake" "$SRC_ROOT/"
  cp -r "$projects_dir/$project/." "$SRC_ROOT/"

  # A dependency directory becomes a git repository with a fixed identity and
  # a fixed date, so its commit hash is the same on every regeneration. Without
  # that the corpus would churn on every run and the FetchContent evidence
  # would not be reproducible.
  local dep
  for dep in "$SRC_ROOT"/dep/*/; do
    [[ -d "$dep" ]] || continue
    git -C "$dep" init -q -b main
    git -C "$dep" -c user.name=sbomb -c user.email=fixtures@sbomb.invalid add -A
    GIT_AUTHOR_DATE="${FIXTURE_DATE}T00:00:00Z" GIT_COMMITTER_DATE="${FIXTURE_DATE}T00:00:00Z" \
      git -C "$dep" -c user.name=sbomb -c user.email=fixtures@sbomb.invalid \
        commit -qm "fixture dependency" --no-gpg-sign
    git -C "$dep" tag -f v1.2.0 >/dev/null
  done

  mkdir -p "$BUILD_ROOT/.cmake/api/v1/query/client-sbomb"
  cat > "$BUILD_ROOT/.cmake/api/v1/query/client-sbomb/query.json" <<'QUERY'
{"requests":[{"kind":"codemodel","version":2},{"kind":"cache","version":2},{"kind":"cmakeFiles","version":1},{"kind":"toolchains","version":1}]}
QUERY

  local configure_args=(-S "$SRC_ROOT" -B "$BUILD_ROOT" -G "$generator"
                        -DCMAKE_BUILD_TYPE=Debug -DCMAKE_EXPORT_COMPILE_COMMANDS=ON)

  # Conan: create the local recipe into a cache under the sentinel root, then
  # install it for this project. The profile is written out rather than
  # detected, so the package id does not move with the host compiler.
  if [[ -f "$SRC_ROOT/conanfile.txt" ]]; then
    if ! conan_prepare; then
      log "  SKIPPED (conan unavailable): $toolchain/$project"
      return 0
    fi
    configure_args+=(-DCMAKE_PREFIX_PATH="$BUILD_ROOT")
  fi
  # The toolchain file is recorded verbatim in the cache, in build.ninja and in
  # cmakeFiles-v1, so it has to live under the sentinel root as well; passing
  # the repository path would leak it into the corpus.
  if [[ -n "$toolchain_file" ]]; then
    cp "$toolchains_dir/$toolchain_file" "$SRC_ROOT/toolchain.cmake"
    configure_args+=(-DCMAKE_TOOLCHAIN_FILE="$SRC_ROOT/toolchain.cmake")
  fi

  if ! cmake "${configure_args[@]}" >/dev/null 2>"$BUILD_ROOT/configure.log"; then
    log "  CONFIGURE FAILED: $toolchain/$project"
    sed 's/^/    /' "$BUILD_ROOT/configure.log" | tail -8 >&2
    return 1
  fi
  if ! cmake --build "$BUILD_ROOT" >/dev/null 2>"$BUILD_ROOT/build.log"; then
    log "  BUILD FAILED: $toolchain/$project"
    sed 's/^/    /' "$BUILD_ROOT/build.log" | tail -8 >&2
    return 1
  fi

  local build_out="$out_dir/build"

  # sbomb's own output must never become fixture input.
  rm -f "$BUILD_ROOT/evidence.json"

  # Structured build-system evidence.
  harvest "$BUILD_ROOT/compile_commands.json" "$build_out/compile_commands.json"
  local reply
  for reply in "$BUILD_ROOT"/.cmake/api/v1/reply/*.json; do
    harvest "$reply" "$build_out/.cmake/api/v1/reply/$(basename "$reply")"
  done

  # Generator-specific evidence.
  if [[ "$generator" == Ninja* ]]; then
    harvest "$BUILD_ROOT/build.ninja" "$build_out/build.ninja"
    harvest "$BUILD_ROOT/rules.ninja" "$build_out/rules.ninja"
    harvest "$BUILD_ROOT/.ninja_deps" "$build_out/.ninja_deps"
  else
    harvest "$BUILD_ROOT/Makefile" "$build_out/Makefile"
    while IFS= read -r found; do
      harvest "$found" "$build_out/${found#"$BUILD_ROOT"/}"
    done < <(find "$BUILD_ROOT/CMakeFiles" \
               \( -name build.make -o -name link.txt -o -name compiler_depend.make \
                  -o -name 'objects*.rsp' -o -name '*.o.d' \) -type f | sort)
  fi

  # Packaging evidence: the manifest the build generated, the image it packed
  # and the install manifest CMake wrote (section 18).
  harvest_glob "$BUILD_ROOT/sbomb-manifest.json" "$build_out"
  harvest_glob "$BUILD_ROOT/install_manifest.txt" "$build_out"
  harvest_glob "$BUILD_ROOT/*.img" "$build_out"
  while IFS= read -r found; do
    harvest "$found" "$build_out/${found#"$BUILD_ROOT"/}"
  done < <(find "$BUILD_ROOT/generated" -type f 2>/dev/null | sort)

  # Conan evidence: the CMakeDeps files name the version and the package root,
  # and the package root holds the licence Conan copied out of the recipe.
  harvest_glob "$BUILD_ROOT/*-config-version.cmake" "$build_out"
  harvest_glob "$BUILD_ROOT/*-data.cmake" "$build_out"
  harvest_glob "$BUILD_ROOT/conandeps_legacy.cmake" "$build_out"

  # FetchContent evidence: the generated populate script names the repository
  # and the tag, and the licence file of the populated dependency is what the
  # component licence is read from (sections 21 and 22.2).
  while IFS= read -r found; do
    harvest "$found" "$build_out/${found#"$BUILD_ROOT"/}"
  done < <(find "$BUILD_ROOT/_deps" -maxdepth 5 \
             \( -name '*-populate-gitclone.cmake' -o -name '*-populate-gitinfo.txt' \) \
             -type f 2>/dev/null | sort)
  while IFS= read -r found; do
    harvest "$found" "$build_out/${found#"$BUILD_ROOT"/}"
  done < <(find "$BUILD_ROOT/_deps" -maxdepth 2 -name 'LICENSE*' -type f 2>/dev/null | sort)

  # Build-tree sources the compiler consumed. A unity build compiles a
  # generated aggregation file and a precompiled header aggregates a header
  # set; both are evidence, and both have to be readable to be parsed
  # (sections 17.1 and 14.5).
  while IFS= read -r found; do
    harvest "$found" "$build_out/${found#"$BUILD_ROOT"/}"
  done < <(find "$BUILD_ROOT/CMakeFiles" \
             \( -path '*/Unity/*' -o -name 'cmake_pch*' \) \
             -type f ! -name '*.gch' ! -name '*.pch' ! -name '*.o' | sort)

  # Link and compile artifacts.
  harvest_glob "$BUILD_ROOT/*.map" "$build_out"
  harvest_glob "$BUILD_ROOT/*.d" "$build_out"
  harvest_glob "$BUILD_ROOT/*.a" "$build_out"
  harvest_glob "$BUILD_ROOT/*.exe" "$build_out"
  while IFS= read -r found; do
    harvest "$found" "$build_out/${found#"$BUILD_ROOT"/}"
  done < <(find "$BUILD_ROOT/CMakeFiles" -name '*.o' -type f | sort)

  # Final deliverables: ELF/PE executables produced at the build root.
  while IFS= read -r found; do
    harvest "$found" "$build_out/${found#"$BUILD_ROOT"/}"
  done < <(find "$BUILD_ROOT" -maxdepth 1 -type f -executable ! -name '*.sh' | sort)

  local version
  version=$(toolchain_version "$toolchain")

  cat > "$out_dir/manifest.json" <<MANIFEST
{
  "buildRoot": "$BUILD_ROOT",
  "generatedAt": "${FIXTURE_DATE}T00:00:00Z",
  "generator": "$generator",
  "host": "linux",
  "project": "$project",
  "sourceRoot": "$SRC_ROOT",
  "toolchain": "$toolchain",
  "toolchainVersion": "$version"
}
MANIFEST

  cat > "$out_dir/PROVENANCE.md" <<PROVENANCE
# Provenance

Toolchain: $toolchain
Version: $version
Generator: $generator
Source: built from tools/fixtures/projects/$project by tools/fixtures/regen.sh
License: MIT (the repository licence; the fixture sources are part of it)
Date: $FIXTURE_DATE
PROVENANCE

  local file_count
  file_count=$(find "$out_dir" -type f | wc -l)
  log "  $toolchain/$project: $file_count files"
}

# --------------------------------------------------------------------------
# Main
# --------------------------------------------------------------------------
failures=0
skipped=()
for toolchain_spec in "${TOOLCHAINS[@]}"; do
  IFS='|' read -r toolchain generator toolchain_file <<< "$toolchain_spec"
  if ! toolchain_available "$toolchain"; then
    log "skipping $toolchain (not installed)"
    skipped+=("$toolchain")
    continue
  fi
  log "$toolchain ($generator)"
  for project in "${PROJECTS[@]}"; do
    allowed=${PROJECT_TOOLCHAINS[$project]:-}
    if [[ -n "$allowed" && " $allowed " != *" $toolchain "* ]]; then
      continue
    fi
    generate_one "$toolchain" "$generator" "$toolchain_file" "$project" || failures=$((failures + 1))
  done
done

rm -rf "$SRC_ROOT" "$BUILD_ROOT"

if (( ${#skipped[@]} > 0 )); then
  log ""
  log "skipped toolchains: ${skipped[*]}"
fi
if (( failures > 0 )); then
  die "$failures fixture(s) failed to build"
fi
log ""
log "fixture corpus regenerated"
