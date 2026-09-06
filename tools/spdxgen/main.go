// Command spdxgen generates the embedded table of SPDX license text hashes
// that specification section 22.3 technique 2 requires.
//
// Only hashes are embedded, never the texts: roughly 740 entries at 32 bytes
// each keeps the single-executable requirement of section 37 unaffected while
// still allowing an exact match against a real LICENSE file.
//
//	go run ./tools/spdxgen              regenerate the digest table and the templates
//	go run ./tools/spdxgen --check      fail if either has drifted
//
// Two artifacts come out of the same download. The digest table is technique 2
// of section 22.3: hashes only, no texts. The template blob is technique 4
// (deviation D18): the standardLicenseTemplate of every license, gzipped and
// embedded, which is what recognizes a licence whose copyright holder has been
// filled in. It is around a megabyte, so it is a compressed blob beside the
// package rather than a Go string literal, and it is decompressed only when a
// digest lookup has already missed.
//
// The check mode exists because the SPDX list changes: a table that silently
// falls behind turns into silent NOASSERTION results.
package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"crypto/sha256"
	"encoding/hex"

	"github.com/example/sbomb/internal/license"
)

const defaultSource = "https://raw.githubusercontent.com/spdx/license-list-data/main/json"

type licenseIndex struct {
	Version  string `json:"licenseListVersion"`
	Licenses []struct {
		ID         string `json:"licenseId"`
		Name       string `json:"name"`
		Deprecated bool   `json:"isDeprecatedLicenseId"`
	} `json:"licenses"`
}

type licenseDetail struct {
	ID       string `json:"licenseId"`
	Text     string `json:"licenseText"`
	Template string `json:"standardLicenseTemplate"`
}

func main() {
	source := flag.String("source", defaultSource, "base URL of the SPDX license list JSON")
	output := flag.String("output", filepath.Join("internal", "license", "spdxhashes.go"), "generated file")
	templateOutput := flag.String("templates", filepath.Join("internal", "license", "spdxtemplates.gz"), "generated template blob")
	check := flag.Bool("check", false, "verify the committed table instead of writing it")
	workers := flag.Int("workers", 16, "concurrent downloads")
	flag.Parse()

	generated, templates, err := generate(*source, *workers)
	if err != nil {
		fmt.Fprintln(os.Stderr, "spdxgen:", err)
		os.Exit(1)
	}

	if *check {
		committed, err := os.ReadFile(*output)
		if err != nil {
			fmt.Fprintln(os.Stderr, "spdxgen:", err)
			os.Exit(1)
		}
		if string(committed) != generated {
			fmt.Fprintf(os.Stderr, "spdxgen: %s is out of date; run: go run ./tools/spdxgen\n", *output)
			os.Exit(1)
		}
		// The blob is compared by what it means, not by its bytes: gzip output
		// depends on the compressor, and a Go release that changes it would
		// otherwise fail this check for no substantive reason.
		committedTemplates, err := os.ReadFile(*templateOutput)
		if err != nil {
			fmt.Fprintln(os.Stderr, "spdxgen:", err)
			os.Exit(1)
		}
		same, reason := sameTemplates(committedTemplates, templates)
		if !same {
			fmt.Fprintf(os.Stderr, "spdxgen: %s is out of date (%s); run: go run ./tools/spdxgen\n", *templateOutput, reason)
			os.Exit(1)
		}
		fmt.Println("spdxgen: the embedded license table and templates match the current SPDX list")
		return
	}

	if err := os.WriteFile(*output, []byte(generated), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "spdxgen:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*templateOutput, templates, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "spdxgen:", err)
		os.Exit(1)
	}
	fmt.Printf("spdxgen: wrote %s and %s (%d KB)\n", *output, *templateOutput, len(templates)/1024)
}

// sameTemplates compares two blobs by their decompressed records.
func sameTemplates(committed, fresh []byte) (bool, string) {
	a, err := readBlob(committed)
	if err != nil {
		return false, err.Error()
	}
	b, err := readBlob(fresh)
	if err != nil {
		return false, err.Error()
	}
	if len(a) != len(b) {
		return false, fmt.Sprintf("%d licenses committed, %d in the list", len(a), len(b))
	}
	for id, template := range b {
		if a[id] != template {
			return false, "template changed for " + id
		}
	}
	return true, ""
}

func readBlob(blob []byte) (map[string]string, error) {
	reader, err := gzip.NewReader(bytes.NewReader(blob))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	raw, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	fields := bytes.Split(raw, []byte{0})
	for i := 0; i+2 < len(fields); i += 3 {
		out[string(fields[i])] = string(fields[i+1]) + "\x00" + string(fields[i+2])
	}
	return out, nil
}

// writeTemplateBlob packs the templates as NUL-separated id/template pairs.
// A template is arbitrary text containing newlines and backslashes, so a
// line-oriented format would need escaping, and an escaping bug would corrupt
// a licence silently.
func writeTemplateBlob(templates map[string]string, deprecated map[string]bool) ([]byte, error) {
	ids := make([]string, 0, len(templates))
	for id := range templates {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var buffer bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buffer, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if strings.ContainsRune(id, 0) || strings.ContainsRune(templates[id], 0) {
			return nil, fmt.Errorf("%s: a NUL byte in the record separator's own alphabet", id)
		}
		flag := "0"
		if deprecated[id] {
			flag = "1"
		}
		if _, err := writer.Write([]byte(id + "\x00" + flag + "\x00" + templates[id] + "\x00")); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func generate(source string, workers int) (string, []byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	var index licenseIndex
	if err := fetchJSON(client, source+"/licenses.json", &index); err != nil {
		return "", nil, fmt.Errorf("fetching the license index: %w", err)
	}
	if len(index.Licenses) == 0 {
		return "", nil, fmt.Errorf("the license index is empty")
	}

	type result struct {
		id       string
		digest   string
		template string
		err      error
	}
	deprecated := map[string]bool{}
	ids := make([]string, 0, len(index.Licenses))
	for _, entry := range index.Licenses {
		ids = append(ids, entry.ID)
		deprecated[entry.ID] = entry.Deprecated
	}
	sort.Strings(ids)

	jobs := make(chan string)
	results := make(chan result)
	var group sync.WaitGroup
	for i := 0; i < workers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for id := range jobs {
				var detail licenseDetail
				if err := fetchJSON(client, source+"/details/"+id+".json", &detail); err != nil {
					results <- result{id: id, err: err}
					continue
				}
				normalized := license.NormalizeText(detail.Text)
				if normalized == "" {
					results <- result{id: id, template: detail.Template}
					continue
				}
				sum := sha256.Sum256([]byte(normalized))
				results <- result{id: id, digest: hex.EncodeToString(sum[:]), template: detail.Template}
			}
		}()
	}
	go func() {
		for _, id := range ids {
			jobs <- id
		}
		close(jobs)
		group.Wait()
		close(results)
	}()

	// A digest maps to one identifier. Some licenses normalize identically --
	// GPL-2.0 and GPL-2.0-only, for instance, differ only in a header this
	// normalization strips. A current identifier always beats a deprecated
	// one, because emitting a deprecated SPDX id in a compliance document is
	// a defect; among equals the lexically first wins, which keeps the table
	// deterministic.
	byDigest := map[string]string{}
	byTemplate := map[string]string{}
	var failures []string
	for r := range results {
		if r.template != "" {
			// Every license keeps its own template, deprecated ones included:
			// unlike a digest, a template is not a key that two licenses can
			// collide on, and a file matching a deprecated license should be
			// recognized and reported rather than missed.
			byTemplate[r.id] = r.template
		}
		switch {
		case r.err != nil:
			failures = append(failures, r.id)
		case r.digest == "":
			// A license with no text, such as some deprecated entries.
		default:
			existing, taken := byDigest[r.digest]
			if !taken || preferID(r.id, existing, deprecated) {
				byDigest[r.digest] = r.id
			}
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		return "", nil, fmt.Errorf("could not fetch %d license text(s): %s", len(failures), strings.Join(failures[:min(5, len(failures))], ", "))
	}
	if len(byTemplate) == 0 {
		return "", nil, fmt.Errorf("the list carried no license templates")
	}

	digests := make([]string, 0, len(byDigest))
	for digest := range byDigest {
		digests = append(digests, digest)
	}
	sort.Strings(digests)

	var builder strings.Builder
	fmt.Fprintf(&builder, `// Code generated by tools/spdxgen. DO NOT EDIT.
//
// SHA-256 digests of the normalized official SPDX license texts, per
// specification section 22.3 technique 2. Only the digests are stored; the
// texts are not embedded.
//
// SPDX license list version: %s
// Licenses in the list: %d
// Distinct normalized texts: %d
//
// The templates that go with them are in spdxtemplates.gz, written by the same
// command.

package license

var knownLicenseHashes = map[string]string{
`, index.Version, len(index.Licenses), len(digests))
	for _, digest := range digests {
		fmt.Fprintf(&builder, "\t%q: %q,\n", digest, byDigest[digest])
	}
	builder.WriteString("}\n")

	blob, err := writeTemplateBlob(byTemplate, deprecated)
	if err != nil {
		return "", nil, fmt.Errorf("packing the license templates: %w", err)
	}
	return builder.String(), blob, nil
}

// preferID reports whether candidate should replace existing.
func preferID(candidate, existing string, deprecated map[string]bool) bool {
	if deprecated[existing] != deprecated[candidate] {
		return !deprecated[candidate]
	}
	return candidate < existing
}

func fetchJSON(client *http.Client, url string, into any) error {
	response, err := client.Get(url)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, response.Status)
	}
	return json.NewDecoder(response.Body).Decode(into)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
