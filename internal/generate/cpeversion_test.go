package generate

import "testing"

// An upstream CPE leaves {} where the version belongs. ESP-IDF manifests
// routinely do, expecting the version to come from the checkout -- which it
// does not, when introspection is off. The placeholder must not reach the
// document: it matches nothing in a vulnerability feed, while the string reads
// as though a version had been stated.
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
		{"no cpe", "", "1.0", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := cpeVersion(c.cpe, c.version); got != c.want {
				t.Errorf("cpeVersion(%q, %q) = %q, want %q", c.cpe, c.version, got, c.want)
			}
		})
	}
}
