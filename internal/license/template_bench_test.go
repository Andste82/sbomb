package license

import "testing"

func BenchmarkTemplateMissThenMatch(b *testing.B) {
	text := uuidLicence
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := ResolveFromText(text, "bench").Expression; got != "BSD-3-Clause" {
			b.Fatalf("got %q", got)
		}
	}
}
