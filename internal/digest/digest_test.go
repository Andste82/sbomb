package digest

import "testing"

func TestCompute(t *testing.T) {
	for name, want := range map[string]string{
		SHA256: "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		SHA1:   "a9993e364706816aba3e25717850c26c9cd0d89d",
	} {
		got, ok := Compute(name, []byte("abc"))
		if !ok || got != want {
			t.Errorf("Compute(%q) = %q, %v; want %q", name, got, ok, want)
		}
	}
	if _, ok := Compute("MD5", []byte("abc")); ok {
		t.Error("Compute(MD5) succeeded; the table names no MD5")
	}
}

func TestSupportedAndOnRequest(t *testing.T) {
	for name, want := range map[string][2]bool{
		SHA256: {true, false},
		SHA1:   {true, true},
		"SHA1": {false, false},
		"MD5":  {false, false},
		"":     {false, false},
	} {
		if got := Supported(name); got != want[0] {
			t.Errorf("Supported(%q) = %v, want %v", name, got, want[0])
		}
		if got := OnRequest(name); got != want[1] {
			t.Errorf("OnRequest(%q) = %v, want %v", name, got, want[1])
		}
	}
}
