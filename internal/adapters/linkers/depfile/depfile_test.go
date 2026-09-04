package depfile

import (
	"reflect"
	"testing"
)

func TestParseLinkDepfile(t *testing.T) {
	text := "app.elf: libfoo.a(libfoo.o) libbar.a main.o\n"
	recs, err := ParseString(text)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}
	want := []Record{
		{Target: "app.elf", Path: "libfoo.a(libfoo.o)"},
		{Target: "app.elf", Path: "libbar.a"},
		{Target: "app.elf", Path: "main.o"},
	}
	if !reflect.DeepEqual(recs, want) {
		t.Fatalf("ParseString() = %#v, want %#v", recs, want)
	}
}
