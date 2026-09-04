#!/bin/sh
set -eu

output=${1:-testdata/large}
exec go run ./tools/generate-large --output "$output"