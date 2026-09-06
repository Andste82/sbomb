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

Components carry the metadata the CRA requires. Nothing here is guessed: a
version, a supplier or a license that no authorized source supplies is reported
as missing rather than inferred from a directory name.

```json
{
  "components": [
    {
      "path": "dep/mbedtls",
      "name": "mbedtls",
      "type": "library",
      "version": "3.5.0",
      "supplier": "Trusted Firmware",
      "license": "Apache-2.0"
    }
  ]
}
```

Files are mapped to components in the priority order of the specification:
curated entries first, then the nearest ancestor directory carrying a package
manifest (`conanfile.txt`, `vcpkg.json`, `idf_component.yml`, `Cargo.toml`,
`west.yml`), then the anchor root, and finally an explicit `unknown:` component
that is flagged for review. A file is never dropped because its component could
not be determined.

Use `sbomb schema` for the current schema document. `--policy` selects a
policy profile independently of the JSON configuration. Relative project and
artifact paths are resolved from the configured project root.