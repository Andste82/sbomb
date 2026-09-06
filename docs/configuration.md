# Configuration

Configuration is JSON. The minimum useful project and build settings are:

```json
{
  "project": {"name": "app", "root": "."},
  "build": {"dir": "build"},
  "artifacts": [{"path": "build/app", "role": "application"}]
}
```

External directories are given stable, portable identities with `anchors`:

```json
{
  "anchors": [
    {"key": "shared", "path": "/opt/shared"},
    {"key": "sdk:espidf", "path": "/opt/esp-idf"}
  ]
}
```

A bare key becomes an `extern:` anchor, so `shared` identifies files as
`extern:shared:<relative path>`. Keys that already name a kind -- `sdk:`,
`pkg:`, `toolchain:`, `sysroot:`, `extern:` -- are used verbatim. The project
and build roots, the compiler installation and the sysroot are anchored
automatically from the CMake File API. Files under no anchor are identified by
absolute path and reported as `UNANCHORED_FILE`; `--redact-unanchored-paths`
replaces those paths with a digest.

Use `sbomb schema` for the current schema document. `--policy` selects a
policy profile independently of the JSON configuration. Relative project and
artifact paths are resolved from the configured project root.