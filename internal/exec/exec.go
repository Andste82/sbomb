// Package exec is the single gateway through which sbomb may run another
// program. Specification section 9.2 draws a hard line: build commands MUST
// NOT be executed, and a fixed allowlist of *introspection* commands MAY be,
// only when the caller has explicitly enabled it.
//
// Everything that makes that line safe lives here rather than at the call
// sites: the allowlist, the absence of a shell, the argument validation, the
// timeout, the output bound and the log record. An adapter cannot reach around
// it, because it has no other way to start a process.
package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	osexec "os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DefaultTimeout and DefaultOutputLimit are the bounds section 9.2 requires.
const (
	DefaultTimeout     = 30 * time.Second
	DefaultOutputLimit = 64 << 20
)

var (
	// ErrNotAllowed reports a command that is not on the allowlist. It is not
	// a failure of the command; it is a refusal to run it.
	ErrNotAllowed = errors.New("command is not on the introspection allowlist")
	// ErrDisabled reports that introspection was not enabled. Callers turn
	// this into an informational finding naming the evidence they could not
	// obtain, rather than degrading silently.
	ErrDisabled = errors.New("introspection is disabled")
	// ErrPathOutsideAnchors reports a path argument that lies outside every
	// registered anchor, which section 9.2 forbids passing to a subprocess.
	ErrPathOutsideAnchors = errors.New("path argument lies outside every registered anchor")
	// ErrOutputLimitExceeded reports a command that produced more than the
	// bound allows. Whatever was read before the bound is returned with it.
	ErrOutputLimitExceeded = errors.New("command output exceeded the limit")
)

// command is one allowlisted invocation shape. Fixed holds the arguments that
// must appear literally; Slots names the positions a caller may fill.
type command struct {
	// Name is the program. A caller may pass an absolute path to it, but the
	// base name has to match.
	Name string
	// Fixed are the literal arguments, in order, with "" marking a slot the
	// caller fills.
	Fixed []string
	// PathSlots are the indices of Fixed whose value is a filesystem path and
	// must therefore lie within a registered anchor.
	PathSlots []int
	// Feature names the introspection switch that must be on
	// (build.introspection.*).
	Feature string
}

// allowlist is the exhaustive table of section 9.2. Nothing outside it runs.
// The shapes are exact: a caller supplies values for the empty slots and
// nothing else, so no untrusted string is ever interpolated into an argument
// that the program parses as an option.
//
// Five shapes section 9.2 lists are absent, because nothing can call them
// without guessing, running a build command, or -- in the case of
// `ninja -t deps` -- rewriting the build directory it was only asked to read;
// deviation D29 records what was measured. An allowlist entry no code reaches
// is not a capability held in reserve, it is a permission granted for nothing.
var allowlist = []command{
	{Name: "ninja", Fixed: []string{"-C", "", "-t", "commands", ""}, PathSlots: []int{1}, Feature: "ninja"},
	{Name: "ninja", Fixed: []string{"-C", "", "-t", "inputs", ""}, PathSlots: []int{1}, Feature: "ninja"},

	{Name: "git", Fixed: []string{"-C", "", "rev-parse", "HEAD"}, PathSlots: []int{1}, Feature: "git"},
	{Name: "git", Fixed: []string{"-C", "", "describe", "--tags", "--always", "--dirty"}, PathSlots: []int{1}, Feature: "git"},
	{Name: "git", Fixed: []string{"-C", "", "config", "--get", "remote.origin.url"}, PathSlots: []int{1}, Feature: "git"},

	{Name: "dpkg", Fixed: []string{"-S", ""}, PathSlots: []int{1}, Feature: "osPackages"},
	{Name: "rpm", Fixed: []string{"-qf", ""}, PathSlots: []int{1}, Feature: "osPackages"},
}

// compilerProbes are the argument shapes permitted for any compiler. The
// program is whatever the build evidence named as the compiler, which is why
// it cannot be listed by name.
var compilerProbes = [][]string{
	{"--version"},
	{"-dumpmachine"},
	{"-print-search-dirs"},
}

// Features says which introspection groups are enabled. All of them are off
// unless the caller turns them on, per section 9.2.
type Features struct {
	Ninja      bool
	Git        bool
	OSPackages bool
	Compiler   bool
}

// Enabled reports whether any introspection at all was allowed.
func (f Features) Enabled() bool {
	return f.Ninja || f.Git || f.OSPackages || f.Compiler
}

func (f Features) enabled(feature string) bool {
	switch feature {
	case "ninja":
		return f.Ninja
	case "git":
		return f.Git
	case "osPackages":
		return f.OSPackages
	case "compiler":
		return f.Compiler
	}
	return false
}

// Record is what one invocation is remembered by. Section 9.2 requires every
// executed command to be logged; keeping the records lets the review report
// state exactly which programs ran, which is what makes a run that consulted a
// process distinguishable from one that read nothing but files.
type Record struct {
	Argv     []string
	Duration time.Duration
	ExitCode int
	Err      error
}

// Runner executes allowlisted introspection commands. The zero value runs
// nothing, which is the required default.
type Runner struct {
	Features Features
	// Anchors are the roots a path argument must lie within. An empty list
	// means no path argument may be passed.
	Anchors []string
	Timeout time.Duration
	// OutputLimit bounds the bytes read from a command.
	OutputLimit int64
	// Exists reports whether a path exists; overridable for tests.
	Exists func(string) bool
	// Log receives every invocation, allowed or refused.
	Log func(Record)

	records []Record
}

// Records returns the invocations this runner attempted, in order.
func (r *Runner) Records() []Record { return append([]Record{}, r.records...) }

// Run executes one allowlisted command and returns its standard output.
// Anything not on the list, or whose paths lie outside the registered anchors,
// is refused before a process is created.
func (r *Runner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if !r.Features.Enabled() {
		return nil, ErrDisabled
	}
	entry, ok := match(name, args)
	if !ok {
		return nil, fmt.Errorf("%w: %s %s", ErrNotAllowed, name, strings.Join(args, " "))
	}
	if !r.Features.enabled(entry.Feature) {
		return nil, fmt.Errorf("%w: %s introspection is off", ErrDisabled, entry.Feature)
	}
	for _, index := range entry.PathSlots {
		if index >= len(args) {
			continue
		}
		if err := r.checkPath(args[index]); err != nil {
			return nil, err
		}
	}
	return r.run(ctx, name, args)
}

// RunCompilerProbe runs one of the permitted compiler probes. The compiler is
// named by the build evidence, so it cannot be on a name allowlist; the
// argument shape is what is constrained.
func (r *Runner) RunCompilerProbe(ctx context.Context, compiler string, args ...string) ([]byte, error) {
	if !r.Features.Compiler {
		return nil, fmt.Errorf("%w: compiler introspection is off", ErrDisabled)
	}
	var permitted bool
	for _, probe := range compilerProbes {
		if equalArgs(probe, args) {
			permitted = true
			break
		}
	}
	if !permitted {
		return nil, fmt.Errorf("%w: %s %s", ErrNotAllowed, compiler, strings.Join(args, " "))
	}
	if err := r.checkProgram(compiler); err != nil {
		return nil, err
	}
	return r.run(ctx, compiler, args)
}

// checkProgram validates the program a compiler probe names. Section 9.2 binds
// path *arguments* to the registered anchors; the program is not an argument,
// and a compiler almost never lies inside the project or the build tree.
// Demanding containment here refused "/usr/bin/cc" while permitting the bare
// name "cc" that PATH resolves to the same binary -- it turned down the exact
// statement and accepted the vague one, and left the compiler group with
// nothing it could actually run. What is still required is that the file
// exists, so a probe never reaches PATH lookup with a path that was meant.
func (r *Runner) checkProgram(path string) error {
	if path == "" {
		return fmt.Errorf("%w: empty path", ErrPathOutsideAnchors)
	}
	if !filepath.IsAbs(path) {
		return nil
	}
	exists := r.Exists
	if exists == nil {
		exists = defaultExists
	}
	if !exists(path) {
		return fmt.Errorf("%w: %s does not exist", ErrPathOutsideAnchors, path)
	}
	return nil
}

func (r *Runner) run(ctx context.Context, name string, args []string) ([]byte, error) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	limit := r.OutputLimit
	if limit <= 0 {
		limit = DefaultOutputLimit
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// No shell: the argument vector goes to the program as given, so nothing
	// in it can be reinterpreted as a command.
	cmd := osexec.CommandContext(ctx, name, args...)
	cmd.Env = []string{"PATH=" + pathEnv(), "LC_ALL=C"}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	cmd.Stdin = nil

	started := time.Now()
	err := cmd.Run()
	record := Record{
		Argv:     append([]string{name}, args...),
		Duration: time.Since(started),
		ExitCode: cmd.ProcessState.ExitCode(),
		Err:      err,
	}
	r.records = append(r.records, record)
	if r.Log != nil {
		r.Log(record)
	}
	data := out.Bytes()
	if int64(len(data)) > limit {
		return data[:limit], ErrOutputLimitExceeded
	}
	if err != nil {
		return data, err
	}
	return data, nil
}

// checkPath enforces the rule that a path argument must exist and lie within a
// registered anchor. Without it, a configuration value could point a
// subprocess at anything on the machine.
func (r *Runner) checkPath(path string) error {
	if path == "" {
		return fmt.Errorf("%w: empty path", ErrPathOutsideAnchors)
	}
	exists := r.Exists
	if exists == nil {
		exists = defaultExists
	}
	if !exists(path) {
		return fmt.Errorf("%w: %s does not exist", ErrPathOutsideAnchors, path)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrPathOutsideAnchors, path)
	}
	for _, anchor := range r.Anchors {
		root, err := filepath.Abs(anchor)
		if err != nil {
			continue
		}
		if absolute == root || strings.HasPrefix(absolute, strings.TrimSuffix(root, string(filepath.Separator))+string(filepath.Separator)) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrPathOutsideAnchors, path)
}

// match finds the allowlist entry an invocation has the shape of.
func match(name string, args []string) (command, bool) {
	base := strings.TrimSuffix(filepath.Base(name), ".exe")
	for _, entry := range allowlist {
		if entry.Name != base || len(entry.Fixed) != len(args) {
			continue
		}
		matched := true
		for index, fixed := range entry.Fixed {
			if fixed == "" {
				// A slot the caller fills. It must not look like an option:
				// otherwise a crafted value could change what the program does.
				if strings.HasPrefix(args[index], "-") {
					matched = false
					break
				}
				continue
			}
			if args[index] != fixed {
				matched = false
				break
			}
		}
		if matched {
			return entry, true
		}
	}
	return command{}, false
}

func equalArgs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Allowlist returns every permitted command shape, for documentation and for
// the tests that hold the table against section 9.2.
func Allowlist() []string {
	return AllowlistFor(Features{Ninja: true, Git: true, OSPackages: true, Compiler: true})
}

// AllowlistFor returns the command shapes the enabled groups permit. A shape
// whose group is off cannot run, so naming it would describe a capability this
// run does not have.
func AllowlistFor(features Features) []string {
	out := make([]string, 0, len(allowlist)+len(compilerProbes))
	for _, entry := range allowlist {
		if !features.enabled(entry.Feature) {
			continue
		}
		parts := append([]string{entry.Name}, entry.Fixed...)
		for index, part := range parts {
			if part == "" {
				parts[index] = "<arg>"
			}
		}
		out = append(out, strings.Join(parts, " "))
	}
	if features.Compiler {
		for _, probe := range compilerProbes {
			out = append(out, "<compiler> "+strings.Join(probe, " "))
		}
	}
	sort.Strings(out)
	return out
}
