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

// The pkg-config file a distribution installs beside a system library. It comes
// out of a sysroot this tool did not build, its parser is written here, and the
// mapping it decides gives a component its name -- so whatever is in it, the
// reader may return a module and claims and nothing else, and must never map a
// file the metadata does not really describe.
func FuzzPkgConfig(f *testing.F) {
	f.Add("prefix=/usr\nexec_prefix=${prefix}\nlibdir=${prefix}/lib\n\nName: libfoo\nVersion: 1.2.3\nLibs: -L${libdir} -lfoo\nCflags: -I${prefix}/include\n")
	f.Add("root_prefix=/usr\nrootprefix=${root_prefix}\nprefix=${rootprefix}\nlibdir=${prefix}/lib\n\nName: systemd\nVersion: 255\n")
	f.Add("prefix=/usr\n\nName: shared-mime-info\nVersion: 2.4\nRequires:\nLibs:\nCflags:\n")
	f.Add("#############\n#  comment  #\n#############\n\nlibdir=/usr/lib\n\nName: libfoo\nVersion: 1\nLibs: -L ${libdir} -l foo\n")
	f.Add("a=${b}\nb=${a}\nlibdir=${a}\n\nName: a\nVersion: 1\n")
	f.Add("libdir=/usr/lib/../../etc\n\nName: a\nVersion: 1\nLibs: -L${libdir} -la\n")
	f.Add("libdir=$${literal}\n\nName: a\nVersion: $\nLibs: -L${nowhere} -la\n")
	f.Add("")

	f.Fuzz(func(t *testing.T, content string) {
		root := t.TempDir()
		library := filepath.Join(root, "usr", "lib", "libfoo.so")
		pc := filepath.Join(root, "usr", "lib", "pkgconfig", "libfoo.pc")
		for _, path := range []string{library, pc} {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Skip(err)
			}
		}
		if err := os.WriteFile(library, []byte("ELF\n"), 0o600); err != nil {
			t.Skip(err)
		}
		if err := os.WriteFile(pc, []byte(content), 0o600); err != nil {
			t.Skip(err)
		}

		described, findings := NewPkgConfigReader().Describe(SystemFile{
			Path: library, Boundary: root, Ref: "sysroot:target/usr/lib/libfoo.so",
		})
		for _, finding := range findings {
			// A finding with no identifier reaches no catalogue, and one with
			// no subject names nothing a reader could look at.
			if finding.ID == "" || finding.Subject.Ref == "" {
				t.Fatalf("a finding names no identifier or no subject: %#v", finding)
			}
			// Section 7.5: no report carries the absolute path of the machine
			// the run happened on.
			if strings.Contains(finding.Subject.Ref, root) {
				t.Fatalf("a finding names the absolute path %q", finding.Subject.Ref)
			}
		}
		if !described.Described() {
			if len(described.Contributions) != 0 {
				t.Fatalf("a file that was mapped to nothing still contributed %#v", described.Contributions)
			}
			return
		}
		// The module is the file's own name, because that is the only name this
		// reader ever addresses a .pc file by.
		if described.Module != "libfoo" {
			t.Fatalf("module = %q, want the name of the only file that could be addressed", described.Module)
		}
		for _, contribution := range described.Contributions {
			if contribution.Field != FieldVersion {
				t.Fatalf("a .pc file contributed %q, which it cannot state", contribution.Field)
			}
			if contribution.Claim.Value == "" || contribution.Claim.Source != pkgConfigSource {
				t.Fatalf("a claim with no value or a foreign origin: %#v", contribution.Claim)
			}
			if contribution.Claim.Rank != RankInstallState {
				t.Fatalf("a claim of rank %v came out of a pkg-config file", contribution.Claim.Rank)
			}
		}
	})
}

// The pack descriptor an MCU vendor ships. It is XML out of a tree this tool
// did not build, it is the first use of encoding/xml in this repository, and
// what keeps an entity bomb and an external entity harmless is four decoder
// fields that are deliberately never assigned -- an invisible property, and
// therefore the one most worth throwing arbitrary bytes at.
func FuzzCMSISPack(f *testing.F) {
	f.Add("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<package schemaVersion=\"1.7.7\">\n" +
		"  <vendor>ARM</vendor>\n  <name>CMSIS</name>\n  <license>LICENSE.txt</license>\n" +
		"  <releases>\n    <release version=\"5.9.0\" date=\"2022-05-02\">notes</release>\n" +
		"    <release version=\"5.8.0\">notes</release>\n  </releases>\n</package>\n")
	f.Add("<package><vendor>ARM</vendor><name>CMSIS</name><releases><release ver")
	f.Add("<!DOCTYPE package [<!ENTITY lol \"lol\"><!ENTITY lol1 \"&lol;&lol;\">]>\n" +
		"<package><vendor>&lol1;</vendor></package>")
	f.Add("<!DOCTYPE package [<!ENTITY xxe SYSTEM \"file:///etc/passwd\">]>\n" +
		"<package><vendor>&xxe;</vendor></package>")
	f.Add("<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><package><vendor>ARM</vendor></package>")
	f.Add("<package><releases><release version=\"\"/></releases></package>")
	f.Add("")

	f.Fuzz(func(t *testing.T, descriptor string) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "ARM.CMSIS.pdsc"), []byte(descriptor), 0o600); err != nil {
			t.Skip(err)
		}

		contributions, findings := cmsisPack{}.Enrich(ComponentRoot{Path: root, Name: "CMSIS"})
		for _, finding := range findings {
			// A finding with no identifier reaches no catalogue, and one with
			// no subject names nothing a reader could look at.
			if finding.ID == "" || finding.Subject.Ref == "" {
				t.Fatalf("a finding names no identifier or no subject: %#v", finding)
			}
			// Whole or not at all: a descriptor that was reported may not also
			// have contributed, because a value out of a file that could not be
			// read to its end is invented.
			if len(contributions) != 0 {
				t.Fatalf("%s was reported and %#v was still taken", finding.ID, contributions)
			}
		}
		for _, contribution := range contributions {
			if contribution.Claim.Value == "" || contribution.Claim.Rank == RankNone {
				t.Fatalf("a claim with no value or no origin: %#v", contribution)
			}
			// A descriptor states a version and a vendor. It states no licence
			// this tool will publish -- <license> names a file -- and there is
			// no purl type to build one from.
			switch contribution.Field {
			case FieldVersion, FieldSupplier:
			default:
				t.Fatalf("a pack descriptor contributed %q, which it cannot state", contribution.Field)
			}
		}
	})
}

// The manifest a Zephyr workspace declares, with the module descriptor a
// project ships beside it. Both are read by the reader written in this
// repository, and what the manifest decides is not only a version but where a
// component root lies -- a path out of somebody else's file, joined onto this
// machine's.
func FuzzWestManifest(f *testing.F) {
	f.Add("manifest:\n  remotes:\n    - name: up\n      url-base: https://example.invalid/org\n"+
		"  projects:\n    - name: hal\n      path: modules/hal\n      revision: v1.0.0\n      remote: up\n",
		"name: hal_module\n")
	f.Add("manifest:\n  projects:\n    - name: hal\n      path: ../../etc\n", "name: ../escape\n")
	f.Add("manifest:\n  projects:\n    - path: modules/hal\n", "name: [\n")
	f.Add("manifest:\n  projects:\n  - name: hal\n    url: https://u:p@example.invalid/org/hal.git\n", "")
	f.Add("manifest:\n  projects: none\n", "")
	f.Add("manifest:\n  projects:\n    - - a\n", "")
	f.Add("", "")

	f.Fuzz(func(t *testing.T, manifest, module string) {
		topdir := t.TempDir()
		write := func(path, content string) {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Skip(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Skip(err)
			}
		}
		write(filepath.Join(topdir, westDir, westConfigName), "[manifest]\npath = zephyr\nfile = west.yml\n")
		write(filepath.Join(topdir, "zephyr", westManifestFile), manifest)
		write(filepath.Join(topdir, "modules", "hal", westModuleDir, westModuleName), module)
		source := filepath.Join(topdir, "app")
		if err := os.MkdirAll(source, 0o755); err != nil {
			t.Skip(err)
		}
		checkWestPackages(t, topdir, source)
	})
}

// The config west writes when a workspace is initialised. It is the only
// statement about where the manifest lies, so whatever it holds must end either
// in a manifest inside the workspace or in nothing at all.
func FuzzWestConfig(f *testing.F) {
	f.Add("[manifest]\npath = zephyr\nfile = west.yml\n")
	f.Add("[manifest]\npath = ../elsewhere\n")
	f.Add("[manifest]\npath = /etc\nfile = passwd\n")
	f.Add("[zephyr]\nbase = zephyr\n")
	f.Add("[manifest]\npath = zephyr\npath = other\n")
	f.Add("")

	f.Fuzz(func(t *testing.T, config string) {
		topdir := t.TempDir()
		write := func(path, content string) {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Skip(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Skip(err)
			}
		}
		write(filepath.Join(topdir, westDir, westConfigName), config)
		write(filepath.Join(topdir, "zephyr", westManifestFile),
			"manifest:\n  projects:\n    - name: hal\n      path: modules/hal\n      revision: v1.0.0\n")
		if err := os.MkdirAll(filepath.Join(topdir, "modules", "hal"), 0o755); err != nil {
			t.Skip(err)
		}
		source := filepath.Join(topdir, "app")
		if err := os.MkdirAll(source, 0o755); err != nil {
			t.Skip(err)
		}
		checkWestPackages(t, topdir, source)
	})
}

// checkWestPackages states what must hold whatever those files contain: a
// package names a component and a directory inside the workspace, it carries a
// purl of the type this adapter builds and no other, and it never records a
// file -- west writes no file list, and inventing one would widen the used set.
func checkWestPackages(t *testing.T, topdir, source string) {
	t.Helper()
	packages, findings := west{}.Discover(Options{SourceDir: source, Context: context.Background()})
	for _, finding := range findings {
		// A finding with no identifier reaches no catalogue, and one with no
		// subject names nothing a reader could look at.
		if finding.ID == "" || finding.Subject.Ref == "" {
			t.Fatalf("a finding names no identifier or no subject: %#v", finding)
		}
	}
	for _, entry := range packages {
		if entry.Name == "" {
			t.Fatal("a package without a name was returned")
		}
		if entry.Root() == "" {
			t.Fatalf("the package %q owns no directory", entry.Name)
		}
		if !strings.HasPrefix(entry.Root(), topdir+string(filepath.Separator)) {
			t.Fatalf("the root %q lies outside the workspace", entry.Root())
		}
		if len(entry.Files) != 0 {
			t.Fatalf("the package %q claims %d file(s); west records none", entry.Name, len(entry.Files))
		}
		if entry.PURL.Value != "" && !strings.HasPrefix(entry.PURL.Value, "pkg:generic/") {
			t.Fatalf("purl %q is not of this adapter's type", entry.PURL.Value)
		}
		for _, claim := range []Claim{entry.Version, entry.License, entry.Supplier, entry.PURL} {
			if claim.Value == "" && (claim.Source != "" || claim.Rank != RankNone) {
				t.Fatalf("a claim named an origin but no value: %#v", claim)
			}
		}
	}
}
