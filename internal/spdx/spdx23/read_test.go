package spdx23

import (
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/example/sbomb/internal/sbomwriter"
)

func TestComponentFilesFindsTheFilesOfAPackage(t *testing.T) {
	data := renderAllFields(t)
	kitchen := []string{
		"build:_deps/kitchen-src/LICENSE", "build:_deps/kitchen-src/kitchen.c", "build:_deps/kitchen-src/kitchen.h",
		"build:gen/kitchen_config.h", "build:gen/table.c", "build:lib/libkitchen.a", "build:obj/kitchen.o",
	}
	for name, want := range map[string][]string{
		// By name, as a person types it: the files include the ones stated
		// as packages because they have no SHA-1.
		"kitchen": kitchen,
		// By local identity, as the review report prints it.
		"component:kitchen": kitchen,
		"hdr":               {"project:hdr/hdr.h"},
		// A used file by its base name, whether it is a file element or a
		// package that stands for one: it is found, and contains nothing.
		"kitchen.c": {},
		"hdr.h":     {},
		// An artifact is a component as well.
		"app": {},
	} {
		got, err := ComponentFiles(data, name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		sort.Strings(got)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v, want %v", name, got, want)
		}
	}
	// The product is the document's root, a patch and a file that is only
	// evidence are not components: CycloneDX lists none of them as one.
	for _, name := range []string{"nowhere", "sentinel-product", "0002-sentinel.patch", "NOTICE"} {
		if _, err := ComponentFiles(data, name); !errors.Is(err, sbomwriter.ErrNoSuchComponent) {
			t.Errorf("%s: %v, want ErrNoSuchComponent", name, err)
		}
	}
}
