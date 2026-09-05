#!/usr/bin/env bash
set -euo pipefail

root_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
version=${VERSION:-$(git -C "$root_dir" describe --tags --always --dirty 2>/dev/null || printf 'dev')}
output_dir=${OUTPUT_DIR:-"$root_dir/dist/$version"}
ldflags="-s -w -X github.com/example/sbomb/internal/buildinfo.Version=$version"

build_one() {
  local goos=$1 goarch=$2 suffix=$3
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
    -trimpath -buildvcs=false -ldflags "$ldflags" -o "$output_dir/sbomb-${goos}-${goarch}${suffix}" ./cmd/sbomb
}

write_self_config() {
  local build_dir=$1 artifact=$2 config=$3
  cat > "$config" <<EOF
{"project":{"name":"sbomb","root":"$root_dir"},"build":{"dir":"$build_dir"},"artifacts":[{"path":"$artifact","role":"application"}]}
EOF
  cat > "$build_dir/compile_commands.json" <<EOF
[{"directory":"$root_dir","file":"$root_dir/cmd/sbomb/main.go","output":"$build_dir/sbomb-main.o","arguments":["go","tool","compile","-o","$build_dir/sbomb-main.o","$root_dir/cmd/sbomb/main.go"]}]
EOF
  : > "$build_dir/link.map"
}

write_self_sbom() {
  local generator=$1 artifact=$2 output=$3
  local build_dir config
  build_dir=$(mktemp -d)
  config=$(mktemp)
  trap 'rm -rf "$build_dir" "$config"' RETURN
  write_self_config "$build_dir" "$artifact" "$config"
  "$generator" generate --build-dir "$build_dir" --config "$config" --output "$output" --reproducible
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
    write_self_sbom "$output_dir/sbomb-linux-amd64" "$output_dir/sbomb-linux-amd64" "$output_dir/sbomb-linux-amd64.cdx.json"
    write_self_sbom "$output_dir/sbomb-linux-amd64" "$output_dir/sbomb-linux-arm64" "$output_dir/sbomb-linux-arm64.cdx.json"
    write_self_sbom "$output_dir/sbomb-linux-amd64" "$output_dir/sbomb-windows-amd64.exe" "$output_dir/sbomb-windows-amd64.cdx.json"
    (cd "$output_dir" && sha256sum sbomb-* > SHA256SUMS)
    ;;
  *)
    printf 'usage: scripts/release.sh [build|--check-reproducible]\n' >&2
    exit 1
    ;;
esac