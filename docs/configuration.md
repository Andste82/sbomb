# Configuration

Configuration is JSON. The minimum useful project and build settings are:

```json
{
  "project": {"name": "app", "root": "."},
  "build": {"dir": "build"},
  "artifacts": [{"path": "build/app", "role": "application"}]
}
```

Use `sbomb schema` for the current schema document. `--policy` selects a
policy profile independently of the JSON configuration. Relative project and
artifact paths are resolved from the configured project root.