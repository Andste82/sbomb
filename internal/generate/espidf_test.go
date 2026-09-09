package generate

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/example/sbomb/internal/pathmodel"
)

// writeManagedComponent lays out what the ESP-IDF component manager leaves in a
// project: the lock file it wrote when it resolved the tree, and the component
// it unpacked into managed_components.
func writeManagedComponent(t *testing.T, root, header string) {
	t.Helper()
	writeTreeFile(t, filepath.Join(root, "dependencies.lock"),
		"dependencies:\n"+
			"  espressif/led_strip:\n"+
			"    component_hash: 5c9d1f\n"+
			"    source:\n"+
			"      registry_url: https://components.espressif.com/\n"+
			"      type: service\n"+
			"    version: 2.5.3\n"+
			"  idf:\n    source:\n      type: idf\n    version: 5.1.2\n"+
			"manifest_hash: 4f1e\ntarget: esp32\nversion: 1.0.0\n")
	writeTreeFile(t, filepath.Join(root, "managed_components", "espressif__led_strip", "idf_component.yml"),
		"version: \"2.5.3\"\nlicense: Apache-2.0\ntargets:\n  - esp32\n")
	writeTreeFile(t, header, "#define LED_STRIP 1\n")
}

// A header of a managed component that the compiler really read must arrive in
// that component -- with the version the lock recorded, the licence the
// component declares and the purl of section 20.4.
func TestAManagedESPIDFComponentReachesTheDocument(t *testing.T) {
	root := t.TempDir()
	header := filepath.Join(root, "managed_components", "espressif__led_strip", "led_strip.h")
	writeManagedComponent(t, root, header)
	cfg, buildDir := makeBuildTree(t, root, header)

	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	owner := ownerOf(result.Document, "pkg:idf/espressif/led_strip:led_strip.h")
	if owner != "component:espressif/led_strip" {
		t.Fatalf("the header belongs to %q, want the managed component", owner)
	}
	component, ok := componentByID(result.Document, "component:espressif/led_strip")
	if !ok {
		t.Fatal("the managed component the header belongs to reached no component")
	}
	if component.Version != "2.5.3" || component.PURL != "pkg:idf/espressif/led_strip@2.5.3" {
		t.Errorf("component = %+v, want the locked version and the purl of section 20.4", component)
	}
	if component.DetectedBy != "idf-component-manager" {
		t.Errorf("detectedBy = %q", component.DetectedBy)
	}
	if len(component.Licenses) != 1 || component.Licenses[0].Expression != "Apache-2.0" {
		t.Errorf("licenses = %+v, want the licence the component declares", component.Licenses)
	}
	if component.VersionSource != "idf" {
		t.Errorf("version source = %q, want the source string section 20.3 maps", component.VersionSource)
	}
	if reported := findingsWithID(result.Findings, "PACKAGE_NOT_LINKED"); len(reported) != 0 {
		t.Errorf("findings = %+v, want none: a file of the component was used", reported)
	}

	// The version evidence must render as a manifest read out of a file, not as
	// the "other" every unmapped source falls to.
	for _, entry := range result.BOM.Components {
		if entry.PURL != "pkg:idf/espressif/led_strip@2.5.3" {
			continue
		}
		if entry.Evidence == nil || len(entry.Evidence.Identity) != 1 {
			t.Fatalf("evidence = %+v, want one identity for the version", entry.Evidence)
		}
		methods := entry.Evidence.Identity[0].Methods
		if len(methods) != 1 || methods[0].Technique != "manifest-analysis" || methods[0].Value != "idf" {
			t.Fatalf("methods = %+v, want manifest-analysis with the exact source string", methods)
		}
		return
	}
	t.Fatal("the managed component did not reach the CycloneDX document")
}

// A component the manager installed but nothing linked is not part of the
// product, so it is absent -- and its absence is said out loud rather than
// passing in silence.
func TestAManagedComponentNothingLinkedIsAbsentAndReported(t *testing.T) {
	root := t.TempDir()
	header := filepath.Join(root, "managed_components", "espressif__led_strip", "led_strip.h")
	writeManagedComponent(t, root, header)
	// The build reads a header of the project's own, never the component's.
	own := filepath.Join(root, "main", "app.h")
	writeTreeFile(t, own, "#define APP 1\n")
	cfg, buildDir := makeBuildTree(t, root, own)

	result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := componentByID(result.Document, "component:espressif/led_strip"); ok {
		t.Error("a component nothing linked reached the document")
	}
	reported := findingsWithID(result.Findings, "PACKAGE_NOT_LINKED")
	if len(reported) != 1 || reported[0].Subject.Ref != "espressif/led_strip" {
		t.Fatalf("findings = %+v, want one PACKAGE_NOT_LINKED naming the component", reported)
	}
}

// The rule of internal/adapters/pkgmanager/pkgmanager.go, proven where it is
// visible: an adapter improves what is known about the files the evidence chain
// already reached, and never puts one there. Two runs over one project -- alike
// but for the lock file and the manifest the component manager wrote -- must
// therefore carry exactly the same files.
func TestTheComponentManagerEvidenceAddsNoFileToTheDocument(t *testing.T) {
	filesOf := func(t *testing.T, withEvidence bool) []string {
		t.Helper()
		root := t.TempDir()
		component := filepath.Join(root, "managed_components", "espressif__led_strip")
		header := filepath.Join(component, "led_strip.h")
		writeTreeFile(t, header, "#define LED_STRIP 1\n")
		// Files of the component the compiler never read. They exist, the
		// component manager put them there, and no evidence chain reaches
		// them -- so the document must not know about them either.
		writeTreeFile(t, filepath.Join(component, "led_strip.c"), "int led;\n")
		writeTreeFile(t, filepath.Join(component, "include", "unused.h"), "#define UNUSED 1\n")
		writeTreeFile(t, filepath.Join(component, "LICENSE"), "Apache-2.0\n")
		if withEvidence {
			writeManagedComponent(t, root, header)
		}
		cfg, buildDir := makeBuildTree(t, root, header)

		result, err := RunWithOptions(cfg, buildDir, true, Options{PathFlavor: pathmodel.PosixFlavor{}})
		if err != nil {
			t.Fatal(err)
		}
		// The anchor a file is expressed against does change once the component
		// is known -- that is the whole point of the adapter -- so the files are
		// compared by the name they carry on disk, which does not.
		names := make([]string, 0, len(result.Document.Files))
		for _, file := range result.Document.Files {
			names = append(names, filepath.Base(file.ID.RelPath))
		}
		sort.Strings(names)
		return names
	}

	without := filesOf(t, false)
	with := filesOf(t, true)
	if !reflect.DeepEqual(without, with) {
		t.Fatalf("the used set changed with the component manager's evidence:\nwithout %v\nwith    %v", without, with)
	}
	for _, name := range with {
		if name == "led_strip.c" || name == "unused.h" || name == "LICENSE" {
			t.Errorf("%q reached the document, but nothing linked it", name)
		}
	}
	if len(with) != 2 {
		t.Fatalf("files = %v, want exactly the source and the header the build read", with)
	}
}
