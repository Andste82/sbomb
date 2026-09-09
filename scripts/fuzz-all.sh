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

# The readers of the package-manager lock files, each one written here rather
# than vendored, each one handed a file out of somebody else's build tree.
run ./internal/adapters/pkgmanager FuzzIDFLock
run ./internal/adapters/pkgmanager FuzzIDFManifest
run ./internal/adapters/pkgmanager FuzzCPMLock

# The manifest a Zephyr workspace declares and the config that names it. The
# manifest decides where a component root lies, so a path out of it is joined
# onto this machine's filesystem, and both are read by the reader written here.
run ./internal/adapters/pkgmanager FuzzWestManifest
run ./internal/adapters/pkgmanager FuzzWestConfig

# The image manifest an embedded-Linux distribution build wrote about its image.
# It comes out of no build tree this tool has ever seen, and both of its formats
# are read by a parser written here.
run ./internal/adapters/pkgmanager FuzzDistroManifest

# The pkg-config file a distribution installs beside a system library. It comes
# out of a sysroot this tool did not build, and what it decides is not only a
# version but which component a file belongs to.
run ./internal/adapters/pkgmanager FuzzPkgConfig
