#!/bin/sh
# Install sbomb.
#
#   curl -fsSL https://andste82.github.io/sbomb/install.sh | sh
#   curl -fsSL https://andste82.github.io/sbomb/install.sh | sh -s -- --version <sbomb-version>
#
# The checksum check is not optional and there is no flag to skip it. A tool
# whose whole argument is that you should be able to verify what you were given
# cannot hand you a binary it did not verify itself.
set -eu

REPO="Andste82/sbomb"
SITE="https://andste82.github.io/sbomb"

version="${SBOMB_VERSION:-latest}"
bin_dir="${SBOMB_BIN_DIR:-}"
with_sbom=0

usage() {
	cat <<EOF
Install sbomb, an evidence-based SBOM generator for CMake build artifacts.

Usage: install.sh [options]

  --version <v>   Release to install, with or without the leading v.
                  Default: latest.
  --bin-dir <d>   Where to put the binary. Default: /usr/local/bin if it is
                  writable, otherwise ~/.local/bin.
  --with-sbom     Also install the release's own CycloneDX document beside the
                  binary, so the thing you installed can be audited.
  --help          This text.

Environment: SBOMB_VERSION and SBOMB_BIN_DIR do the same as the flags.
More at $SITE
EOF
}

fail() {
	echo "install.sh: $1" >&2
	exit 1
}

# Fetch one file, telling the two kinds of failure apart.
#
# A 404 is an answer: the file is not in that release, and asking again will
# not change it. Anything else -- a 5xx, a reset connection, a timed-out
# request -- says nothing about the release and is worth another try. Reporting
# the second as the first is what makes a transient failure read as a broken
# release, and sends whoever hits it looking in the wrong place.
#
# curl's own --retry draws the same line: it repeats a transient failure and
# leaves a 404 alone.
fetch() {
	# $1 url, $2 destination, $3 what it is, for the message.
	code=$(curl -sL --retry 3 --retry-delay 2 -o "$2" -w '%{http_code}' "$1" 2>/dev/null) || code=""
	case "$code" in
	200)
		return 0
		;;
	404)
		fail "$3 is not there ($1); check that the release exists and carries this file"
		;;
	"")
		fail "$3 could not be downloaded, and the server gave no answer; the release itself may be fine, so this is worth retrying"
		;;
	*)
		fail "$3 could not be downloaded (HTTP $code); the release itself may be fine, so this is worth retrying"
		;;
	esac
}

need() {
	command -v "$1" >/dev/null 2>&1 || fail "this needs $1 and cannot find it"
}

while [ $# -gt 0 ]; do
	case "$1" in
	--version)
		[ $# -ge 2 ] || fail "--version needs a value"
		version="$2"
		shift 2
		;;
	--version=*)
		version="${1#--version=}"
		shift
		;;
	--bin-dir)
		[ $# -ge 2 ] || fail "--bin-dir needs a value"
		bin_dir="$2"
		shift 2
		;;
	--bin-dir=*)
		bin_dir="${1#--bin-dir=}"
		shift
		;;
	--with-sbom)
		with_sbom=1
		shift
		;;
	--help | -h)
		usage
		exit 0
		;;
	*)
		fail "unknown option: $1 (try --help)"
		;;
	esac
done

need curl
need uname
need mktemp

# The five platforms the release actually carries. An unsupported one is told
# so here, rather than through a 404 on a URL it could never have fetched.
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
x86_64 | amd64) arch="amd64" ;;
aarch64 | arm64) arch="arm64" ;;
esac
case "$os-$arch" in
linux-amd64 | linux-arm64 | darwin-amd64 | darwin-arm64) ;;
*)
	fail "no release for $os-$arch; sbomb publishes linux-amd64, linux-arm64, darwin-amd64, darwin-arm64, and windows-amd64 through install.ps1"
	;;
esac

# "latest" is resolved from where /releases/latest redirects to, not from the
# API. The API is rate limited per IP for unauthenticated callers, which a
# shared CI runner or an office behind one address will hit through no fault of
# its own -- and an installer that fails because somebody else installed too
# often is not an installer. The redirect has no such limit and needs no token.
if [ "$version" = "latest" ]; then
	tag=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" |
		sed -n 's#.*/releases/tag/##p')
	[ -n "$tag" ] || fail "could not work out the latest version; pass --version to name one"
else
	# A tag carries a leading v; accepting it without saves a support round.
	case "$version" in
	v*) tag="$version" ;;
	*) tag="v$version" ;;
	esac
fi

asset="sbomb-$os-$arch"
base="https://github.com/$REPO/releases/download/$tag"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

echo "install.sh: fetching sbomb $tag for $os-$arch"
fetch "$base/$asset" "$tmp/$asset" "$asset"
fetch "$base/SHA256SUMS" "$tmp/SHA256SUMS" SHA256SUMS

# sha256sum on Linux, shasum on macOS. Comparing the digest we compute against
# the line for this asset checks the file and not just that some line matched.
if command -v sha256sum >/dev/null 2>&1; then
	actual=$(sha256sum "$tmp/$asset" | cut -d ' ' -f 1)
elif command -v shasum >/dev/null 2>&1; then
	actual=$(shasum -a 256 "$tmp/$asset" | cut -d ' ' -f 1)
else
	fail "this needs sha256sum or shasum to verify the download and has neither"
fi
expected=$(sed -n "s/^\\([0-9a-f]\\{64\\}\\)[[:space:]][[:space:]]*[*]\\{0,1\\}$asset\$/\\1/p" "$tmp/SHA256SUMS" | head -n 1)
[ -n "$expected" ] || fail "SHA256SUMS names no checksum for $asset"
[ "$actual" = "$expected" ] || fail "checksum mismatch for $asset: expected $expected, got $actual"
echo "install.sh: checksum verified"

# /usr/local/bin when it is writable, ~/.local/bin otherwise. Never sudo on the
# user's behalf: a script fetched from the network does not get to decide that.
if [ -z "$bin_dir" ]; then
	if [ -w /usr/local/bin ] 2>/dev/null; then
		bin_dir="/usr/local/bin"
	else
		bin_dir="$HOME/.local/bin"
	fi
fi
mkdir -p "$bin_dir" || fail "cannot create $bin_dir"

install -m 0755 "$tmp/$asset" "$bin_dir/sbomb" 2>/dev/null ||
	{ cp "$tmp/$asset" "$bin_dir/sbomb" && chmod 0755 "$bin_dir/sbomb"; } ||
	fail "cannot write $bin_dir/sbomb; pass --bin-dir, or re-run with the rights to write there"

if [ "$with_sbom" -eq 1 ]; then
	fetch "$base/$asset.cdx.json" "$bin_dir/sbomb.cdx.json" "the SBOM for $asset"
	echo "install.sh: SBOM written to $bin_dir/sbomb.cdx.json"
fi

echo "install.sh: installed $("$bin_dir/sbomb" version) to $bin_dir/sbomb"

case ":${PATH}:" in
*":$bin_dir:"*) ;;
*)
	echo "install.sh: $bin_dir is not on your PATH; add it, for example:" >&2
	echo "    export PATH=\"$bin_dir:\$PATH\"" >&2
	;;
esac
