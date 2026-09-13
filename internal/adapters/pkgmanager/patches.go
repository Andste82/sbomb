package pkgmanager

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/limits"
)

// A patched component is a modified component (specification section 19.4),
// and two package managers write down which patches they applied. This reader
// answers that one question and nothing else: it is handed a directory that is
// already settled as a component root and reads the two files directly in it,
// exactly like an enricher (see enricher.go). It does not descend, does not
// walk up and does not look at a sibling -- and it never reads a patch, only
// the record that one was applied. The content of a patch is material sbomb
// does not own (requirement R6), and CycloneDX pedigree.diff stays empty for
// the same reason.
//
// Two files, because those are the two records that exist:
//
//   - conandata.yml, which a Conan recipe uses to declare its patches and
//     which is already a component-root marker of section 19.2;
//   - portfile.cmake, which is how a vcpkg port applies one. An overlay port
//     is kept in the project tree, so this is a file a run can actually see;
//     a port inside a vcpkg checkout is not below any anchor sbomb registers.
const (
	// maxPatchRecordBytes is the bound of section 30 for one patch record. A
	// file over it is not read and INPUT_LIMIT_EXCEEDED says so: a record that
	// was refused is not a record that says "no patches".
	maxPatchRecordBytes = 1 << 20
	// maxPatchesPerComponent bounds the list the way section 30 bounds every
	// other one. A component with more recorded patches than this is still
	// modified -- the answer does not depend on the count -- so the list is
	// bounded, the answer is not, and INPUT_LIMIT_EXCEEDED names the detail
	// the document lost.
	maxPatchesPerComponent = 64
)

// patchSuffixes are the names a patch file carries. A portfile names its
// patches as plain arguments, so the suffix is what identifies them.
var patchSuffixes = []string{".patch", ".diff"}

// Patches reports the patches the package metadata directly in root records as
// applied. An empty result is the ordinary case and is not a gap: most
// components carry no such record at all, which is why nothing is reported
// when none is found.
func Patches(root string) ([]domain.Patch, []domain.Finding) {
	if root == "" {
		return nil, nil
	}
	findings := make([]domain.Finding, 0)
	collected := make([]domain.Patch, 0)

	data, err := readPatchRecord(filepath.Join(root, "conandata.yml"))
	switch {
	case err == nil:
		patches, parseErr := conanPatches(data)
		if parseErr != nil {
			findings = append(findings, patchEvidenceFinding("EVIDENCE_UNREADABLE", root,
				fmt.Sprintf("conandata.yml states patches in a shape this reader does not read, so no patch record was taken from it: %v", parseErr)))
		}
		collected = append(collected, patches...)
	case errors.Is(err, limits.ErrInputLimitExceeded):
		findings = append(findings, patchEvidenceFinding("INPUT_LIMIT_EXCEEDED", root,
			"conandata.yml is larger than the reader limit of section 30, so nothing was read from it and no patch record was taken"))
	}

	data, err = readPatchRecord(filepath.Join(root, "portfile.cmake"))
	switch {
	case err == nil:
		collected = append(collected, portfilePatches(data)...)
	case errors.Is(err, limits.ErrInputLimitExceeded):
		findings = append(findings, patchEvidenceFinding("INPUT_LIMIT_EXCEEDED", root,
			"portfile.cmake is larger than the reader limit of section 30, so nothing was read from it and no patch record was taken"))
	}

	patches, truncated := dedupePatches(collected)
	if truncated {
		findings = append(findings, patchEvidenceFinding("INPUT_LIMIT_EXCEEDED", root,
			fmt.Sprintf("the component records more applied patches than the reader limit of section 30, so the list stops at %d; the component is modified either way",
				maxPatchesPerComponent)))
	}
	return patches, findings
}

// patchEvidenceFinding leaves the subject empty for the caller to fill. This
// reader is handed a directory and knows no component: naming one after the
// directory's base name states a physical location of section 7.9 in a finding
// that a relocated run must state identically -- for the project's own
// component that base name is the checkout directory, which moves -- and it
// resolves to no component in the document, because every other component
// finding names the identity (`component:<name>`) rather than a directory.
func patchEvidenceFinding(id, root, message string) domain.Finding {
	_ = root
	return domain.Finding{
		ID: id, Severity: domain.SeverityWarning,
		Subject: domain.Subject{Kind: "component"},
		Message: message,
	}
}

// readPatchRecord reads one metadata file under the bound of section 30. A
// missing file is the ordinary case, and the caller distinguishes it from a
// file that was refused because it is too large -- the first says there is no
// record, the second says there is one that was not read.
func readPatchRecord(path string) ([]byte, error) {
	bounds := limits.Config{MaxInput: maxPatchRecordBytes}
	return bounds.ReadFile(path)
}

// conanPatches reads the patches block of a Conan recipe's conandata.yml. Two
// shapes occur in the wild and both are read: a mapping from version to a
// sequence of patch entries, which is what the Conan Center recipes use, and a
// bare sequence for a recipe that carries one version.
func conanPatches(data []byte) ([]domain.Patch, error) {
	root, err := parseYAMLSubset(data)
	if err != nil {
		return nil, err
	}
	block := root.child("patches")
	if block == nil {
		return nil, nil
	}
	entries := block.itemsOf()
	if len(entries) == 0 {
		for _, version := range block.keysOf() {
			entries = append(entries, block.child(version).itemsOf()...)
		}
	}
	patches := make([]domain.Patch, 0, len(entries))
	for _, entry := range entries {
		file := entry.scalarAt("patch_file")
		if file == "" {
			// A recipe that patches by base64 payload rather than by file is
			// still patching, so the entry counts -- it just has no name.
			if entry.scalarAt("patch_description") == "" && entry.scalarAt("patch_type") == "" {
				continue
			}
		}
		patches = append(patches, domain.Patch{
			File:        file,
			Type:        patchType(entry.scalarAt("patch_type")),
			Description: entry.scalarAt("patch_description"),
			Source:      "conandata.yml",
		})
	}
	return patches, nil
}

// portfilePatches reads the patch file names a vcpkg portfile names. The
// portfile is CMake and a full reader of it would be an interpreter, so the
// rule is the narrow one: a token that names a patch file is a patch the port
// applies. A portfile does not mention a patch it does not use.
func portfilePatches(data []byte) []domain.Patch {
	patches := make([]domain.Patch, 0)
	for _, line := range strings.Split(string(data), "\n") {
		if index := strings.IndexByte(line, '#'); index >= 0 {
			line = line[:index]
		}
		for _, token := range strings.FieldsFunc(line, func(r rune) bool {
			return r == ' ' || r == '\t' || r == '\r' || r == '(' || r == ')' ||
				r == '"' || r == '\'' || r == ';'
		}) {
			if !isPatchName(token) {
				continue
			}
			patches = append(patches, domain.Patch{
				File:   token,
				Type:   domain.PatchUnofficial,
				Source: "portfile.cmake",
			})
		}
	}
	return patches
}

func isPatchName(token string) bool {
	lowered := strings.ToLower(token)
	for _, suffix := range patchSuffixes {
		if strings.HasSuffix(lowered, suffix) && len(lowered) > len(suffix) {
			return true
		}
	}
	return false
}

// patchType maps what a manager says about a patch onto the CycloneDX
// patches[].type enum. The enum has four values; a manager's own vocabulary is
// its own, so only an exact match is carried over and everything else is
// "unofficial" -- which is what a patch a package manager applied is
// (spec-delta section 8).
func patchType(stated string) string {
	switch strings.ToLower(strings.TrimSpace(stated)) {
	case domain.PatchBackport:
		return domain.PatchBackport
	case domain.PatchCherryPick:
		return domain.PatchCherryPick
	case domain.PatchMonkey:
		return domain.PatchMonkey
	default:
		return domain.PatchUnofficial
	}
}

// dedupePatches orders the patches by (type, file) -- the ordering section 29
// requires for pedigree.patches[] -- and drops a name recorded twice. The
// bound is applied after the ordering, so it takes the same entries however
// the files were read, and the second result says whether it cut the list
// short.
func dedupePatches(patches []domain.Patch) ([]domain.Patch, bool) {
	sort.SliceStable(patches, func(i, j int) bool {
		if patches[i].Type != patches[j].Type {
			return patches[i].Type < patches[j].Type
		}
		if patches[i].File != patches[j].File {
			return patches[i].File < patches[j].File
		}
		return patches[i].Source < patches[j].Source
	})
	out := make([]domain.Patch, 0, len(patches))
	seen := map[string]bool{}
	truncated := false
	for _, patch := range patches {
		key := patch.Type + "\x00" + patch.File + "\x00" + patch.Source
		if seen[key] {
			continue
		}
		seen[key] = true
		if len(out) == maxPatchesPerComponent {
			truncated = true
			break
		}
		out = append(out, patch)
	}
	if len(out) == 0 {
		return nil, truncated
	}
	return out, truncated
}

// HasGitRoot reports whether a directory is itself the root of a git
// repository or a submodule working tree.
//
// It is a filesystem question and not a git question on purpose. Asking git
// would answer for the nearest enclosing repository, so a library copied into
// a project's own tree would inherit the project's dirty state -- and an edit
// to the manufacturer's own code would be published as a modification of a
// third-party component. The check is for os.Stat(root/.git) and nothing else:
// a directory for an ordinary checkout, a file for a submodule.
func HasGitRoot(root string) bool {
	if root == "" {
		return false
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		return false
	}
	return true
}
