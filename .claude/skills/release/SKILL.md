---
name: release
description: Cut and publish an sbomb release — decide the version from the changes, run the full gate, tag, and verify what CI published. Use when asked to make a release, cut a version, tag a release, or publish a build.
---

# Releasing sbomb

A release is a public, irreversible act: it publishes binaries under the
maintainer's name and other people build against them. Everything below is
verification before that point, and the two steps that touch the outside world
are the last two.

## 1. Decide the version

**If the user named a version, use it. Do not argue with it.**

Otherwise derive it from what changed since the last tag:

```bash
git describe --tags --abbrev=0          # the last release
git log --oneline "$(git describe --tags --abbrev=0)"..HEAD
sed -n '/^## Unreleased/,/^## [0-9]/p' docs/CHANGELOG.md
```

sbomb is pre-1.0, so the middle number carries features:

| What landed since the last tag | Bump |
|---|---|
| A new subcommand, flag, output field, or detection technique | minor — `0.9.0` → `0.10.0` |
| Only fixes, docs, or internal work | patch — `0.9.0` → `0.9.1` |
| A change that breaks an existing SBOM's shape or a CLI contract | minor, and say so in the release notes |

State the number and the one-line reason before doing anything else, so the
user can override it.

## 2. Bump the version in the two places that carry it

```bash
# internal/buildinfo/buildinfo.go — the tool's own identity
var Version = "0.10.0"
```

```bash
# docs/CHANGELOG.md — the "## Unreleased" heading becomes the version
## 0.10.0
```

Nothing else hard-codes a version. The release script derives it from the git
tag and strips the leading `v`, so the binary reports `0.10.0` where the tag
says `v0.10.0`.

## 3. Run the full gate

Every step. A step that cannot run is a deviation to record, not a step to
skip. The list lives in `docs/dev/README.md`; this is it plus the release's own
two checks:

```bash
go build ./...
go build -mod=vendor ./...                 # builds with no network
go vet ./...
gofmt -l $(go list -f '{{.Dir}}' ./...)    # must print nothing
go test ./...
CGO_ENABLED=1 go test ./... -race
go test -count=1 -tags e2e ./test/...      # needs a real toolchain
tools/fixtures/regen.sh --check
scripts/determinism-check.sh
go run ./tools/findingsdoc --check
go run ./tools/spdxgen --check             # needs network; informational
```

Then the release build itself, locally, before any tag exists:

```bash
rm -rf dist
VERSION=v0.10.0 scripts/release.sh --check-reproducible
VERSION=v0.10.0 scripts/release.sh build
ls dist/0.10.0/
```

Expect **twelve** files: five binaries — linux/amd64, linux/arm64,
windows/amd64, darwin/amd64, darwin/arm64 — a `.cdx.json` beside each,
`sbomb-cmake.tar.gz` (the CMake bundle FetchContent pulls, with this version
substituted into its fetcher), and `SHA256SUMS` covering all eleven. Check the self-SBOM is not empty of licences:

```bash
python3 -c "
import json; d=json.load(open('dist/0.10.0/sbomb-linux-amd64.cdx.json'))
print(d['metadata']['component']['version'], d['metadata']['component']['licenses'])
for c in d['components']: print(' -', c['name'], c['licenses'])
"
rm -rf dist
```

`dist/` is gitignored, but remove it anyway so nothing stray reaches the commit.

## 4. Commit and tag

```bash
git add -A
git commit -m "Release 0.10.0"
git tag -a v0.10.0 -m "sbomb 0.10.0"
```

The version commit is its own commit. It has been swept into an unrelated one
before by `git add -A`; if that happens and nothing is pushed yet, split it
with `git reset --soft` rather than leaving it.

## 5. Push — this publishes

Pushing the tag starts `release.yaml`, which creates the GitHub release and
uploads the artifacts. There is no draft step and no undo.

```bash
git push
git push origin v0.10.0
```

The repository is private, so `gh` is not installed in the devcontainer and the
push needs the token from `.devcontainer/.env` through an inline credential
helper. **The token must never be written to `.git/config`, `~/.git-credentials`,
a command line, or any output.**

```bash
set -a; . .devcontainer/.env; set +a
git -c credential.helper='!f(){ echo "username=x-access-token"; echo "password=$GITHUB_TOKEN"; }; f' push
git -c credential.helper='!f(){ echo "username=x-access-token"; echo "password=$GITHUB_TOKEN"; }; f' push origin v0.10.0
```

**The tagged commit has to reach `main` unchanged.** If the release goes
through a branch and a pull request, merge it so that the commit keeps its
hash: a merge commit, or a fast-forward. **Never rebase or squash a pull
request that carries the release commit** — that writes a new commit with the
same content, the tag stays behind on the old one, and `main` no longer
contains the tag at all:

```bash
git describe --tags origin/main   # must name the release just cut
```

If it names the release before it, the tag is off the branch. Do not move a
published tag to repair it: the assets were built from the commit the tag names,
and after a rebase the two trees are not even the same. Cut the next release
from `main` instead and let it settle there.

## 6. Verify what CI actually published

Three workflows run on the tag: `ci`, `determinism` and `release`. `release`
publishes and then calls `smoke-test`, which downloads the published binary on
Linux and Windows and checks it produces the right components from the
committed fixture.

```bash
set -a; . .devcontainer/.env; set +a
curl -sS -H "Authorization: Bearer $GITHUB_TOKEN" \
  "https://api.github.com/repos/Andste82/sbomb/actions/runs?per_page=10" |
  python3 -c "
import json,sys
for r in json.load(sys.stdin)['workflow_runs'][:8]:
    print(r['name'], r['head_branch'], r['status'], r['conclusion'])
"
```

Then confirm the release carries all twelve assets:

```bash
curl -sS -H "Authorization: Bearer $GITHUB_TOKEN" \
  "https://api.github.com/repos/Andste82/sbomb/releases/tags/v0.10.0" |
  python3 -c "
import json,sys; d=json.load(sys.stdin)
print(d['name'], d['html_url'])
for a in d['assets']: print(' -', a['name'], a['size'])
"
```

Report the URL and the asset list. A release whose workflows are red is not
finished; say so plainly rather than reporting the tag as done.

## What has gone wrong before

Each of these cost a release, and each is now checked above.

- **The release script had never run in CI.** It fabricated a self-SBOM, which
  tripped the policy, and `set -e` aborted before `SHA256SUMS` was written.
  Deviation D16. Hence step 3 runs `scripts/release.sh build` locally.
- **The tool did not build for Windows at all.** `internal/limits` used
  `syscall.O_NOFOLLOW`, which does not exist there, so every cross-compile
  failed. Only the full five-target build shows this.
- **The binary reported `sbomb v0.9.0`** where a development build reported
  `0.9.0`, because the `v` belongs to the tag and not to the version.
- **`smoke-test` could never pass.** First it raced the workflow that creates
  the release it downloads; then, on `release: published`, it never started at
  all, because GitHub does not run workflows from events the default token
  created. It is called by `release.yaml` now.
- **An unauthenticated asset download answers 404, not 403,** while the
  repository is private — which reads like a missing tag. The composite action
  takes a token for that reason.
- **v0.13.0 ended up tagged on a commit that is not on `main`.** The release
  went out from a branch, and the pull request that brought it to `main` was
  merged with rebase. The content arrived; the commit did not. `git describe`
  on `main` answered `v0.12.0-5-g…`, so `main` believed the last release was
  the one before. Worse, the rebase put another commit underneath, so the
  tagged tree and `main`'s tree differ in `cmake/SbombFetch.cmake` — a file
  that ships inside `sbomb-cmake.tar.gz`. Moving the tag would have made it
  name content the published asset does not contain. Hence the check in step 5.

## Not part of a release

Do not bump the version, tag, or push as a side effect of other work. A release
is asked for.
