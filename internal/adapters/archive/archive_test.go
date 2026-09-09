package archive

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func arMember(name, body string) []byte {
	header := fmt.Sprintf("%-16s%-12s%-6s%-6s%-8s%-10d`\n", name+"/", "0", "0", "0", "100644", len(body))
	return append([]byte(header), []byte(body)...)
}

func TestParseRegularArchive(t *testing.T) {
	data := append([]byte("!<arch>\n"), arMember("main.o", "object")...)
	members, err := Parse(bytes.NewReader(data), "/tmp/libx.a")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(members) != 1 || members[0].Name != "main.o" || members[0].Thin {
		t.Fatalf("members = %#v", members)
	}
}

func TestParseThinArchiveResolvesRelativePath(t *testing.T) {
	data := append([]byte("!<thin>\n"), arMember("objects/main.o", "")...)
	members, err := Parse(bytes.NewReader(data), "/build/lib/libx.a")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	wantPath := filepath.Join("/build/lib", "objects", "main.o")
	if len(members) != 1 || members[0].Path != wantPath || !members[0].Thin {
		t.Fatalf("members = %#v", members)
	}
}

func TestParseRealMSVCStaticLibrary(t *testing.T) {
	path := filepath.Join("..", "..", "..", "testdata", "fixtures", "msvc-ninja", "p13-prebuilt", "build", "prebuilt", "libvendor.lib")
	members, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile(%s) error = %v", path, err)
	}
	var sawVendor bool
	for _, member := range members {
		name := strings.ReplaceAll(member.Name, "\\", "/")
		if strings.EqualFold(name, "vendor_blob.obj") || strings.HasSuffix(strings.ToLower(name), "/vendor_blob.obj") {
			sawVendor = true
		}
	}
	if !sawVendor {
		t.Fatalf("real MSVC library members = %#v, want vendor_blob.obj", members)
	}
}
