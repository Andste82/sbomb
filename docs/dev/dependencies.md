# Dependencies

Third-party Go modules are permitted (specification section 37). The binding
constraints are that every release binary builds with `CGO_ENABLED=0`, that no
module performs network I/O on the paths used, and that `vendor/` is committed
so a build needs no network at all.

Each direct dependency is listed with what it does and what removing it would
cost.

sbomb itself is under the MIT license. Every module below is under a permissive
license compatible with it, which is why `vendor/` can be committed and
redistributed with the source.

| Module | Purpose | Cost of removing it |
|---|---|---|
| `github.com/google/uuid` | Serial numbers: UUIDv4 in normal mode, UUIDv5 over the canonical document in reproducible mode (section 28.9). | Small. RFC 4122 v4 and v5 are a few dozen lines over `crypto/rand` and `crypto/sha1`. |
| `github.com/santhosh-tekuri/jsonschema/v6` | The first of the two validation layers section 32.5 requires: checking the generated document against the official CycloneDX JSON Schema. | Large. Hand-writing a JSON Schema evaluator is exactly the kind of drift risk the second layer exists to avoid. |
| `golang.org/x/text` | Required by the schema validator to render its messages. Not used directly beyond supplying its message printer. | None on its own; it comes with the validator. |

Not used, though section 37 suggests them:

- `github.com/CycloneDX/cyclonedx-go` — the document model is hand-written
  because section 29 requires this implementation to impose its own
  serialization order anyway, and the model is small enough that a second
  representation to convert into would add work rather than remove it. Revisit
  when a second output format arrives.
- `github.com/package-url/packageurl-go` — no purl is emitted yet. It becomes
  worthwhile with the package-manager adapters, where percent-encoding is easy
  to get subtly wrong.

Everything else -- depfile parsing, map parsing, Ninja parsing, DWARF, archive
reading, the path model -- stays on the standard library, and adding a
dependency for any of those requires a recorded deviation.

## Verifying

```sh
go build -mod=vendor ./...   # must succeed with no network
go mod verify
```
