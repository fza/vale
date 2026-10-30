package lint

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
)

// nestedCheckout is a checkout whose docs directory carries a nested
// configuration raising the fixture rule to an error there.
func nestedCheckout(t *testing.T) string {
	t.Helper()

	root := checkout(t, "obviously", everyMarkdownFile)
	writeTree(t, root, map[string]string{"docs/.vale.ini": "[*.md]\nDemo.Wordy = error\n"})

	return root
}

// nestedLinter reads the configuration the way a run started in root does,
// nested files included, for the input doc, and caches into dir.
func nestedLinter(t *testing.T, root, dir, doc string) *Linter {
	t.Helper()

	t.Chdir(root)
	cfg, err := core.ReadPipeline(&core.CLIFlags{IgnoreGlobal: true}, false, doc)
	if err != nil {
		t.Fatal(err)
	}
	linter, err := NewLinter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = linter.EnableCache(dir, "v1"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { linter.CloseCache(nil) })

	return linter
}

// A nested configuration's sections are anchored to the directory as the run
// spelled it, so two checkouts share their entries when the runs spell their
// files alike, relative or absolute, and the nested section still applies.
func TestNestedConfigurationSharesAcrossCheckouts(t *testing.T) {
	spellings := map[string]func(root string) string{
		"relative": func(_ string) string { return filepath.Join("docs", "doc.md") },
		"absolute": docOf,
	}

	for name, spell := range spellings {
		t.Run(name, func(t *testing.T) {
			first, second := nestedCheckout(t), nestedCheckout(t)
			dir := t.TempDir()

			linted, err := nestedLinter(t, first, dir, spell(first)).Lint([]string{spell(first)}, "*")
			if err != nil {
				t.Fatal(err)
			}
			var severities []string
			for _, file := range linted {
				for _, a := range file.Alerts {
					severities = append(severities, a.Severity)
				}
			}
			if !reflect.DeepEqual(severities, []string{"error"}) {
				t.Fatalf("the nested section should raise the rule, got %v", severities)
			}

			doc := spell(second)
			l := nestedLinter(t, second, dir, doc)
			content := []byte(wrongDocument)
			if _, found := l.cache.Get(l.cacheKey(doc, content)); !found {
				t.Fatal("a second checkout spelling its file alike should find the first one's entry")
			}
		})
	}
}
