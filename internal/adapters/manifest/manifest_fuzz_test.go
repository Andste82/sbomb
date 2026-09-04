package manifest

import "testing"

func FuzzParse(f *testing.F) {
	f.Add([]byte(`{"schemaVersion":1,"outputs":[]}`))
	f.Add([]byte(`{"schemaVersion":1,"outputs":[{"path":"../escape","inputs":[]}]}`))
	f.Fuzz(func(t *testing.T, input []byte) {
		_, _ = Parse(input)
	})
}
