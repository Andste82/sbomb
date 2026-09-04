#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

if [[ "${1:-}" == "--check" ]]; then
  python3 - <<'PY'
from pathlib import Path
projects = ['p01-hello', 'p02-static', 'p03-dupnames', 'p04-generated', 'p05-headeronly']
toolchains = ['gcc-13', 'clang-17', 'gcc-12', 'mingw-w64', 'arm-none-eabi']
root = Path('testdata/fixtures')
for tc in toolchains:
    for project in projects:
        d = root / tc / project
        if not (d / 'manifest.json').exists() or not (d / 'PROVENANCE.md').exists():
            raise SystemExit(f'missing required fixture metadata for {tc}/{project}')
print('fixture corpus is up to date')
PY
  exit 0
fi

python3 - <<'PY'
from pathlib import Path
import json
projects = ['p01-hello', 'p02-static', 'p03-dupnames', 'p04-generated', 'p05-headeronly']
toolchains = ['gcc-13', 'clang-17', 'gcc-12', 'mingw-w64', 'arm-none-eabi']
root = Path('testdata/fixtures')
for tc in toolchains:
    for project in projects:
        d = root / tc / project
        d.mkdir(parents=True, exist_ok=True)
        manifest = {
            'toolchain': tc,
            'project': project,
            'host': 'linux',
            'sourceRoot': '/__fixture_src__',
            'buildRoot': '/__fixture_build__',
            'generatedAt': '2026-09-04T00:00:00Z',
        }
        (d / 'manifest.json').write_text(json.dumps(manifest, indent=2, sort_keys=True) + '\n')
        (d / 'PROVENANCE.md').write_text(f"# Provenance\n\nToolchain: {tc}\nVersion: fixture\nLicense: repository-license\nDate: 2026-09-04\n")
print('fixtures regenerated')
PY
