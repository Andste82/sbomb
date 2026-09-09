package msbuild

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func encodeUTF16(data string, bom bool) []byte {
	out := make([]byte, 0, len(data)*2+2)
	if bom {
		out = append(out, 0xff, 0xfe)
	}
	for _, r := range data {
		var pair [2]byte
		binary.LittleEndian.PutUint16(pair[:], uint16(r))
		out = append(out, pair[:]...)
	}
	return out
}

func TestDecodeTLogUTF16LE(t *testing.T) {
	for _, test := range []struct {
		name string
		bom  bool
	}{{"bom", true}, {"without bom", false}} {
		t.Run(test.name, func(t *testing.T) {
			got, err := DecodeTLog(encodeUTF16("C:\\src\\main.c\n", test.bom))
			if err != nil || got != "C:\\src\\main.c\n" {
				t.Fatalf("got %q, err %v", got, err)
			}
		})
	}
}

func TestDecodeTLogRejectsOddCodeUnit(t *testing.T) {
	if _, err := DecodeTLog([]byte{0xff, 0xfe, 0x41}); err == nil {
		t.Fatal("truncated UTF-16LE was accepted")
	}
}

func TestReadTLogMapping(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CL.write.1.tlog")
	if err := os.WriteFile(path, encodeUTF16("C:\\build\\main.obj\tC:\\src\\main.c\n", true), 0o600); err != nil {
		t.Fatal(err)
	}
	evidence, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence.Mappings) != 1 || evidence.Mappings[0].Strategy != "msbuild-tlog" {
		t.Fatalf("mappings = %#v", evidence.Mappings)
	}
}

func TestReadTLogCaretPairMapping(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CL.write.1.tlog")
	text := "^C:\\src\\main.c\nC:\\build\\main.obj\n"
	if err := os.WriteFile(path, encodeUTF16(text, false), 0o600); err != nil {
		t.Fatal(err)
	}
	evidence, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence.Mappings) != 1 || evidence.Mappings[0].Source != "C:\\src\\main.c" || evidence.Mappings[0].Object != "C:\\build\\main.obj" {
		t.Fatalf("mappings = %#v", evidence.Mappings)
	}
}

func TestReadTLogHeaderEvidence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CL.read.1.tlog")
	text := "^C:\\src\\main.c\nC:\\build\\main.obj\nC:\\SDK\\stdio.h\n"
	if err := os.WriteFile(path, encodeUTF16(text, true), 0o600); err != nil {
		t.Fatal(err)
	}
	evidence, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	object := `C:\build\main.obj`
	header := `C:\SDK\stdio.h`
	if len(evidence.Headers[object]) != 1 || evidence.Headers[object][0] != header {
		t.Fatalf("headers = %#v", evidence.Headers)
	}
}

func TestReadCommittedVisualStudioFixture(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "testdata", "fixtures", "msvc-vs17", "p02-static", "build")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skip("Visual Studio fixture not present")
	}
	evidence, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence.Mappings) == 0 {
		t.Fatal("Visual Studio fixture produced no TLog mappings")
	}
	var sawMain bool
	for _, mapping := range evidence.Mappings {
		if strings.HasSuffix(strings.ToLower(mapping.Source), "main.c") && strings.HasSuffix(strings.ToLower(mapping.Object), "main.obj") {
			sawMain = true
		}
	}
	if !sawMain {
		t.Fatalf("no main.c -> main.obj mapping in %d mappings", len(evidence.Mappings))
	}
}

func TestReadTLogResourceInput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "RC.write.1.tlog")
	text := "^C:\\src\\app.rc\nC:\\build\\app.res\n"
	if err := os.WriteFile(path, encodeUTF16(text, false), 0o600); err != nil {
		t.Fatal(err)
	}
	evidence, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence.Mappings) != 1 || evidence.Mappings[0].Source != "C:\\src\\app.rc" || evidence.Mappings[0].Object != "C:\\build\\app.res" {
		t.Fatalf("rc mappings = %#v", evidence.Mappings)
	}
}

func TestReadTLogOrphanObject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CL.write.1.tlog")
	text := "^C:\\src\\orphan.c\nC:\\build\\orphan.obj\n"
	if err := os.WriteFile(path, encodeUTF16(text, false), 0o600); err != nil {
		t.Fatal(err)
	}
	evidence, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence.Mappings) != 1 || evidence.Mappings[0].Object != "C:\\build\\orphan.obj" {
		t.Fatalf("orphan mappings = %#v", evidence.Mappings)
	}
}
