package fixtures

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const SentinelSourceRoot = "/__fixture_src__"
const SentinelBuildRoot = "/__fixture_build__"

var hostPathFragments = []string{
	"/home/",
	"/Users/",
	"C:\\Users",
	"/workspaces/",
	"/workspace/",
}

func Toolchains() []string {
	return []string{"gcc-13", "clang-17", "gcc-12", "gcc-12-make", "mingw-w64", "arm-none-eabi"}
}

func Projects() []string {
	return []string{"p01-hello", "p02-static", "p03-dupnames", "p04-generated", "p05-headeronly"}
}

func AllPairs() [][2]string {
	pairs := make([][2]string, 0, len(Toolchains())*len(Projects()))
	for _, toolchain := range Toolchains() {
		for _, project := range Projects() {
			pairs = append(pairs, [2]string{toolchain, project})
		}
	}
	return pairs
}

func FixtureDir(toolchain, project string) string {
	return filepath.Join("testdata", "fixtures", toolchain, project)
}

func NormalizeText(s string) string {
	replacements := []struct{ old, new string }{
		{"/workspaces/sbomb", SentinelSourceRoot},
		{"/workspaces", SentinelSourceRoot},
		{"/__fixture_src__", SentinelSourceRoot},
		{"/__fixture_build__", SentinelBuildRoot},
		{"/tmp/", SentinelBuildRoot + "/"},
		{"/home/", SentinelSourceRoot + "/"},
		{"/Users/", SentinelSourceRoot + "/"},
	}
	for _, r := range replacements {
		s = strings.ReplaceAll(s, r.old, r.new)
	}
	return s
}

func RewriteFixtureTree(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out := NormalizeText(string(b))
		if !bytes.Equal(b, []byte(out)) {
			if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
				return err
			}
		}
		return nil
	})
}

func CheckNoHostPaths(root string) error {
	var bad []string
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(b)
		for _, fragment := range hostPathFragments {
			if strings.Contains(text, fragment) {
				bad = append(bad, path+" contains "+fragment)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if len(bad) > 0 {
		return errors.New(strings.Join(bad, "; "))
	}
	return nil
}

func CheckFixtureProvenance(dir string) error {
	p := filepath.Join(dir, "PROVENANCE.md")
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	text := string(b)
	if !strings.Contains(text, "Toolchain:") || !strings.Contains(text, "Date:") {
		return fmt.Errorf("PROVENANCE.md in %s is not parseable", dir)
	}
	return nil
}
