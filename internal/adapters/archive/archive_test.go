package archive

import (
	"bytes"
	"fmt"
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
	if len(members) != 1 || members[0].Path != "/build/lib/objects/main.o" || !members[0].Thin {
		t.Fatalf("members = %#v", members)
	}
}
