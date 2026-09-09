package pkgmanager

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// The vcpkg SPDX document, the Conan CMakeDeps files, the FetchContent
// populate script, .gitmodules and an SBOM the upstream bundled all come from
// a build tree this tool did not create. Discover must survive whatever is in
// them.
func FuzzDiscover(f *testing.F) {
	f.Add(`{"packages":[{"name":"a","versionInfo":"1.0"}]}`,
		`set(PACKAGE_VERSION "1.0")`,
		`clone --no-checkout "url" "a-src"`+"\n"+`checkout "v1.0" --`,
		"[submodule \"dep/a\"]\n\tpath = dep/a\n\turl = git@h:o/r.git\n",
		`{"bomFormat":"CycloneDX","specVersion":"1.6","version":1,`+
			`"metadata":{"component":{"name":"a","version":"2.0","purl":"pkg:generic/a@2.0"}}}`)
	f.Add("{", "set(", "clone", "[submodule", "{")
	f.Add("", "", "", "", "")
	f.Add(`{"packages":[]}`, `set(PACKAGE_VERSION "")`, `checkout "" --`, "path =\n",
		`{"bomFormat":"CycloneDX","specVersion":"1.6","version":1,"metadata":{}}`)
	f.Add(`{"packages":[{"name":"a"}]}`, `set(`, `checkout`, "",
		`{"spdxVersion":"SPDX-2.3","packages":[]}`)

	f.Fuzz(func(t *testing.T, spdx, conanVersion, populate, gitmodules, bundled string) {
		build := t.TempDir()
		source := t.TempDir()

		share := filepath.Join(build, "vcpkg_installed", "x64-linux", "share", "a")
		if err := os.MkdirAll(share, 0o755); err != nil {
			t.Skip(err)
		}
		write := func(path, content string) {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Skip(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Skip(err)
			}
		}
		write(filepath.Join(share, "vcpkg.spdx.json"), spdx)
		// The bundled document lies in the package root the vcpkg adapter
		// settles, which is exactly where an enricher is offered a directory.
		write(filepath.Join(share, "sbom.cdx.json"), bundled)
		write(filepath.Join(build, "a-config-version.cmake"), conanVersion)
		write(filepath.Join(build, "a-release-x86_64-data.cmake"), conanVersion)
		write(filepath.Join(build, "_deps", "a-subbuild", "a-populate-prefix", "tmp", "a-populate-gitclone.cmake"), populate)
		write(filepath.Join(source, ".gitmodules"), gitmodules)

		packages, _ := Discover(Options{BuildDir: build, SourceDir: source, Context: context.Background()})
		for _, entry := range packages {
			// A package with no name owns no files and would name a component
			// after nothing.
			if entry.Name == "" {
				t.Fatal("a package without a name was returned")
			}
			if entry.PURL.Value != "" && len(entry.PURL.Value) < 5 {
				t.Fatalf("implausible purl %q", entry.PURL.Value)
			}
			// A claim with an origin but no value would publish identity
			// evidence for a version nobody stated, which is the one shape
			// Take exists to make impossible.
			for _, claim := range []Claim{entry.Version, entry.License, entry.Supplier, entry.PURL} {
				if claim.Value == "" && (claim.Source != "" || claim.Rank != RankNone) {
					t.Fatalf("a claim named an origin but no value: %#v", claim)
				}
			}
		}
	})
}
