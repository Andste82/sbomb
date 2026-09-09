// Package msbuild reads the evidence emitted by Visual Studio/MSBuild builds.
package msbuild

import (
	"encoding/binary"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

type Mapping struct {
	Object   string
	Source   string
	Strategy string
}

type Evidence struct {
	Mappings  []Mapping
	Headers   map[string][]string
	LibInputs map[string][]string
}

func HasEvidence(buildDir string) bool {
	found := false
	_ = filepath.WalkDir(buildDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(path), ".tlog") {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}

func Read(buildDir string) (Evidence, error) {
	evidence := Evidence{Headers: map[string][]string{}, LibInputs: map[string][]string{}}
	return evidence, filepath.WalkDir(buildDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(path), ".tlog") {
			return nil
		}
		name := filepath.Base(path)
		lowerName := strings.ToLower(name)
		if !(strings.Contains(lowerName, "cl.") || strings.Contains(lowerName, "link.") || strings.Contains(lowerName, "lib-") || strings.Contains(lowerName, "rc.")) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text, err := DecodeTLog(data)
		if err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
		parseTLog(text, name, &evidence)
		return nil
	})
}

func DecodeTLog(data []byte) (string, error) {
	if len(data)%2 != 0 {
		return "", fmt.Errorf("UTF-16LE data has an odd byte count")
	}
	if len(data) >= 2 && data[0] == 0xff && data[1] == 0xfe {
		data = data[2:]
	}
	if len(data)%2 != 0 {
		return "", fmt.Errorf("truncated UTF-16LE code unit")
	}
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(data[i*2:])
	}
	return string(utf16.Decode(units)), nil
}

func parseTLog(text, name string, evidence *Evidence) {
	pendingSource := ""
	pendingSources := []string{}
	pendingLibInputs := []string{}
	object := ""
	lowerName := strings.ToLower(name)
	isReadLog := strings.Contains(lowerName, ".read.")
	isCommandLog := strings.Contains(lowerName, ".command.")
	isLibLog := strings.Contains(lowerName, "lib")
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "^#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) >= 2 && strings.HasSuffix(strings.ToLower(parts[0]), ".obj") {
			evidence.Mappings = append(evidence.Mappings, Mapping{Object: parts[0], Source: parts[1], Strategy: "msbuild-tlog"})
			object = parts[0]
			continue
		}
		path := strings.TrimPrefix(line, "^")
		lower := strings.ToLower(path)
		sourcePaths := strings.Split(path, "|")
		if isLibLog && strings.HasPrefix(line, "^") {
			pendingLibInputs = sourcePaths
		}
		if isLibLog && len(pendingLibInputs) > 0 {
			if strings.HasSuffix(lower, ".lib") {
				evidence.LibInputs[path] = append(evidence.LibInputs[path], pendingLibInputs...)
			} else if idx := strings.Index(lower, "/out:"); idx >= 0 {
				libOut := strings.Trim(strings.TrimSpace(line[idx+5:]), "\"")
				libOut = strings.Fields(libOut)[0]
				libOut = strings.Trim(libOut, "\"")
				if libOut != "" {
					evidence.LibInputs[libOut] = append(evidence.LibInputs[libOut], pendingLibInputs...)
				}
			}
		}
		sourcePath := sourcePaths[0]
		sourceLower := strings.ToLower(sourcePath)
		if strings.HasSuffix(sourceLower, ".c") || strings.HasSuffix(sourceLower, ".cc") || strings.HasSuffix(sourceLower, ".cpp") || strings.HasSuffix(sourceLower, ".cxx") || strings.HasSuffix(sourceLower, ".rc") {
			pendingSources = pendingSources[:0]
			for _, candidate := range sourcePaths {
				candidateLower := strings.ToLower(candidate)
				if strings.HasSuffix(candidateLower, ".c") || strings.HasSuffix(candidateLower, ".cc") || strings.HasSuffix(candidateLower, ".cpp") || strings.HasSuffix(candidateLower, ".cxx") || strings.HasSuffix(candidateLower, ".rc") {
					pendingSources = append(pendingSources, candidate)
				}
			}
			pendingSource = sourcePath
			continue
		}
		if isCommandLog && pendingSource != "" {
			if index := strings.Index(strings.ToLower(line), "/fo"); index >= 0 {
				objectPath := strings.Trim(strings.TrimSpace(line[index+3:]), "\"")
				objectPath = strings.TrimSuffix(objectPath, "\\\\")
				if objectPath != "" {
					evidence.Mappings = append(evidence.Mappings, Mapping{Object: objectPath, Source: pendingSource, Strategy: "msbuild-tlog"})
				}
				pendingSource = ""
				continue
			}
		}
		if strings.HasSuffix(lower, ".c") || strings.HasSuffix(lower, ".cc") || strings.HasSuffix(lower, ".cpp") || strings.HasSuffix(lower, ".cxx") || strings.HasSuffix(lower, ".rc") {
			pendingSource = path
			continue
		}
		if len(pendingSources) > 0 && (strings.HasSuffix(lower, ".obj") || strings.HasSuffix(lower, ".res")) {
			matchingSource := pendingSources[0]
			for index, source := range pendingSources {
				if strings.EqualFold(strings.TrimSuffix(filepath.Base(source), filepath.Ext(source)), strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))) {
					matchingSource = source
					pendingSources = append(pendingSources[:index], pendingSources[index+1:]...)
					break
				}
			}
			evidence.Mappings = append(evidence.Mappings, Mapping{Object: path, Source: matchingSource, Strategy: "msbuild-tlog"})
			object = path
			if len(pendingSources) == 1 {
				pendingSource = pendingSources[0]
			}
			if len(pendingSources) > 0 {
				pendingSources = pendingSources[1:]
			} else {
				pendingSource = ""
			}
		} else if pendingSource != "" && (strings.HasSuffix(lower, ".obj") || strings.HasSuffix(lower, ".res")) {
			evidence.Mappings = append(evidence.Mappings, Mapping{Object: path, Source: pendingSource, Strategy: "msbuild-tlog"})
			object = path
			pendingSource = ""
		}
		if isReadLog && object != "" && (strings.HasSuffix(lower, ".h") || strings.HasSuffix(lower, ".hpp") || strings.HasSuffix(lower, ".hh")) {
			evidence.Headers[object] = append(evidence.Headers[object], path)
		}
	}
}
