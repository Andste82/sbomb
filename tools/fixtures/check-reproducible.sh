#!/usr/bin/env bash
# Two regenerations of an unchanged corpus must produce the same bytes.
#
# Reviewing a real corpus change means reading a diff, and a diff full of files
# that moved for no reason is a diff nobody reads. This regenerates twice and
# reports anything that differs.
#
# Three projects are excluded, each because the toolchain itself is not
# reproducible and no build input this repository controls changes that. They
# are named rather than filtered by pattern, so a fourth cannot join them
# silently:
#
#   p03-dupnames  CMake writes a target's dependency list in an unstable order,
#                 so mod_a and mod_b swap places in the File API reply. The
#                 file is content-addressed, so its name moves with it, and the
#                 codemodel and index that reference the name move too.
#
#   p09-lto       The linker map names GCC's temporary LTO objects
#                 (/tmp/ccXXXXXX.ltrans0.ltrans.o), whose names are drawn per
#                 invocation. That is the fixture's whole point -- section 17.3
#                 downgrades attribution under LTO precisely because the map
#                 names temporaries rather than sources -- so removing it would
#                 remove the evidence. The object files themselves are stable
#                 since -frandom-seed was pinned.
#
#   p11-conan     Conan assigns a random suffix to the cache folder a locally
#                 built package lands in, and every generated file that names
#                 that folder moves with it. The cache is wiped per run on
#                 purpose, so the corpus does not depend on host state; keeping
#                 it would trade one kind of unreproducibility for a worse one.
#
# Usage:
#   tools/fixtures/check-reproducible.sh
#
# It regenerates the corpus in the working tree twice, which takes a few
# minutes and needs every fixture toolchain installed.
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
corpus_dir="$repo_root/testdata/fixtures"

UNSTABLE=(p03-dupnames p09-lto p11-conan)

log() { printf '%s\n' "$*" >&2; }

snapshot=$(mktemp -d)
trap 'rm -rf "$snapshot"' EXIT

log "first regeneration"
"$repo_root/tools/fixtures/regen.sh" >/dev/null
cp -a "$corpus_dir/." "$snapshot/"

log "second regeneration"
"$repo_root/tools/fixtures/regen.sh" >/dev/null

# diff -rq reports both differing files and files present on one side only,
# which is what a content-addressed name change looks like.
differences=$(diff -rq "$snapshot" "$corpus_dir" || true)

unexpected=0
while IFS= read -r line; do
  [[ -n "$line" ]] || continue
  excluded=0
  for project in "${UNSTABLE[@]}"; do
    if [[ "$line" == *"/$project/"* || "$line" == *"/$project:"* ]]; then
      excluded=1
      break
    fi
  done
  if (( excluded == 0 )); then
    log "  $line"
    unexpected=$((unexpected + 1))
  fi
done <<< "$differences"

if (( unexpected > 0 )); then
  log ""
  log "$unexpected file(s) differ between two regenerations of an unchanged corpus"
  exit 1
fi
log ""
log "two regenerations agree, outside the ${#UNSTABLE[@]} recorded projects"
