// Command replynorm sorts the pointer-ordered arrays out of a CMake File API
// reply directory.
//
// It exists so that regenerating an unchanged fixture corpus is a no-op. A
// target's "dependencies" array is written in the iteration order of a
// std::set of cmTargetDepend, whose comparator falls back to comparing the
// target pointers, so the order is the order the targets happened to be
// allocated in. Two configure runs of the same project therefore produce a
// different array, and because every reply document is named by the digest of
// its own bytes, the file name moves with it, and so do the codemodel and the
// index that reference the name. For a project with nine components that is
// several files of pure churn on every regeneration, which is what made
// p03-dupnames unreviewable before and what would have made p14-foss worse.
//
// A JSON array is the only way to serialize a set, and the set has no order,
// so sorting it by target identifier discards nothing that was in the
// evidence. Nothing sbomb writes depends on it either: the writer in
// internal/cyclonedx sorts every dependency list it emits, and no golden moved
// when the corpus was normalized.
//
// The rewrite is surgical and leaves a document the File API could itself
// have written. Only the array elements move -- every byte of jsoncpp's
// layout around them is copied through rather than re-serialized -- and each
// renamed document is renamed to the digest of its new content, so the reply
// stays content-addressed by CMake's own rule. Documents whose name is not a
// digest, such as the index file regen.sh pins to the fixture date, keep the
// name they have and only their references are updated.
//
//	go run ./tools/fixtures/replynorm <reply-dir>
package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// The array as jsoncpp's styled writer lays it out at the top level of a
// target document: one tab of indentation, the value on the following line.
const (
	dependenciesOpen  = "\n\t\"dependencies\" : \n\t[\n"
	dependenciesClose = "\n\t]"
)

// contentAddressedName recognizes a reply document named by its own digest.
var contentAddressedName = regexp.MustCompile(`^(.*-)([0-9a-f]{20})\.json$`)

var errMalformed = errors.New("malformed File API reply")

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: replynorm <reply-dir>")
		os.Exit(1)
	}
	if err := normalizeReplyDir(os.Args[1]); err != nil {
		fmt.Fprintf(os.Stderr, "replynorm: %s: %v\n", os.Args[1], err)
		os.Exit(1)
	}
}

func normalizeReplyDir(replyDir string) error {
	names, err := filepath.Glob(filepath.Join(replyDir, "*.json"))
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return fmt.Errorf("%w: no documents in %s", errMalformed, replyDir)
	}
	sort.Strings(names)

	documents := make(map[string][]byte, len(names))
	for _, name := range names {
		content, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		documents[filepath.Base(name)] = content
	}
	if err := verifyDigests(documents); err != nil {
		return err
	}

	for name, content := range documents {
		sorted, err := sortDependencies(content)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		documents[name] = sorted
	}
	rename(documents)

	return write(replyDir, names, documents)
}

// verifyDigests checks this tool's own SHA-3 against the input. Every reply
// document CMake named by a digest is hashed before anything is changed, and
// a mismatch means either that the implementation is wrong or that the
// convention has changed -- in both cases renaming a document would produce a
// name no client could resolve, so nothing is written.
func verifyDigests(documents map[string][]byte) error {
	verified := 0
	for name, content := range documents {
		match := contentAddressedName.FindStringSubmatch(name)
		if match == nil {
			continue
		}
		if got := replyHash(content); got != match[2] {
			return fmt.Errorf("%w: %s is named for digest %s but hashes to %s",
				errMalformed, name, match[2], got)
		}
		verified++
	}
	if verified == 0 {
		return fmt.Errorf("%w: no document is named by its digest", errMalformed)
	}
	return nil
}

// sortDependencies reorders the elements of a target document's top-level
// "dependencies" array by target identifier, leaving every other byte alone.
func sortDependencies(content []byte) ([]byte, error) {
	text := string(content)
	start := strings.Index(text, dependenciesOpen)
	if start < 0 {
		return content, nil
	}
	bodyStart := start + len(dependenciesOpen)
	end := strings.Index(text[bodyStart:], dependenciesClose)
	if end < 0 {
		return nil, fmt.Errorf("%w: unterminated dependencies array", errMalformed)
	}
	body := text[bodyStart : bodyStart+end]

	elements, err := splitObjects(body)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(elements, func(i, j int) bool {
		return elementID(elements[i]) < elementID(elements[j])
	})

	var out bytes.Buffer
	out.WriteString(text[:bodyStart])
	out.WriteString(strings.Join(elements, ",\n"))
	out.WriteString(text[bodyStart+end:])
	return out.Bytes(), nil
}

// splitObjects cuts the body of an array of objects into its elements,
// counting braces outside of strings so that the separating comma is found
// without parsing the whole document.
func splitObjects(body string) ([]string, error) {
	var elements []string
	depth, inString, escaped, element := 0, false, false, 0
	for index := 0; index < len(body); index++ {
		character := body[index]
		switch {
		case escaped:
			escaped = false
		case inString && character == '\\':
			escaped = true
		case character == '"':
			inString = !inString
		case inString:
		case character == '{':
			depth++
		case character == '}':
			depth--
			if depth == 0 {
				elements = append(elements, body[element:index+1])
				// Step over the ",\n" that separates two elements.
				if index+1 < len(body) {
					if body[index+1] != ',' {
						return nil, fmt.Errorf("%w: unexpected %q after an array element", errMalformed, body[index+1])
					}
					index += 2
				}
				element = index + 1
			}
		}
	}
	if depth != 0 || inString {
		return nil, fmt.Errorf("%w: unbalanced array element", errMalformed)
	}
	return elements, nil
}

// elementID is the "id" member of one dependency, which is the target
// identifier the array is sorted by.
func elementID(element string) string {
	const key = "\"id\" : \""
	start := strings.Index(element, key)
	if start < 0 {
		return ""
	}
	start += len(key)
	end := strings.Index(element[start:], "\"")
	if end < 0 {
		return ""
	}
	return element[start : start+end]
}

// rename brings the directory back to a fixed point: a document whose content
// changed is renamed to the digest of the new content, and every reference to
// the old name is rewritten. That changes the referring document in turn, so
// this repeats until nothing moves -- three rounds at most, target to
// codemodel to index.
func rename(documents map[string][]byte) {
	for {
		renamed := false
		for name, content := range documents {
			match := contentAddressedName.FindStringSubmatch(name)
			if match == nil {
				continue
			}
			digest := replyHash(content)
			if digest == match[2] {
				continue
			}
			newName := match[1] + digest + ".json"
			delete(documents, name)
			documents[newName] = content
			for other, otherContent := range documents {
				documents[other] = bytes.ReplaceAll(otherContent, []byte(name), []byte(newName))
			}
			renamed = true
			break
		}
		if !renamed {
			return
		}
	}
}

func write(replyDir string, previous []string, documents map[string][]byte) error {
	for _, path := range previous {
		if _, kept := documents[filepath.Base(path)]; kept {
			continue
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	for name, content := range documents {
		if err := os.WriteFile(filepath.Join(replyDir, name), content, 0o644); err != nil {
			return err
		}
	}
	return nil
}
