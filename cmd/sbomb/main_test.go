package main

import "testing"

func TestExecuteVersion(t *testing.T) {
	code, _, _ := execute([]string{"version"})
	if code != 0 {
		t.Fatalf("version exit code = %d, want 0", code)
	}
}

func TestExecuteUnknownFlag(t *testing.T) {
	code, _, _ := execute([]string{"--bogus"})
	if code != 1 {
		t.Fatalf("--bogus exit code = %d, want 1", code)
	}
}
