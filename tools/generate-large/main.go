// Command generate-large writes the `large` fixture of appendix F: a build
// directory shaped like a real one, at the scale section 31 sets a budget for.
//
// It is generated rather than committed because it is two hundred megabytes of
// linker map and fifty thousand of everything else. What matters is that it is
// *runnable*: a compile database, a depfile per object, a map in the format
// GNU ld actually writes, a link dependency file and an artifact. A fixture
// that no run can be pointed at measures nothing.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	root := flag.String("output", "testdata/large", "output directory")
	files := flag.Int("files", 50000, "number of translation units")
	mapSize := flag.Int64("map-bytes", 200<<20, "minimum linker map size")
	headers := flag.Int("headers-per-file", 3, "headers each translation unit includes")
	flag.Parse()

	if *files < 1 || *mapSize < 1 {
		fmt.Fprintln(os.Stderr, "files and map-bytes must be positive")
		os.Exit(2)
	}
	if err := generate(*root, *files, *headers, *mapSize); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("generate-large: %s (%d translation units, map >= %d bytes)\n", *root, *files, *mapSize)
}

// Layout: sources beside the build directory, exactly as a real out-of-source
// build has them, so the anchor model has both roots to work with.
func generate(root string, files, headersPerFile int, mapSize int64) error {
	source := filepath.Join(root, "src")
	build := filepath.Join(root, "build")
	objects := filepath.Join(build, "CMakeFiles", "large.dir")
	for _, dir := range []string{source, filepath.Join(source, "include"), objects} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	// Shared headers: every unit includes a few, which is what makes the
	// header set large without making the file count meaningless.
	for index := 0; index < headersPerFile*4; index++ {
		path := filepath.Join(source, "include", fmt.Sprintf("shared-%02d.h", index))
		content := fmt.Sprintf("#pragma once\nint shared_%02d(void);\n", index)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}

	compileDB, err := os.Create(filepath.Join(build, "compile_commands.json"))
	if err != nil {
		return err
	}
	defer compileDB.Close()
	db := bufio.NewWriterSize(compileDB, 1<<20)
	if _, err := db.WriteString("[\n"); err != nil {
		return err
	}

	for index := 0; index < files; index++ {
		name := fmt.Sprintf("unit-%05d", index)
		sourcePath := filepath.Join(source, name+".c")
		objectPath := filepath.Join(objects, name+".c.o")

		var unit strings.Builder
		for offset := 0; offset < headersPerFile; offset++ {
			unit.WriteString(fmt.Sprintf("#include \"include/shared-%02d.h\"\n", (index+offset)%(headersPerFile*4)))
		}
		unit.WriteString(fmt.Sprintf("\nint %s(void) { return %d; }\n", strings.ReplaceAll(name, "-", "_"), index))
		if err := os.WriteFile(sourcePath, []byte(unit.String()), 0o644); err != nil {
			return err
		}
		// The object has to exist: it is what the link evidence names.
		if err := os.WriteFile(objectPath, []byte{0x7f, 'E', 'L', 'F'}, 0o644); err != nil {
			return err
		}

		// One depfile per object, in the format the compiler writes.
		var depfile strings.Builder
		depfile.WriteString(objectPath + ": " + sourcePath)
		for offset := 0; offset < headersPerFile; offset++ {
			depfile.WriteString(fmt.Sprintf(" \\\n  %s/include/shared-%02d.h", source, (index+offset)%(headersPerFile*4)))
		}
		depfile.WriteString("\n")
		if err := os.WriteFile(objectPath+".d", []byte(depfile.String()), 0o644); err != nil {
			return err
		}

		separator := ",\n"
		if index == files-1 {
			separator = "\n"
		}
		entry := fmt.Sprintf(
			`  {"directory": %q, "file": %q, "output": %q, "arguments": ["cc", "-g", "-c", %q, "-o", %q]}%s`,
			build, sourcePath, objectPath, sourcePath, objectPath, separator)
		if _, err := db.WriteString(entry); err != nil {
			return err
		}
	}
	if _, err := db.WriteString("]\n"); err != nil {
		return err
	}
	if err := db.Flush(); err != nil {
		return err
	}

	if err := writeMap(filepath.Join(build, "large.map"), objects, files, mapSize); err != nil {
		return err
	}
	if err := writeLinkDepfile(filepath.Join(build, "large.d"), objects, files); err != nil {
		return err
	}
	// A Makefile at the build root, because the dependency files this writes
	// are the Makefiles generator's layout and section 9.1 selects the adapter
	// by what generated the tree.
	if err := os.WriteFile(filepath.Join(build, "Makefile"), []byte("all:\n"), 0o644); err != nil {
		return err
	}
	// The deliverable itself. Not a real ELF: what is being measured is the
	// evidence pipeline, and an unparsable artifact is a case the tool has to
	// handle anyway.
	return os.WriteFile(filepath.Join(build, "large"), []byte{0x7f, 'E', 'L', 'F', 0}, 0o644)
}

// writeMap writes a GNU ld map in the shape the parser actually meets: a
// memory map whose placement lines name every object, padded with symbol lines
// until the file reaches the requested size.
func writeMap(path, objects string, files int, minSize int64) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := bufio.NewWriterSize(file, 1<<20)

	if _, err := writer.WriteString("Archive member included to satisfy reference by file (symbol)\n\n"); err != nil {
		return err
	}
	if _, err := writer.WriteString("\nLinker script and memory map\n\n"); err != nil {
		return err
	}

	var written int64
	address := int64(0x1000)
	for index := 0; index < files; index++ {
		object := filepath.Join(objects, fmt.Sprintf("unit-%05d.c.o", index))
		line := fmt.Sprintf("LOAD %s\n", object)
		if _, err := writer.WriteString(line); err != nil {
			return err
		}
		written += int64(len(line))
	}
	if _, err := writer.WriteString("\n.text           0x0000000000001000     0x1000\n"); err != nil {
		return err
	}
	for index := 0; index < files; index++ {
		object := filepath.Join(objects, fmt.Sprintf("unit-%05d.c.o", index))
		line := fmt.Sprintf(" .text          0x%016x       0x20 %s\n", address, object)
		address += 0x20
		if _, err := writer.WriteString(line); err != nil {
			return err
		}
		written += int64(len(line))
	}
	// Symbol lines carry no file and are what a real map is mostly made of.
	filler := fmt.Sprintf("                0x%016x                some_symbol_name\n", address)
	for written < minSize {
		if _, err := writer.WriteString(filler); err != nil {
			return err
		}
		written += int64(len(filler))
	}
	return writer.Flush()
}

func writeLinkDepfile(path, objects string, files int) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := bufio.NewWriterSize(file, 1<<20)
	if _, err := writer.WriteString("large:"); err != nil {
		return err
	}
	for index := 0; index < files; index++ {
		object := filepath.Join(objects, fmt.Sprintf("unit-%05d.c.o", index))
		if _, err := writer.WriteString(" \\\n  " + object); err != nil {
			return err
		}
	}
	if _, err := writer.WriteString("\n"); err != nil {
		return err
	}
	return writer.Flush()
}
