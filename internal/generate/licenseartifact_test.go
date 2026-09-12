package generate

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/testutil"
)

// fossDep is a dependency of the committed FOSS source tree, read where it
// lies. Retention is about bytes, so the test works on the same bytes the
// corpus ships rather than on a licence written by the test.
func fossDep(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(testutil.CorpusSourceTree(t), "dep", name)
}

// retain runs section 22.9 over one root, with the identity a component of
// that root would have.
func retain(t *testing.T, name, root string) retainedLicenses {
	t.Helper()
	resolver := &componentResolver{}
	return resolver.retainLicenseArtifacts(
		domain.FileID{Anchor: "project", RelPath: "dep/" + name}, root)
}

// The requirement in one test: MIT says the copyright notice and the
// permission notice "shall be included in all copies", and the notice is
// inside the text. The canonical SPDX text for MIT carries a placeholder where
// the holder belongs, so an identifier discharges nothing -- what has to
// survive is the component's own bytes, unchanged, with a digest a recipient
// can check them against.
func TestAMitLicenceIsRetainedByteForByteWithItsHolder(t *testing.T) {
	root := fossDep(t, "mit-lib")
	retained := retain(t, "mit-lib", root)

	if len(retained.artifacts) != 1 {
		t.Fatalf("retained %d artifact(s), want 1: %#v", len(retained.artifacts), retained.artifacts)
	}
	artifact := retained.artifacts[0]
	if artifact.Kind != domain.LicenseArtifactLicense {
		t.Errorf("kind = %q, want %q", artifact.Kind, domain.LicenseArtifactLicense)
	}
	if got := artifact.File.Canonical(); got != "project:dep/mit-lib/LICENSE" {
		t.Errorf("file = %q, want the canonical identity of the licence file", got)
	}

	onDisk, err := os.ReadFile(filepath.Join(root, "LICENSE"))
	if err != nil {
		t.Fatal(err)
	}
	if string(artifact.Bytes) != string(onDisk) {
		t.Error("the retained bytes are not the bytes of the file")
	}
	// Computed here, from the file, with nothing of the implementation in the
	// path: a digest the tool produced and checked against itself would assert
	// nothing.
	sum := sha256.Sum256(onDisk)
	if artifact.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("sha256 = %q, want %q", artifact.SHA256, hex.EncodeToString(sum[:]))
	}
	if !strings.Contains(string(artifact.Bytes), "Copyright (c) 2026 Fixture MIT Library Authors") {
		t.Error("the holder line is not in the retained bytes, which is the whole point of retaining them")
	}
	if artifact.DetectedID != "MIT" {
		t.Errorf("detected id = %q, want MIT", artifact.DetectedID)
	}
	if artifact.Technique == "" {
		t.Error("an identifier was detected and no technique of section 22.3 is named")
	}
}

// Apache-2.0 section 4(d) is a separate obligation from 4(a): the licence has
// to be handed on, and so do the attribution notices the NOTICE file carries.
// They are two kinds of artifact, and the NOTICE is not evidence of which
// licence applies.
func TestApacheLibRetainsTheLicenceAndTheNoticeAsTwoKinds(t *testing.T) {
	retained := retain(t, "apache-lib", fossDep(t, "apache-lib"))

	kinds := map[string]domain.LicenseArtifact{}
	for _, artifact := range retained.artifacts {
		kinds[artifact.Kind] = artifact
	}
	if len(kinds) != 2 {
		t.Fatalf("retained kinds = %v, want a licence and a notice", kinds)
	}
	grant, ok := kinds[domain.LicenseArtifactLicense]
	if !ok || grant.File.Canonical() != "project:dep/apache-lib/LICENSE" {
		t.Fatalf("licence artifact = %#v", grant)
	}
	notice, ok := kinds[domain.LicenseArtifactNotice]
	if !ok || notice.File.Canonical() != "project:dep/apache-lib/NOTICE" {
		t.Fatalf("notice artifact = %#v", notice)
	}
	if len(notice.Bytes) == 0 {
		t.Error("the notice was retained without its content")
	}
	// A notice carries no identifier, deliberately: recorded beside its bytes
	// it is one inference away from becoming the component's licence.
	if notice.DetectedID != "" || notice.Technique != "" {
		t.Errorf("the notice carries an identifier (%q/%q); section 22.9 forbids it", notice.DetectedID, notice.Technique)
	}
	// The grant answers step 5 of section 22.2; the notice answers nothing.
	found, resolved := licenseFromRetained(retained)
	if !resolved || found.Expression != "Apache-2.0" || found.Source != "LICENSE" {
		t.Errorf("step 5 answered %#v, want Apache-2.0 from LICENSE", found)
	}
}

// The defect this closes is the early return: the first licence file that
// resolved decided the identifier and the rest were never opened, so a
// component offering a choice of two shipped one of the two texts.
func TestBothLicencesOfADualLicensedComponentAreRetained(t *testing.T) {
	retained := retain(t, "multi-license", fossDep(t, "multi-license"))

	var files []string
	for _, artifact := range retained.artifacts {
		if artifact.Kind != domain.LicenseArtifactLicense {
			t.Errorf("%s was retained as %q", artifact.File.Canonical(), artifact.Kind)
		}
		if len(artifact.Bytes) == 0 {
			t.Errorf("%s was retained empty", artifact.File.Canonical())
		}
		files = append(files, artifact.File.Canonical())
	}
	want := "project:dep/multi-license/LICENSE-APACHE,project:dep/multi-license/LICENSE-MIT"
	if strings.Join(files, ",") != want {
		t.Errorf("retained %v, want both texts", files)
	}
	if len(retained.dropped) != 0 {
		t.Errorf("something was dropped: %v", retained.dropped)
	}
}

// Section 22.2 no longer consults a NOTICE, and this is the case that made it
// wrong: the file recites a complete licence text that is not the component's
// own. Reading it produced a licence with high confidence, or -- via the
// composition path -- a NOASSERTION that named licences nobody granted here.
func TestANoticeDecidesNoLicenceAtAll(t *testing.T) {
	mit, err := os.ReadFile(filepath.Join(fossDep(t, "mit-lib"), "LICENSE"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	write(t, filepath.Join(root, "NOTICE"), string(mit))

	retained := retain(t, "quoter", root)
	if len(retained.artifacts) != 1 || retained.artifacts[0].Kind != domain.LicenseArtifactNotice {
		t.Fatalf("retained %#v, want one notice", retained.artifacts)
	}
	if _, resolved := licenseFromRetained(retained); resolved {
		t.Error("a NOTICE answered step 5 of section 22.2")
	}
	if observed := licenseEvidenceFromRetained(retained); len(observed) > 0 {
		t.Errorf("a NOTICE produced licence evidence: %#v", observed)
	}
}

// Section 22.9 and requirement R10: the identifier is there, the text is not,
// and that is exactly the case where the attribution obligation cannot be
// satisfied from what the tool saw. It is stated where the entry would have
// been instead of being left to be discovered by a recipient.
func TestALicenceWithoutItsTextIsReported(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "dep", "headeronly", "src", "only.c")
	write(t, source, "// SPDX-License-Identifier: MIT\nint only(void){return 0;}\n")
	// A licence file makes the directory a component (section 19.2), and it is
	// deliberately not in the root of the component: the grant that names the
	// component's boundary is the one whose bytes are retained.
	write(t, filepath.Join(root, "dep", "headeronly", "LICENSE-MIT"), "SPDX-License-Identifier: MIT\n")

	file := domain.UsedFile{ID: fileID("project", "dep/headeronly/src/only.c")}
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		map[string]string{file.ID.Canonical(): source}, map[string]string{"project": root}, nil)
	component := &domain.Component{ID: "component:headeronly", Name: "headeronly", DetectedBy: "package-metadata:LICENSE-MIT", DistributionRole: domain.RoleDistributed}
	findings := resolver.enrichComponent(component, []domain.UsedFile{file})

	if hasFindingID(findings, "FOSS_LICENSE_TEXT_MISSING") {
		t.Errorf("the text is retained and the finding fired anyway: %v", findingIDs(findings))
	}
	if len(component.LicenseArtifacts) != 1 {
		t.Fatalf("retained %#v, want the LICENSE-MIT of the root", component.LicenseArtifacts)
	}

	// The same component with the grant removed: the header still says MIT, so
	// the identifier survives and the text does not.
	if err := os.Remove(filepath.Join(root, "dep", "headeronly", "LICENSE-MIT")); err != nil {
		t.Fatal(err)
	}
	bare := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		map[string]string{file.ID.Canonical(): source}, map[string]string{"project": root}, nil)
	bareComponent := &domain.Component{ID: "component:headeronly", Name: "headeronly", DistributionRole: domain.RoleDistributed}
	findings = bare.enrichComponent(bareComponent, []domain.UsedFile{file})
	if len(bareComponent.LicenseArtifacts) != 0 {
		t.Errorf("retained %#v from a root that carries nothing", bareComponent.LicenseArtifacts)
	}
	if !hasFindingID(findings, "FOSS_LICENSE_TEXT_MISSING") {
		t.Errorf("findings = %v, want FOSS_LICENSE_TEXT_MISSING", findingIDs(findings))
	}
}

// The counterpart: no text and no identifier either. There is nothing to
// attribute yet, and UNKNOWN_LICENSE already says so -- a second finding
// saying the text of an unknown licence is missing would be noise.
func TestAComponentWithNoLicenceAtAllReportsOnlyTheUnknownLicence(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "dep", "nolicense", "src", "nolicense.c")
	write(t, source, "int nolicense(void){return 0;}\n")
	write(t, filepath.Join(root, "dep", "nolicense", "vcpkg.json"), "{}\n")

	file := domain.UsedFile{ID: fileID("project", "dep/nolicense/src/nolicense.c")}
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		map[string]string{file.ID.Canonical(): source}, map[string]string{"project": root}, nil)
	component := &domain.Component{ID: "component:nolicense", Name: "nolicense", DetectedBy: "package-metadata:vcpkg.json"}
	findings := resolver.enrichComponent(component, []domain.UsedFile{file})

	if len(component.LicenseArtifacts) != 0 {
		t.Errorf("retained %#v from a component that carries no licence file", component.LicenseArtifacts)
	}
	if hasFindingID(findings, "FOSS_LICENSE_TEXT_MISSING") {
		t.Errorf("findings = %v; there is no identifier, so nothing is missing", findingIDs(findings))
	}
	if !hasFindingID(findings, "UNKNOWN_LICENSE") {
		t.Errorf("findings = %v, want UNKNOWN_LICENSE", findingIDs(findings))
	}
}

// A truncated licence is not a licence. The limits of section 22.9 therefore
// bound the list and never the file: the oversized entry is left out and named
// in a finding, and what is kept is whole.
func TestAnOversizedLicenceIsDroppedAndTheRestIsKeptWhole(t *testing.T) {
	root := t.TempDir()
	huge := strings.Repeat("A", 2<<20)
	write(t, filepath.Join(root, "LICENSE"), huge)
	write(t, filepath.Join(root, "NOTICE"), "Copyright (c) 2026 Example Holder\n")

	retained := retain(t, "huge", root)
	if len(retained.artifacts) != 1 || retained.artifacts[0].Kind != domain.LicenseArtifactNotice {
		t.Fatalf("retained %#v, want the notice alone", retained.artifacts)
	}
	if string(retained.artifacts[0].Bytes) != "Copyright (c) 2026 Example Holder\n" {
		t.Error("the retained notice is not complete")
	}
	if len(retained.dropped) != 1 || !strings.Contains(retained.dropped[0], "LICENSE") {
		t.Fatalf("dropped = %v, want the oversized LICENSE named", retained.dropped)
	}
	if !strings.Contains(retained.dropped[0], "over the limit") {
		t.Errorf("dropped = %q, want the reason stated", retained.dropped[0])
	}
}

// The count limit, and the order it is applied in: the consultation order of
// section 22.3 puts the grants first, so a root with more attribution material
// than the limit loses the material rather than the licence.
func TestMoreLicenceFilesThanTheLimitBoundTheList(t *testing.T) {
	root := t.TempDir()
	names := []string{
		"LICENSE", "LICENSE-MIT", "LICENSE-Apache-2.0", "LICENSE-BSD",
		"LICENSE-ISC", "LICENSE-Zlib", "LICENSE-0BSD", "COPYING",
		"NOTICE", "COPYRIGHT",
	}
	for _, name := range names {
		write(t, filepath.Join(root, name), "text of "+name+"\n")
	}

	retained := retain(t, "many", root)
	if len(retained.artifacts) != maxLicenseArtifacts {
		t.Fatalf("retained %d artifact(s), want the limit of %d", len(retained.artifacts), maxLicenseArtifacts)
	}
	for _, artifact := range retained.artifacts {
		if artifact.Kind != domain.LicenseArtifactLicense {
			t.Errorf("%s survived the limit while a grant did not", artifact.File.Canonical())
		}
	}
	if len(retained.dropped) != 2 {
		t.Fatalf("dropped = %v, want the two files past the limit", retained.dropped)
	}
	for _, dropped := range retained.dropped {
		if !strings.Contains(dropped, "past the limit") {
			t.Errorf("dropped = %q, want the reason stated", dropped)
		}
	}
}

// A claim about reads that nothing counts is not a claim. Every read retention
// causes goes through one counter, and this asserts both the count and the set
// of paths: what retention opens is the recognized licence files of the
// settled component root, which is exactly what section 23 admits beyond the
// evidence-selected files ("plus license files of mapped components").
func TestRetentionReadsEachFileOnceAndNothingButThePermittedSet(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "dep", "lib", "src", "lib.c")
	write(t, source, "// SPDX-License-Identifier: MIT\nint lib(void){return 0;}\n")
	write(t, filepath.Join(root, "dep", "lib", "LICENSE"), "SPDX-License-Identifier: MIT\n")
	write(t, filepath.Join(root, "dep", "lib", "NOTICE"), "Copyright (c) 2026 Example Holder\n")
	// Neither of these may be opened: one is not evidence-selected, the other
	// is a licence file of a directory that is not this component's root.
	write(t, filepath.Join(root, "dep", "lib", "src", "unused.c"), "int unused(void){return 0;}\n")
	write(t, filepath.Join(root, "LICENSE"), "SPDX-License-Identifier: GPL-2.0-only\n")

	file := domain.UsedFile{ID: fileID("project", "dep/lib/src/lib.c")}
	resolver := newComponentResolver(config.Config{Project: config.Project{Name: "firmware"}},
		map[string]string{file.ID.Canonical(): source}, map[string]string{"project": root}, nil)
	component := &domain.Component{ID: "component:lib", Name: "lib", DetectedBy: "package-metadata:LICENSE"}
	resolver.enrichComponent(component, []domain.UsedFile{file})

	// Exactly the two licence files of the component root: not the LICENSE of
	// the enclosing directory, which belongs to another component, and not the
	// source that no evidence selected.
	want := map[string]bool{
		filepath.Join(root, "dep", "lib", "LICENSE"): true,
		filepath.Join(root, "dep", "lib", "NOTICE"):  true,
	}
	if len(resolver.licenseReads) != len(want) {
		t.Fatalf("retention opened %v, want %v", resolver.licenseReads, want)
	}
	for path, count := range resolver.licenseReads {
		if !want[path] {
			t.Errorf("%s was opened by retention; section 23 does not permit it", path)
		}
		if count != 1 {
			t.Errorf("%s was opened %d times, want once", path, count)
		}
	}

	// And a second component resolving to the same root reads nothing again.
	second := &domain.Component{ID: "component:lib-again", Name: "lib", DetectedBy: "package-metadata:LICENSE"}
	resolver.enrichComponent(second, []domain.UsedFile{file})
	for path, count := range resolver.licenseReads {
		if count != 1 {
			t.Errorf("%s was opened %d times across two components, want once", path, count)
		}
	}
	if len(second.LicenseArtifacts) != 2 {
		t.Errorf("the second component retained %#v, want the same two artifacts", second.LicenseArtifacts)
	}
	// And retention really did keep both files, so the counts above are the
	// cost of the whole feature and not of a code path that did nothing.
	if len(component.LicenseArtifacts) != 2 {
		t.Errorf("retained %#v, want the licence and the notice", component.LicenseArtifacts)
	}
}

// The stored order is a document's order, not a resolution order. Section 29
// fixes it at (kind, canonical path) so that two runs over one evidence set
// produce one document; the consultation order of section 22.3 is a different
// question, answered above.
func TestRetainedArtifactsAreStoredByKindThenPath(t *testing.T) {
	unsorted := []domain.LicenseArtifact{
		{Kind: domain.LicenseArtifactNotice, File: domain.FileID{Anchor: "project", RelPath: "d/NOTICE"}},
		{Kind: domain.LicenseArtifactLicense, File: domain.FileID{Anchor: "project", RelPath: "d/LICENSE-MIT"}},
		{Kind: domain.LicenseArtifactLicense, File: domain.FileID{Anchor: "project", RelPath: "d/LICENSE-APACHE"}},
		{Kind: domain.LicenseArtifactCopyright, File: domain.FileID{Anchor: "project", RelPath: "d/COPYRIGHT"}},
	}
	var got []string
	for _, artifact := range sortedLicenseArtifacts(unsorted) {
		got = append(got, artifact.Kind+" "+artifact.File.Canonical())
	}
	want := strings.Join([]string{
		"copyright project:d/COPYRIGHT",
		"license project:d/LICENSE-APACHE",
		"license project:d/LICENSE-MIT",
		"notice project:d/NOTICE",
	}, ",")
	if strings.Join(got, ",") != want {
		t.Errorf("stored order = %v", got)
	}
}

func hasFindingID(findings []domain.Finding, id string) bool {
	for _, finding := range findings {
		if finding.ID == id {
			return true
		}
	}
	return false
}

func findingIDs(findings []domain.Finding) []string {
	ids := make([]string, 0, len(findings))
	for _, finding := range findings {
		ids = append(ids, finding.ID)
	}
	return ids
}
