package generate

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/license"
	"github.com/example/sbomb/internal/testutil"
)

// copyrightOf runs the description of one component over one used file, with
// the statements the hashing pass would have observed handed in exactly as
// generate hands them in.
func copyrightOf(t *testing.T, root, name string, files []domain.UsedFile,
	observed map[string][]string, curated config.Component) (*domain.Component, []domain.Finding) {
	t.Helper()
	physical := map[string]string{}
	for _, file := range files {
		physical[file.ID.Canonical()] = filepath.Join(root, file.ID.RelPath)
	}
	cfg := config.Config{Project: config.Project{Name: "firmware"}}
	if curated.Path != "" {
		cfg.Components = []config.Component{curated}
	}
	resolver := newComponentResolver(cfg, physical, map[string]string{"project": root}, nil)
	resolver.setCopyrightStatements(observed)
	component := &domain.Component{ID: "component:" + name, Name: name,
		DetectedBy: "package-metadata:LICENSE"}
	findings := resolver.enrichComponent(component, files)
	return component, findings
}

func copyrightTexts(component *domain.Component) []string {
	out := make([]string, 0, len(component.Copyrights))
	for _, statement := range component.Copyrights {
		out = append(out, statement.Text)
	}
	return out
}

// For a BSD dependency the notice is inside the licence text and nowhere else:
// the header carries an identifier and no holder. So the retained artifacts of
// section 22.9 are a source of section 22.10, and this is the component that
// proves it -- on the corpus' own bytes.
func TestTheHolderOfABSDDependencyComesFromItsLicenceText(t *testing.T) {
	root := testutil.CorpusSourceTree(t)
	file := domain.UsedFile{ID: fileID("project", "dep/bsd-hdr/include/bsd_hdr.h")}
	component, findings := copyrightOf(t, root, "bsd-hdr", []domain.UsedFile{file},
		map[string][]string{}, config.Component{})

	want := "Copyright (c) 2026, Fixture BSD Header Authors"
	if got := copyrightTexts(component); len(got) != 1 || got[0] != want {
		t.Fatalf("statements = %q, want [%q] read out of the retained LICENSE", got, want)
	}
	if component.Copyrights[0].File.Canonical() != "project:dep/bsd-hdr/LICENSE" {
		t.Errorf("statement came from %q, want the licence file it is written in",
			component.Copyrights[0].File.Canonical())
	}
	if hasFindingID(findings, "FOSS_COPYRIGHT_MISSING") {
		t.Errorf("findings = %v; the holder is there", findingIDs(findings))
	}
}

// A component that states nothing says so. `nocopyright` of the fixture is
// 0BSD with no notice anywhere -- neither in its sources nor in its licence
// file -- which is a real shape and not an invented one.
func TestAComponentWithNoStatementReportsIt(t *testing.T) {
	root := testutil.CorpusSourceTree(t)
	file := domain.UsedFile{ID: fileID("project", "dep/nocopyright/src/nocopyright.c")}
	component, findings := copyrightOf(t, root, "nocopyright", []domain.UsedFile{file},
		map[string][]string{}, config.Component{})

	if len(component.Copyrights) != 0 {
		t.Fatalf("statements = %q, want none", copyrightTexts(component))
	}
	if !hasFindingID(findings, "FOSS_COPYRIGHT_MISSING") {
		t.Errorf("findings = %v, want FOSS_COPYRIGHT_MISSING", findingIDs(findings))
	}
	// Otherwise unchanged: the licence is still resolved from its own file and
	// the text is still retained.
	if len(component.Licenses) == 0 || component.Licenses[0].Expression != "0BSD" {
		t.Errorf("licence = %#v, want 0BSD as before", component.Licenses)
	}
	if len(component.LicenseArtifacts) != 1 {
		t.Errorf("artifacts = %#v, want the retained LICENSE as before", component.LicenseArtifacts)
	}
}

// A curated conclusion answers the finding, because the component then does
// carry an attribution -- somebody's, stated as such. It never becomes an
// observation: evidence.copyright stays empty.
func TestACuratedNoticeAnswersTheFindingWithoutBecomingEvidence(t *testing.T) {
	root := testutil.CorpusSourceTree(t)
	file := domain.UsedFile{ID: fileID("project", "dep/nocopyright/src/nocopyright.c")}
	component, findings := copyrightOf(t, root, "nocopyright", []domain.UsedFile{file},
		map[string][]string{}, config.Component{Path: "dep/nocopyright", Name: "nocopyright",
			Copyright: "Copyright (c) 2026 Reviewed By Hand"})

	if component.Copyright != "Copyright (c) 2026 Reviewed By Hand" {
		t.Errorf("component.copyright = %q, want the curated value", component.Copyright)
	}
	if len(component.Copyrights) != 0 {
		t.Errorf("the curated value became an observation: %q", copyrightTexts(component))
	}
	if hasFindingID(findings, "FOSS_COPYRIGHT_MISSING") {
		t.Errorf("findings = %v; a notice was curated", findingIDs(findings))
	}
}

// The limit of section 22.10, and what "deterministically" means: the cut is
// taken after the ordering, so the same 200 entries survive whatever order the
// files were read in.
func TestThreeHundredStatementsAreBoundedDeterministically(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "dep", "crowded", "LICENSE"), "SPDX-License-Identifier: MIT\n")
	write(t, filepath.Join(root, "dep", "crowded", "src", "crowded.c"), "int crowded(void){return 0;}\n")

	files := []domain.UsedFile{{ID: fileID("project", "dep/crowded/src/crowded.c")}}
	statements := make([]string, 0, 300)
	for i := 0; i < 300; i++ {
		statements = append(statements, fmt.Sprintf("Copyright (c) 2026 Holder %03d", i))
	}
	observed := map[string][]string{"project:dep/crowded/src/crowded.c": statements}

	component, findings := copyrightOf(t, root, "crowded", files, observed, config.Component{})
	if len(component.Copyrights) != license.MaxCopyrightStatements {
		t.Fatalf("kept %d statements, want the limit of %d",
			len(component.Copyrights), license.MaxCopyrightStatements)
	}
	kept := copyrightTexts(component)
	if kept[0] != "Copyright (c) 2026 Holder 000" || kept[len(kept)-1] != "Copyright (c) 2026 Holder 199" {
		t.Errorf("kept %q ... %q, want the first 200 by text order", kept[0], kept[len(kept)-1])
	}
	if !hasFindingID(findings, "FOSS_COPYRIGHT_LIMIT") {
		t.Fatalf("findings = %v, want FOSS_COPYRIGHT_LIMIT", findingIDs(findings))
	}
	for _, finding := range findings {
		if finding.ID == "FOSS_COPYRIGHT_LIMIT" && !strings.Contains(finding.Message, "100 were dropped") {
			t.Errorf("message = %q, want the number dropped", finding.Message)
		}
	}

	// The same statements collected in another order produce the same
	// document. A component's statements are gathered per file from a map, so
	// this is the ordering the result may not depend on.
	reversed := make([]string, len(statements))
	for i := range statements {
		reversed[i] = statements[len(statements)-1-i]
	}
	again, _ := copyrightOf(t, root, "crowded", files,
		map[string][]string{"project:dep/crowded/src/crowded.c": reversed}, config.Component{})
	if strings.Join(copyrightTexts(again), "\n") != strings.Join(kept, "\n") {
		t.Error("the truncation depends on the order the statements were collected in")
	}
}

// Both sources, one component, and the deduplication between them: the same
// holder stated by a source file and by the licence text is one entry, and the
// entry kept is the one from the file that comes first canonically.
func TestStatementsOfFilesAndArtifactsAreMergedOnce(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "dep", "both", "LICENSE"),
		"SPDX-License-Identifier: MIT\n\nCopyright (c) 2019-2026 Shared Holder\n")
	write(t, filepath.Join(root, "dep", "both", "src", "both.c"), "int both(void){return 0;}\n")

	files := []domain.UsedFile{{ID: fileID("project", "dep/both/src/both.c")}}
	observed := map[string][]string{
		"project:dep/both/src/both.c": {"2026 Shared Holder", "Copyright (c) 2026 Second Holder"},
	}
	component, _ := copyrightOf(t, root, "both", files, observed, config.Component{})

	want := []string{"Copyright (c) 2019-2026 Shared Holder", "Copyright (c) 2026 Second Holder"}
	if got := copyrightTexts(component); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("statements = %q, want %q", got, want)
	}
	// project:dep/both/LICENSE sorts before project:dep/both/src/both.c, so
	// the licence file's spelling of the shared holder is the one kept.
	if component.Copyrights[0].File.Canonical() != "project:dep/both/LICENSE" {
		t.Errorf("the kept entry came from %q, want the first file canonically",
			component.Copyrights[0].File.Canonical())
	}
}
