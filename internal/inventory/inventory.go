package inventory

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/example/sbomb/internal/domain"
	"github.com/example/sbomb/internal/pathmodel"
)

// InventoryFile is the JSON-serializable representation used for --inventory-dump.
type InventoryFile struct {
	Canonical  string              `json:"canonical"`
	Class      string              `json:"class"`
	Hashes     map[string]string   `json:"hashes,omitempty"`
	SizeBytes  int64               `json:"sizeBytes,omitempty"`
	Missing    bool                `json:"missing,omitempty"`
	Component  string              `json:"component,omitempty"`
	Properties map[string][]string `json:"properties,omitempty"`
}

// InventoryComponent is the minimal stable component listing for inventory dumps.
type InventoryComponent struct {
	ID     string `json:"id"`
	BomRef string `json:"bomRef,omitempty"`
	Name   string `json:"name,omitempty"`
}

// InventoryDump is the stable, CycloneDX-independent inventory document.
type InventoryDump struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Files         []InventoryFile      `json:"files"`
	Components    []InventoryComponent `json:"components,omitempty"`
}

// HashAlgorithmSHA256 is the CycloneDX spelling of the default algorithm.
const HashAlgorithmSHA256 = "SHA-256"

type HashOptions struct {
	Anchors              []string
	AllowUnanchoredReads bool
	// Resolve maps a file identity to where its bytes can be read. A canonical
	// identity such as "project:main.c" is not a filesystem path, so without
	// this nothing can be hashed.
	Resolve func(domain.FileID) string
	// Jobs bounds the worker pool. Section 23 requires hashing to be
	// parallelized and its results to be order-independent.
	Jobs int
}

// MergeUsedFiles collapses duplicate file evidence records into a single inhabited record.
func MergeUsedFiles(files []domain.UsedFile) []domain.UsedFile {
	if len(files) == 0 {
		return nil
	}
	byKey := make(map[string]*domain.UsedFile)
	order := make([]string, 0, len(files))
	for _, f := range files {
		key := f.ID.Canonical()
		if existing, ok := byKey[key]; ok {
			if existing.Class == "" {
				existing.Class = f.Class
			}
			if existing.ComponentID == "" {
				existing.ComponentID = f.ComponentID
			}
			if existing.Hashes == nil && len(f.Hashes) > 0 {
				existing.Hashes = make(map[string]string, len(f.Hashes))
			}
			for alg, hash := range f.Hashes {
				if hash == "" {
					continue
				}
				if existing.Hashes == nil {
					existing.Hashes = map[string]string{}
				}
				existing.Hashes[alg] = hash
			}
			if existing.SizeBytes == 0 && f.SizeBytes != 0 {
				existing.SizeBytes = f.SizeBytes
			}
			existing.Missing = existing.Missing || f.Missing
			if f.Properties != nil {
				for k, values := range f.Properties {
					if existing.Properties == nil {
						existing.Properties = make(map[string][]string)
					}
					if _, ok := existing.Properties[k]; !ok {
						existing.Properties[k] = nil
					}
					for _, v := range values {
						if !containsString(existing.Properties[k], v) {
							existing.Properties[k] = append(existing.Properties[k], v)
						}
					}
				}
			}
			continue
		}
		clone := f
		if clone.Properties == nil {
			clone.Properties = map[string][]string{}
		}
		if clone.Hashes == nil {
			clone.Hashes = map[string]string{}
		}
		byKey[key] = &clone
		order = append(order, key)
	}
	if len(order) == 0 {
		return nil
	}
	sort.Strings(order)
	out := make([]domain.UsedFile, 0, len(order))
	for _, key := range order {
		out = append(out, *byKey[key])
	}
	return out
}

// HashUsedFiles computes SHA-256 hashes for all readable file records and marks missing files.
func HashUsedFiles(files []domain.UsedFile) []domain.UsedFile {
	return HashUsedFilesWithOptions(files, HashOptions{})
}

func HashUsedFilesWithOptions(files []domain.UsedFile, options HashOptions) []domain.UsedFile {
	jobs := options.Jobs
	if jobs <= 0 {
		jobs = runtime.NumCPU()
	}
	if jobs > len(files) {
		jobs = len(files)
	}
	out := make([]domain.UsedFile, len(files))
	if len(files) == 0 {
		return out
	}

	// Results are written to a fixed index, so the output order does not
	// depend on which worker finished first.
	indexes := make(chan int)
	var group sync.WaitGroup
	for worker := 0; worker < jobs; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for i := range indexes {
				out[i] = hashOne(files[i], options)
			}
		}()
	}
	for i := range files {
		indexes <- i
	}
	close(indexes)
	group.Wait()
	return out
}

func hashOne(file domain.UsedFile, options HashOptions) domain.UsedFile {
	clone := file
	clone.Properties = cloneStringMap(file.Properties)
	if clone.Properties == nil {
		clone.Properties = map[string][]string{}
	}
	clone.Hashes = map[string]string{}

	unreadable := func() domain.UsedFile {
		clone.Missing = true
		clone.Hashes = nil
		clone.Properties["finding"] = appendUnique(clone.Properties["finding"], "MISSING_FILE_HASH")
		return clone
	}

	path := canonicalFilePath(clone.ID, options)
	if path == "" {
		return unreadable()
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return unreadable()
	}
	resolved, err := filepath.EvalSymlinks(path)
	isSymlink := err == nil && filepath.Clean(resolved) != filepath.Clean(path)
	if err != nil || (isSymlink && !options.AllowUnanchoredReads && !withinAnchor(resolved, options.Anchors)) {
		return unreadable()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return unreadable()
	}
	clone.SizeBytes = info.Size()
	sum := sha256.Sum256(data)
	// CycloneDX names the algorithm "SHA-256"; the inventory dump and the
	// SBOM must agree on one spelling (section 28.6).
	clone.Hashes[HashAlgorithmSHA256] = hex.EncodeToString(sum[:])
	return clone
}

func withinAnchor(path string, anchors []string) bool {
	cleanPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	for _, anchor := range anchors {
		cleanAnchor, err := filepath.Abs(anchor)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(cleanAnchor, cleanPath)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !pathmodel.IsAbsolute(rel) {
			return true
		}
	}
	return false
}

// BuildInventoryDump converts a file set into the stable machine-readable inventory dump format.
func BuildInventoryDump(files []domain.UsedFile, components []domain.Component) InventoryDump {
	merged := MergeUsedFiles(files)
	ordered := make([]InventoryFile, 0, len(merged))
	for _, f := range merged {
		entry := InventoryFile{
			Canonical:  f.ID.Canonical(),
			Class:      string(f.Class),
			Hashes:     map[string]string{},
			SizeBytes:  f.SizeBytes,
			Missing:    f.Missing,
			Component:  f.ComponentID,
			Properties: cloneStringMap(f.Properties),
		}
		for alg, hash := range f.Hashes {
			entry.Hashes[alg] = hash
		}
		ordered = append(ordered, entry)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Canonical < ordered[j].Canonical })

	componentEntries := make([]InventoryComponent, 0, len(components))
	for _, c := range components {
		componentEntries = append(componentEntries, InventoryComponent{ID: c.ID, BomRef: c.BomRef, Name: c.Name})
	}
	sort.Slice(componentEntries, func(i, j int) bool { return componentEntries[i].ID < componentEntries[j].ID })

	return InventoryDump{SchemaVersion: 1, Files: ordered, Components: componentEntries}
}

// DetectStaleness checks for timestamp violations where a source or header is newer than the artifact.
// DetectStaleness compares file and artifact timestamps (section 27.3).
// Resolve maps identities to readable paths, exactly as in HashOptions.
func DetectStaleness(files []domain.UsedFile, artifacts []string, buildID string, resolve func(domain.FileID) string) ([]domain.Finding, error) {
	_ = buildID
	findings := make([]domain.Finding, 0, len(files))
	for _, f := range files {
		if f.Class == "" || !isSourceLike(f.Class) {
			continue
		}
		filePath := canonicalFilePath(f.ID, HashOptions{Resolve: resolve})
		if filePath == "" {
			continue
		}
		info, err := os.Stat(filePath)
		if err != nil {
			continue
		}
		for _, artifact := range artifacts {
			artInfo, err := os.Stat(artifact)
			if err != nil {
				continue
			}
			if info.ModTime().After(artInfo.ModTime()) {
				findings = append(findings, domain.Finding{
					ID:       "STALE_BUILD_EVIDENCE",
					Severity: domain.SeverityError,
					Subject:  domain.Subject{Kind: "file", Ref: f.ID.Canonical()},
					Message:  "Source file is newer than the build artifact",
					Detail: map[string]any{
						"source":         f.ID.Canonical(),
						"artifact":       artifact,
						"source_mtime":   info.ModTime().UTC().Format(time.RFC3339),
						"artifact_mtime": artInfo.ModTime().UTC().Format(time.RFC3339),
					},
					Evidence: []string{artifact},
				})
				break
			}
		}
	}
	return findings, nil
}

func classifyFile(path string, generated bool) domain.FileClass {
	lower := strings.ToLower(filepath.ToSlash(path))
	switch {
	case strings.HasSuffix(lower, ".o"), strings.HasSuffix(lower, ".obj"):
		return domain.FileClassObject
	case strings.Contains(lower, ".so") || strings.HasSuffix(lower, ".dylib") || strings.HasSuffix(lower, ".dll"):
		return domain.FileClassSharedLibrary
	case strings.HasSuffix(lower, ".a"), strings.HasSuffix(lower, ".lib"):
		return domain.FileClassArchive
	case strings.HasSuffix(lower, ".h"), strings.HasSuffix(lower, ".hh"), strings.HasSuffix(lower, ".hpp"), strings.HasSuffix(lower, ".hxx"):
		if generated {
			return domain.FileClassGeneratedHeader
		}
		return domain.FileClassHeader
	case strings.HasSuffix(lower, ".c"), strings.HasSuffix(lower, ".cc"), strings.HasSuffix(lower, ".cpp"), strings.HasSuffix(lower, ".cxx"), strings.HasSuffix(lower, ".c++"):
		if generated {
			return domain.FileClassGeneratedSource
		}
		return domain.FileClassSource
	default:
		return domain.FileClassUnknown
	}
}

// canonicalFilePath maps a file identity to where its bytes can be read. An
// identity is not a path -- "project:main.c" names no file -- so the caller
// supplies the mapping; the fallback exists only for callers that still work
// with plain paths.
func canonicalFilePath(id domain.FileID, options HashOptions) string {
	if options.Resolve != nil {
		return options.Resolve(id)
	}
	if id.RelPath == "" {
		return ""
	}
	if pathmodel.IsAbsolute(id.RelPath) {
		return id.RelPath
	}
	return filepath.Clean(id.RelPath)
}

func isSourceLike(cls domain.FileClass) bool {
	switch cls {
	case domain.FileClassSource, domain.FileClassGeneratedSource, domain.FileClassHeader, domain.FileClassGeneratedHeader:
		return true
	default:
		return false
	}
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func appendUnique(values []string, item string) []string {
	if containsString(values, item) {
		return values
	}
	return append(values, item)
}

func cloneStringMap(in map[string][]string) map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]string, len(in))
	for k, v := range in {
		if v == nil {
			out[k] = nil
			continue
		}
		cp := make([]string, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}
