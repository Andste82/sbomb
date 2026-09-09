package pkgmanager

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

// The ESP-IDF lock file and component manifest get targets of their own rather
// than two more parameters of FuzzDiscover: the reader behind them is written
// in this repository instead of being a vendored parser, so it is the one that
// most needs to be shown surviving arbitrary bytes -- and a seven-argument fuzz
// function is unreadable.
func FuzzIDFLock(f *testing.F) {
	f.Add("dependencies:\n  espressif/led_strip:\n    source:\n      type: service\n    version: 2.5.3\n")
	f.Add("dependencies:\n  idf:\n    source:\n      type: idf\n    version: 5.1.2\n")
	f.Add("dependencies: {a: b}\n")
	f.Add("dependencies:\n  ../escape:\n    version: 1\n")
	f.Add("dependencies:\n\ta:\n")
	f.Add("")

	f.Fuzz(func(t *testing.T, lock string) {
		source := t.TempDir()
		if err := os.WriteFile(filepath.Join(source, idfLockName), []byte(lock), 0o600); err != nil {
			t.Skip(err)
		}
		if err := os.MkdirAll(filepath.Join(source, idfManagedDir, "espressif__led_strip"), 0o755); err != nil {
			t.Skip(err)
		}
		checkIDFPackages(t, source)
	})
}

func FuzzIDFManifest(f *testing.F) {
	f.Add("version: \"2.5.3\"\nlicense: Apache-2.0\n")
	f.Add("description: |\n  text\ntargets:\n  - esp32\n")
	f.Add("license: [MIT\n")
	f.Add("&anchor\n")
	f.Add("")

	f.Fuzz(func(t *testing.T, manifest string) {
		source := t.TempDir()
		root := filepath.Join(source, idfManagedDir, "espressif__led_strip")
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Skip(err)
		}
		if err := os.WriteFile(filepath.Join(root, idfManifestName), []byte(manifest), 0o600); err != nil {
			t.Skip(err)
		}
		checkIDFPackages(t, source)
	})
}

// checkIDFPackages states what must hold whatever those two files contain: a
// package names a component and a directory inside managed_components, and a
// purl of another type would be a claim about a package manager that installed
// nothing here.
func checkIDFPackages(t *testing.T, source string) {
	t.Helper()
	packages, _ := espidf{}.Discover(Options{SourceDir: source, Context: context.Background()})
	for _, entry := range packages {
		if entry.Name == "" {
			t.Fatal("a package without a name was returned")
		}
		if entry.Root() == "" {
			t.Fatalf("the package %q owns no directory", entry.Name)
		}
		if !strings.HasPrefix(entry.Root(), filepath.Join(source, idfManagedDir)+string(filepath.Separator)) {
			t.Fatalf("the root %q lies outside managed_components", entry.Root())
		}
		if entry.PURL.Value != "" && !strings.HasPrefix(entry.PURL.Value, "pkg:idf/") {
			t.Fatalf("purl %q is not of this adapter's type", entry.PURL.Value)
		}
	}
}

// The CPM lock gets a target of its own for the same reason the ESP-IDF files
// do: the reader behind it is written in this repository rather than vendored,
// and it is handed a file out of a build tree this tool did not create.
func FuzzCPMLock(f *testing.F) {
	f.Add("# CPM Package Lock\n\n# fmt\nCPMDeclarePackage(fmt\n  NAME fmt\n  VERSION 9.1.0\n  GITHUB_REPOSITORY fmtlib/fmt\n)\n")
	f.Add("# fmt (unversioned)\n# CPMDeclarePackage(fmt\n#  NAME fmt\n#  GIT_TAG main\n#)\n")
	f.Add("CPMDeclarePackage(")
	f.Add("CPMDeclarePackage(a\n  NAME ../../etc/passwd\n  GIT_REPOSITORY https://u:p@h/o/r.git\n)\n")
	f.Add("CPMDeclarePackage(a\n  VERSION 1\n)\nCPMDeclarePackage(a\n  VERSION 2\n)\n")
	f.Add("")

	f.Fuzz(func(t *testing.T, lock string) {
		build := t.TempDir()
		if err := os.WriteFile(filepath.Join(build, cpmLockName), []byte(lock), 0o600); err != nil {
			t.Skip(err)
		}
		if err := os.MkdirAll(filepath.Join(build, "_deps", "a-src"), 0o755); err != nil {
			t.Skip(err)
		}
		packages, _ := fetchContent{}.Discover(Options{BuildDir: build, Context: context.Background()})
		// Whatever the lock says, it may only describe the package the _deps
		// layout already named: no second package, and no name or root taken
		// out of the file itself.
		if len(packages) != 1 {
			t.Fatalf("packages = %#v, want the one the directory layout names", packages)
		}
		if packages[0].Name != "a" {
			t.Fatalf("the package was renamed to %q by the lock file", packages[0].Name)
		}
		if packages[0].Root() != filepath.Join(build, "_deps", "a-src") {
			t.Fatalf("the root %q is not the one the directory layout gives", packages[0].Root())
		}
		for _, claim := range []Claim{packages[0].Version, packages[0].PURL} {
			if claim.Value == "" && (claim.Source != "" || claim.Rank != RankNone) {
				t.Fatalf("a claim named an origin but no value: %#v", claim)
			}
		}
	})
}

// An image manifest is the one file in this package that comes out of no build
// tree at all: the user names it, a distribution build somewhere else wrote it,
// and both of its formats are read by a parser written here. Whatever is in it,
// the reader may return claims and findings and nothing else.
func FuzzDistroManifest(f *testing.F) {
	f.Add("PACKAGE NAME: busybox\nPACKAGE VERSION: 1.36.1\nRECIPE NAME: busybox\nLICENSE: GPL-2.0-only\n")
	f.Add("\"PACKAGE\",\"VERSION\",\"LICENSE\"\n\"busybox\",\"1.36.1\",\"GPL-2.0+, LGPL-2.1+\"\n")
	f.Add("\"PACKAGE\",\"VERSION\"\n\"a\"\n")
	f.Add("PACKAGE NAME:\nPACKAGE VERSION:\n\nPACKAGE NAME: a\n")
	f.Add("PACKAGE NAME: a\nPACKAGE VERSION: 1\n\nPACKAGE NAME: a\nPACKAGE VERSION: 2\n")
	f.Add("")

	f.Fuzz(func(t *testing.T, manifest string) {
		path := filepath.Join(t.TempDir(), "manifest")
		if err := os.WriteFile(path, []byte(manifest), 0o600); err != nil {
			t.Skip(err)
		}
		metadata, findings := ReadDistroManifests([]string{path})
		for _, finding := range findings {
			// A finding with no identifier reaches no catalogue, and one with
			// no subject names nothing a reader could look at.
			if finding.ID == "" || finding.Subject.Ref == "" {
				t.Fatalf("a finding names no identifier or no subject: %#v", finding)
			}
		}
		for _, source := range metadata.Sources() {
			for _, name := range []string{"a", "busybox", ""} {
				for _, contribution := range metadata.Describe(name) {
					// A claim with an origin but no value would publish
					// identity evidence for a version nobody stated, and a
					// value with no origin could be attributed to nothing.
					if contribution.Claim.Value == "" || contribution.Claim.Source == "" {
						t.Fatalf("%s described %q as %#v", source.Path, name, contribution)
					}
					if contribution.Claim.Rank != RankInstallState {
						t.Fatalf("a claim of rank %v came out of an image manifest", contribution.Claim.Rank)
					}
				}
			}
		}
	})
}
