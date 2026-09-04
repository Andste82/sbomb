package ninja

import (
	"strings"
	"testing"
)

// TestParseRealNinjaFile tests parsing a realistic build.ninja file
// simulating GCC compilation
func TestParseRealNinjaFile(t *testing.T) {
	// Simulate a realistic build.ninja for a simple project
	ninjaBuild := `
# Build configuration
builddir = build
srcdir = src

# Variables
cxx = g++
cxxflags = -std=c++17 -Wall -fPIC
ldflags =

rule CXX_COMPILER
  command = $cxx $cxxflags -o $out -c $in
  description = Compiling $out

rule CXX_EXECUTABLE_LINKER
  command = $cxx $ldflags -o $out $in
  description = Linking $out

# Compile rules
build build/CMakeFiles/app.dir/src/main.cpp.o: CXX_COMPILER src/main.cpp
build build/CMakeFiles/app.dir/src/util.cpp.o: CXX_COMPILER src/util.cpp

# Link rule
build build/app: CXX_EXECUTABLE_LINKER build/CMakeFiles/app.dir/src/main.cpp.o build/CMakeFiles/app.dir/src/util.cpp.o
  description = Linking executable app
`

	f, err := ParseFile(strings.NewReader(ninjaBuild))
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	// Verify variables were parsed
	if f.Variables["cxx"] != "g++" {
		t.Fatalf("expected cxx=g++, got %s", f.Variables["cxx"])
	}
	if f.Variables["builddir"] != "build" {
		t.Fatalf("expected builddir=build, got %s", f.Variables["builddir"])
	}

	// Verify compile rules
	if len(f.Rules) < 2 {
		t.Fatalf("expected at least 2 compile rules, got %d", len(f.Rules))
	}

	// First compile rule
	rule := f.Rules[0]
	if len(rule.Outputs) != 1 || !strings.Contains(rule.Outputs[0], "main.cpp.o") {
		t.Fatalf("expected main.cpp.o output, got %v", rule.Outputs)
	}
	if len(rule.Inputs) != 1 || rule.Inputs[0] != "src/main.cpp" {
		t.Fatalf("expected src/main.cpp input, got %v", rule.Inputs)
	}

	// Verify edges map
	if _, ok := f.Edges["build/CMakeFiles/app.dir/src/main.cpp.o"]; !ok {
		t.Fatalf("expected edge for main.cpp.o output")
	}
}

// TestParseMultipleTargetNinja tests parsing for projects with multiple targets
// that have the same basename (duplicate names scenario)
func TestParseMultipleTargetNinja(t *testing.T) {
	ninjaBuild := `
rule CXX_COMPILER
  command = g++ -o $out -c $in

rule LINK
  command = g++ -o $out $in

# Target A compiles src/main.cpp
build build/targetA/main.cpp.o: CXX_COMPILER src/main.cpp

# Target B compiles different/main.cpp (same basename, different directory)
build build/targetB/main.cpp.o: CXX_COMPILER different/main.cpp

# Link each target
build build/app_a: LINK build/targetA/main.cpp.o
build build/app_b: LINK build/targetB/main.cpp.o
`

	f, err := ParseFile(strings.NewReader(ninjaBuild))
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	// Should have 4 rules (2 compiles, 2 links)
	if len(f.Rules) != 4 {
		t.Fatalf("expected 4 rules, got %d", len(f.Rules))
	}

	// Verify the two object files with same basename map to different sources
	obj_a := f.Edges["build/targetA/main.cpp.o"]
	obj_b := f.Edges["build/targetB/main.cpp.o"]

	if len(obj_a.Inputs) != 1 || obj_a.Inputs[0] != "src/main.cpp" {
		t.Fatalf("targetA should link to src/main.cpp, got %v", obj_a.Inputs)
	}
	if len(obj_b.Inputs) != 1 || obj_b.Inputs[0] != "different/main.cpp" {
		t.Fatalf("targetB should link to different/main.cpp, got %v", obj_b.Inputs)
	}
}

// TestParseWithWindowsPaths tests correct handling of Windows drive letters
func TestParseWithWindowsPaths(t *testing.T) {
	// Windows paths with C$:/path are handled by tokenizer
	// This is tested in the basic tokenizer tests
	t.Skip("Windows path handling validated in basic tokenizer tests")
}
