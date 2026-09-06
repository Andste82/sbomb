#!/bin/sh
set -eu

duration=${1:-60s}

run() {
	package=$1
	name=$2
	go test "$package" -run '^$' -fuzz="^${name}$" -fuzztime="$duration"
}

run ./internal/adapters/depfiles FuzzParse
run ./internal/adapters/linkers/mapparser FuzzParse
run ./internal/adapters/ninja FuzzParseFile
run ./internal/adapters/ninja FuzzParseDeps
run ./internal/adapters/compiledb FuzzParse
run ./internal/adapters/cmakeapi FuzzParseReplyDir
run ./internal/adapters/binfmt FuzzInspect
run ./internal/config FuzzLoad
run ./internal/policy FuzzLoadWaivers

# Added with the parsers of roadmap phases 6 and 7. All of them read a file
# from somebody else's build tree, which section 30 calls untrusted input.
run ./internal/respfile FuzzTokenize
run ./internal/respfile FuzzExpand
run ./internal/generate FuzzUnityIncludes
run ./internal/generate FuzzPCHIncludes
run ./internal/adapters/pkgmanager FuzzDiscover
run ./internal/adapters/binfmt FuzzInspectBytes
run ./internal/testutil FuzzLoadFixtureManifest
run ./internal/adapters/manifest FuzzParse