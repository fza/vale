package lint

import (
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/system"
)

// TestConfigFS tests that a Config reads its assets from its own filesystem:
// two configurations over two in-memory filesystems see only their own
// rules, and neither touches system.Default.
func TestConfigFS(t *testing.T) {
	ini := "StylesPath = /styles\nMinAlertLevel = suggestion\n\n[*]\nBasedOnStyles = T\n"
	rule := func(token string) []byte {
		return []byte("extends: existence\nmessage: \"'%s'\"\nlevel: error\ntokens:\n  - " + token + "\n")
	}

	one := system.NewMemFS()
	one.AddFile("/styles/T/W.yml", rule("very"))
	two := system.NewMemFS()
	two.AddFile("/styles/T/W.yml", rule("good"))

	for _, c := range []struct {
		fs   *system.MemFS
		want string
	}{{one, "very"}, {two, "good"}} {
		cfg, err := core.LoadConfigFromStringFS(c.fs, ini)
		if err != nil {
			t.Fatal(err)
		}
		linter, err := NewLinter(cfg)
		if err != nil {
			t.Fatal(err)
		}
		linted, err := linter.LintStringAs("This is very good.\n", "input.md")
		if err != nil {
			t.Fatal(err)
		}
		alerts := linted[0].Alerts
		if len(alerts) != 1 || alerts[0].Match != c.want {
			t.Fatalf("got %v; want one alert on %q", alerts, c.want)
		}
	}

	if _, isMem := system.Default.(*system.MemFS); isMem {
		t.Fatal("system.Default was replaced")
	}
}
