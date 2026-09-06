package headers

import "testing"

func TestClassificationUsesToolchainReportedDirectories(t *testing.T) {
	// Section 14.4: classification must use what the toolchain reports, not a
	// hardcoded path list. This header sits nowhere near /usr/include.
	c := New([]string{"/opt/gcc-13/lib/gcc/arm-none-eabi/13.2.1/include"}, nil, nil)

	class := c.Classify(Input{
		Canonical: "toolchain:gcc/lib/gcc/arm-none-eabi/13.2.1/include/stdint.h",
		Physical:  "/opt/gcc-13/lib/gcc/arm-none-eabi/13.2.1/include/stdint.h",
	})
	if class != ClassCompilerRuntime {
		t.Errorf("class = %q, want %q", class, ClassCompilerRuntime)
	}
	if IncludedByDefault(class) {
		t.Error("a compiler runtime header is excluded by default")
	}
}

func TestToolchainInstallationOutsideItsIncludeDirsIsSystem(t *testing.T) {
	// A toolchain anchor spans the whole installation. Only the compiler's own
	// include directories make a header a compiler runtime header.
	c := New([]string{"/opt/gcc-13/lib/gcc/include"}, nil, nil)

	class := c.Classify(Input{
		Canonical: "toolchain:gcc/arm-none-eabi/include/stdio.h",
		Physical:  "/opt/gcc-13/arm-none-eabi/include/stdio.h",
	})
	if class != ClassSystem {
		t.Errorf("class = %q, want %q", class, ClassSystem)
	}
}

func TestEachAnchorKindMapsToItsClass(t *testing.T) {
	c := New(nil, nil, nil)
	cases := []struct {
		canonical string
		physical  string
		want      Class
	}{
		{"project:src/app.h", "", ClassProject},
		{"build:generated/config.h", "", ClassGenerated},
		{"sdk:esp-idf/components/log/log.h", "", ClassSDK},
		{"pkg:conan/mbedtls/aes.h", "", ClassThirdParty},
		{"extern:vendor/blob.h", "", ClassThirdParty},
		{"sysroot:linux/usr/include/stdio.h", "", ClassSystem},
		{"abs:usr/include/stdio.h", "/usr/include/stdio.h", ClassSystem},
		{"abs:opt/mystery/thing.h", "/opt/mystery/thing.h", ClassUnknown},
	}
	for _, tc := range cases {
		if got := c.Classify(Input{Canonical: tc.canonical, Physical: tc.physical}); got != tc.want {
			t.Errorf("Classify(%q) = %q, want %q", tc.canonical, got, tc.want)
		}
	}
}

func TestGeneratedFlagOutranksTheAnchor(t *testing.T) {
	c := New(nil, nil, nil)
	class := c.Classify(Input{Canonical: "project:src/version.h", Generated: true})
	if class != ClassGenerated {
		t.Errorf("class = %q, want %q", class, ClassGenerated)
	}
}

func TestExternalComponentRootMakesProjectHeadersThirdParty(t *testing.T) {
	// A vendored dependency lives under the project anchor but is not project
	// code (section 14.4, third-party-header).
	c := New(nil, nil, []ComponentRoot{{Canonical: "project:dep/mbedtls", External: true}})

	if got := c.Classify(Input{Canonical: "project:dep/mbedtls/aes.h"}); got != ClassThirdParty {
		t.Errorf("vendored header = %q, want %q", got, ClassThirdParty)
	}
	if got := c.Classify(Input{Canonical: "project:src/app.h"}); got != ClassProject {
		t.Errorf("project header = %q, want %q", got, ClassProject)
	}
	// A prefix that is not a whole path segment must not match.
	if got := c.Classify(Input{Canonical: "project:dep/mbedtls-extra/x.h"}); got != ClassProject {
		t.Errorf("sibling directory = %q, want %q", got, ClassProject)
	}
}

func TestNestedComponentRootWins(t *testing.T) {
	c := New(nil, nil, []ComponentRoot{
		{Canonical: "project:dep", External: true},
		{Canonical: "project:dep/vendor-sdk", SDK: true},
	})
	if got := c.Classify(Input{Canonical: "project:dep/vendor-sdk/api.h"}); got != ClassSDK {
		t.Errorf("class = %q, want %q", got, ClassSDK)
	}
}

func TestUnknownHeadersAreIncludedAndFlagged(t *testing.T) {
	// Section 14.4 as clarified: review means include and flag, not omit.
	if !IncludedByDefault(ClassUnknown) {
		t.Error("an unclassifiable header must still be included")
	}
}
