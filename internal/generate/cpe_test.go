package generate

import "testing"

// A cpe may leave {} where the version belongs. Filling it is the only
// derivation allowed; a placeholder that cannot be filled takes the cpe with
// it, because "{}" matches nothing in a vulnerability feed while the string
// reads as though a version had been stated.
func TestCPEVersion(t *testing.T) {
	const placeholder = "cpe:2.3:o:amazon:freertos:{}:*:*:*:*:*:*:*"

	for _, c := range []struct {
		name    string
		cpe     string
		version string
		want    string
	}{
		{"placeholder filled", placeholder, "10.4.3", "cpe:2.3:o:amazon:freertos:10.4.3:*:*:*:*:*:*:*"},
		{"placeholder unfillable", placeholder, "", ""},
		{"no placeholder, no version", "cpe:2.3:a:vendor:lib:1.0:*:*:*:*:*:*:*", "", "cpe:2.3:a:vendor:lib:1.0:*:*:*:*:*:*:*"},
		{"no placeholder, with version", "cpe:2.3:a:vendor:lib:1.0:*:*:*:*:*:*:*", "2.0", "cpe:2.3:a:vendor:lib:1.0:*:*:*:*:*:*:*"},
		{"only the first placeholder", "cpe:2.3:a:v:p:{}:{}:*:*:*:*:*:*", "1.0", "cpe:2.3:a:v:p:1.0:{}:*:*:*:*:*:*"},
		{"no cpe", "", "1.0", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := cpeVersion(c.cpe, c.version); got != c.want {
				t.Errorf("cpeVersion(%q, %q) = %q, want %q", c.cpe, c.version, got, c.want)
			}
		})
	}
}
