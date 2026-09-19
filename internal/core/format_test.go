package core

import "testing"

func TestFormatFromExtNamesABuildFile(t *testing.T) {
	// A build file carries no extension, so matching on one never reaches it.
	for _, path := range []string{"Makefile", "makefile", "GNUmakefile", "src/Makefile"} {
		ext, format := FormatFromExt(path, map[string]string{})
		if ext != ".mk" || format != "code" {
			t.Errorf("%s: got (%q, %q), want (\".mk\", \"code\")", path, ext, format)
		}
	}

	// A suffixed build file reaches the same language through its extension.
	ext, format := FormatFromExt("rules.mk", map[string]string{})
	if ext != ".mk" || format != "code" {
		t.Errorf("rules.mk: got (%q, %q), want (\".mk\", \"code\")", ext, format)
	}

	// A file merely opening with those letters is not one.
	ext, _ = FormatFromExt("Makefile.md", map[string]string{})
	if ext == ".mk" {
		t.Errorf("Makefile.md should keep its own extension, got %q", ext)
	}
}
