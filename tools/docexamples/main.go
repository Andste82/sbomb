// Command docexamples checks that the JSON configuration examples in the user
// documentation are valid configurations.
//
// A documented example the tool would reject is worse than no example:
// somebody copies it, gets an error, and concludes the tool is broken. This
// runs each fenced JSON block that looks like a configuration through the same
// loader a real run uses. It found the artifact role "firmware" in the
// documentation, which the loader has never accepted.
//
//	go run ./tools/docexamples
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/example/sbomb/internal/config"
	"github.com/example/sbomb/internal/policy"
)

var fence = regexp.MustCompile("(?s)```json\\n(.*?)```")

// configurationKeys are the section names a configuration example has. A block
// naming none of them is something else -- a waiver file, a fragment of a
// CycloneDX document -- and is skipped.
var configurationKeys = map[string]bool{
	"schemaVersion": true, "project": true, "build": true, "mode": true,
	"artifacts": true, "policy": true, "output": true, "anchors": true,
	"discovery": true, "components": true, "manifests": true,
	"distroManifests": true,
}

func main() {
	documents := []string{
		"README.md",
		filepath.Join("docs", "configuration.md"),
		filepath.Join("docs", "getting-started.md"),
		filepath.Join("docs", "architecture.md"),
		filepath.Join("docs", "ci.md"),
		filepath.Join("docs", "foss.md"),
	}
	checked, failed := 0, 0
	for _, document := range documents {
		data, err := os.ReadFile(document)
		if err != nil {
			continue
		}
		for index, match := range fence.FindAllStringSubmatch(string(data), -1) {
			if looksLikeWaivers(match[1]) {
				checked++
				if err := loadWaiverExample(match[1]); err != nil {
					failed++
					fmt.Fprintf(os.Stderr, "%s block %d: %v\n", document, index+1, err)
				}
				continue
			}
			var probe map[string]json.RawMessage
			if json.Unmarshal([]byte(match[1]), &probe) != nil {
				continue
			}
			if !looksLikeConfiguration(probe) {
				continue
			}
			checked++
			if err := loadExample(probe); err != nil {
				failed++
				fmt.Fprintf(os.Stderr, "%s block %d: %v\n", document, index+1, err)
			}
		}
	}
	if failed > 0 {
		fmt.Fprintf(os.Stderr, "docexamples: %d of %d documented configurations are invalid\n", failed, checked)
		os.Exit(1)
	}
	fmt.Printf("docexamples: %d documented configurations load\n", checked)
}

// withField fills in a field the loader insists on, so that a fragment showing
// one section is judged on that section rather than on what it omits.
func withField(section json.RawMessage, name, value string) json.RawMessage {
	fields := map[string]json.RawMessage{}
	if len(section) > 0 {
		if err := json.Unmarshal(section, &fields); err != nil {
			return section
		}
	}
	if _, present := fields[name]; !present {
		fields[name] = json.RawMessage(value)
	}
	completed, err := json.Marshal(fields)
	if err != nil {
		return section
	}
	return completed
}

// looksLikeWaivers recognizes a waiver example by what a waiver entry contains
// rather than by the shape of the document around it. The shape is exactly
// what a wrong example gets wrong: docs/configuration.md showed a bare array
// for a file the loader reads as an object, and a check keyed on the object
// would have skipped it as "something else".
func looksLikeWaivers(block string) bool {
	return strings.Contains(block, `"id"`) &&
		strings.Contains(block, `"subject"`) &&
		strings.Contains(block, `"reason"`)
}

// loadWaiverExample runs a block through the loader a real run uses, from a
// file, because that is the only entry point there is.
func loadWaiverExample(block string) error {
	file := filepath.Join(os.TempDir(), "docexamples-waivers.json")
	if err := os.WriteFile(file, []byte(block), 0o600); err != nil {
		return err
	}
	defer os.Remove(file)
	waivers, err := policy.LoadWaivers(file)
	if err != nil {
		return err
	}
	if len(waivers) == 0 {
		return fmt.Errorf("the example parses but states no waiver, so a reader copying it silences nothing")
	}
	return nil
}

func looksLikeConfiguration(probe map[string]json.RawMessage) bool {
	for key := range probe {
		if configurationKeys[key] {
			return true
		}
	}
	return false
}

// loadExample runs a block through the real loader. Most examples show one
// section, so the two fields the loader insists on are supplied when the
// fragment omits them.
func loadExample(probe map[string]json.RawMessage) error {
	probe["project"] = withField(probe["project"], "name", `"example"`)
	probe["build"] = withField(probe["build"], "dir", `"build"`)
	completed, err := json.Marshal(probe)
	if err != nil {
		return err
	}

	file, err := os.CreateTemp("", "docexample-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(completed); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if _, err := config.Load(file.Name()); err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(err.Error()))
	}
	return nil
}
