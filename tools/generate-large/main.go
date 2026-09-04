package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	root := flag.String("output", "testdata/large", "output directory")
	files := flag.Int("files", 50000, "number of generated files")
	mapSize := flag.Int64("map-bytes", 200<<20, "linker map size")
	flag.Parse()

	if *files < 1 || *mapSize < 1 {
		fmt.Fprintln(os.Stderr, "files and map-bytes must be positive")
		os.Exit(2)
	}
	if err := os.MkdirAll(filepath.Join(*root, "files"), 0o755); err != nil {
		fatal(err)
	}
	for index := 0; index < *files; index++ {
		path := filepath.Join(*root, "files", fmt.Sprintf("file-%05d.c", index))
		if err := os.WriteFile(path, []byte(fmt.Sprintf("int file_%05d(void) { return %d; }\n", index, index)), 0o644); err != nil {
			fatal(err)
		}
	}
	mapPath := filepath.Join(*root, "large.map")
	mapFile, err := os.Create(mapPath)
	if err != nil {
		fatal(err)
	}
	defer mapFile.Close()
	line := " build/objects/large-object.o .text 0x00000000 0x00000010\n"
	if _, err := mapFile.WriteString("Memory Configuration\n"); err != nil {
		fatal(err)
	}
	remaining := *mapSize - int64(len("Memory Configuration\n"))
	chunk := strings.Repeat(line, 4096)
	for remaining > 0 {
		write := int64(len(chunk))
		if write > remaining {
			write = remaining
		}
		if _, err := mapFile.WriteString(chunk[:write]); err != nil {
			fatal(err)
		}
		remaining -= write
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
