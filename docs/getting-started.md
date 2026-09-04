# Getting Started

Build the CLI and run it against a configured build directory:

```sh
go build -o sbomb ./cmd/sbomb
./sbomb generate --build-dir build --config sbomb.json --output sbomb.cdx.json
```

For CMake projects, include `cmake/Sbomb.cmake`, call `sbomb_enable(TARGET app)`,
and build the report explicitly:

```sh
cmake -S . -B build -DSBOMB_EXECUTABLE="$PWD/sbomb"
cmake --build build
cmake --build build --target sbomb
```

The normal build never invokes sbomb. The explicit target produces one report
per enabled target in `SBOMB_OUTPUT_DIR`.