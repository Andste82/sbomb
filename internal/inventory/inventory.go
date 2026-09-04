package resolver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/example/sbomb/internal/domain"
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

type HashOptions struct {
	Anchors              []string
	AllowUnanchoredReads bool
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
	out := make([]domain.UsedFile, len(files))
	for i, f := range files {
		clone := f
		if clone.Properties == nil {
			clone.Properties = map[string][]string{}
		}
		if clone.Hashes == nil {
			clone.Hashes = map[string]string{}
		}
		path := canonicalFilePath(clone.ID)
		if path == "" {
			clone.Missing = true
			clone.Hashes = nil
			clone.Properties["finding"] = appendUnique(clone.Properties["finding"], "MISSING_FILE_HASH")
			out[i] = clone
			continue
		}
		st, err := os.Stat(path)
		if err != nil || !st.Mode().IsRegular() {
			clone.Missing = true
			clone.Hashes = nil
			clone.Properties["finding"] = appendUnique(clone.Properties["finding"], "MISSING_FILE_HASH")
			out[i] = clone
			continue
		}
		resolved, err := filepath.EvalSymlinks(path)
		isSymlink := err == nil && filepath.Clean(resolved) != filepath.Clean(path)
		if err != nil || (isSymlink && !options.AllowUnanchoredReads && !withinAnchor(resolved, options.Anchors)) {
			clone.Missing = true
			clone.Hashes = nil
			clone.Properties["finding"] = appendUnique(clone.Properties["finding"], "MISSING_FILE_HASH")
			out[i] = clone
			continue
		}
		clone.SizeBytes = st.Size()
		data, err := os.ReadFile(path)
		if err != nil {
			clone.Missing = true
			clone.Hashes = nil
			clone.Properties["finding"] = appendUnique(clone.Properties["finding"], "MISSING_FILE_HASH")
			out[i] = clone
			continue
		}
		sum := sha256.Sum256(data)
		clone.Hashes["sha256"] = hex.EncodeToString(sum[:])
		out[i] = clone
	}
	return out
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
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
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
func DetectStaleness(files []domain.UsedFile, artifacts []string, buildID string) ([]domain.Finding, error) {
	_ = buildID
	findings := make([]domain.Finding, 0, len(files))
	for _, f := range files {
		if f.Class == "" || !isSourceLike(f.Class) {
			continue
		}
		filePath := canonicalFilePath(f.ID)
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

func canonicalFilePath(id domain.FileID) string {
	if id.RelPath == "" {
		return ""
	}
	// For the in-memory model, RelPath is usually already a real path on disk.
	// If it is a relative path, resolve against the working directory to make hashing deterministic.
	if filepath.IsAbs(id.RelPath) {
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

func init() {
	_ = json.Marshal
	_ = fmt.Sprintf
}
