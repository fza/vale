package lint

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/errata-ai/vale/v3/internal/core"
)

// cacheFixture is a styles directory, a cache directory, and a linter that
// uses both.
type cacheFixture struct {
	styles string
	cache  string
	t      *testing.T
}

func newCacheFixture(t *testing.T) *cacheFixture {
	t.Helper()

	f := &cacheFixture{styles: t.TempDir(), cache: t.TempDir(), t: t}
	f.writeRule("Wordy.yml", "obviously")

	return f
}

func (f *cacheFixture) writeRule(name, token string) {
	f.t.Helper()

	dir := filepath.Join(f.styles, "Demo")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		f.t.Fatal(err)
	}

	body := "extends: existence\nmessage: \"Avoid '%s'.\"\nlevel: warning\ntokens:\n  - " + token + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

// linter builds a linter over the fixture's styles, with the cache switched on
// unless version is empty.
func (f *cacheFixture) linter(version string) *Linter {
	f.t.Helper()

	cfg, err := core.NewConfig(&core.CLIFlags{IgnoreGlobal: true})
	if err != nil {
		f.t.Fatal(err)
	}
	cfg.AddStylesPath(f.styles)
	cfg.Styles = []string{"Demo"}
	cfg.GBaseStyles = []string{"Demo"}
	cfg.MinAlertLevel = 0

	linter, err := NewLinter(cfg)
	if err != nil {
		f.t.Fatal(err)
	}

	if version != "" {
		if err = linter.EnableCache(f.cache, version); err != nil {
			f.t.Fatal(err)
		}
	}

	return linter
}

// write puts body at name inside a fresh directory and returns the path.
func (f *cacheFixture) write(dir, name, body string) string {
	f.t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		f.t.Fatal(err)
	}

	return path
}

func (f *cacheFixture) lint(l *Linter, path string) []string {
	f.t.Helper()

	linted, err := l.Lint([]string{path}, "*")
	if err != nil {
		f.t.Fatal(err)
	}

	var out []string
	for _, file := range linted {
		for _, a := range file.SortedAlerts() {
			out = append(out, a.Check+":"+a.Message)
		}
	}

	return out
}

func (f *cacheFixture) entries() int {
	f.t.Helper()

	count := 0
	err := filepath.Walk(f.cache, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Base(path) == "trim.txt" {
			return nil //nolint:nilerr // counting, not reading
		}
		count++
		return nil
	})
	if err != nil {
		f.t.Fatal(err)
	}

	return count
}

// A cached answer is the answer the file would have produced.
func TestCachedRunMatchesUncachedRun(t *testing.T) {
	f := newCacheFixture(t)
	path := f.write(t.TempDir(), "doc.md", "This is obviously wrong.\n")

	cold := f.lint(f.linter("v1"), path)
	if len(cold) == 0 {
		t.Fatal("the fixture rule must fire")
	}

	warm := f.lint(f.linter("v1"), path)
	if strings.Join(warm, "|") != strings.Join(cold, "|") {
		t.Fatalf("warm = %v, cold = %v", warm, cold)
	}
	if f.entries() != 1 {
		t.Fatalf("stored %d entries, want 1", f.entries())
	}
}

// The content is part of the key, so an edit is picked up.
func TestEditedFileIsNotServedFromCache(t *testing.T) {
	f := newCacheFixture(t)
	dir := t.TempDir()
	path := f.write(dir, "doc.md", "This is obviously wrong.\n")

	if got := f.lint(f.linter("v1"), path); len(got) != 1 {
		t.Fatalf("got %v, want one alert", got)
	}

	f.write(dir, "doc.md", "This is fine.\n")
	if got := f.lint(f.linter("v1"), path); len(got) != 0 {
		t.Fatalf("got %v, want none", got)
	}
}

// The same bytes under two names are two questions: the configuration assigns
// rules by path.
func TestPathIsPartOfTheKey(t *testing.T) {
	f := newCacheFixture(t)
	dir := t.TempDir()

	body := "This is obviously wrong.\n"
	first := f.write(dir, "one.md", body)
	second := f.write(dir, "two.md", body)

	f.lint(f.linter("v1"), first)
	f.lint(f.linter("v1"), second)

	if f.entries() != 2 {
		t.Fatalf("stored %d entries, want 2", f.entries())
	}
}

// The styles are part of the salt, so editing a rule is picked up without
// anyone clearing the cache.
func TestEditedStyleInvalidatesTheCache(t *testing.T) {
	f := newCacheFixture(t)
	path := f.write(t.TempDir(), "doc.md", "This is obviously wrong.\n")

	if got := f.lint(f.linter("v1"), path); len(got) != 1 {
		t.Fatalf("got %v, want one alert", got)
	}

	f.writeRule("Wordy.yml", "definitely")
	if got := f.lint(f.linter("v1"), path); len(got) != 0 {
		t.Fatalf("got %v, want none after the rule changed", got)
	}
}

// A build that reports differently must not read what an earlier one wrote.
func TestVersionInvalidatesTheCache(t *testing.T) {
	f := newCacheFixture(t)
	path := f.write(t.TempDir(), "doc.md", "This is obviously wrong.\n")

	f.lint(f.linter("v1"), path)
	f.lint(f.linter("v2"), path)

	if f.entries() != 2 {
		t.Fatalf("stored %d entries, want 2", f.entries())
	}
}

// A format whose converter reads the filesystem is never stored: the key
// cannot name what the answer depends on.
func TestOnlySelfContainedFormatsAreCached(t *testing.T) {
	f := newCacheFixture(t)
	linter := f.linter("v1")

	cases := map[string]bool{
		"doc.md":   true,
		"doc.html": true,
		"doc.go":   true,
		"doc.adoc": false,
		"doc.rst":  false,
		"doc.dita": false,
		"doc.xml":  false,
		"doc.txt":  false,
		"doc.yml":  false,
	}

	for name, want := range cases {
		if got := linter.cacheable(filepath.Join("dir", name)); got != want {
			t.Errorf("cacheable(%s) = %v, want %v", name, got, want)
		}
	}
}

// Nothing is stored when caching is off.
func TestUncachedLinterStoresNothing(t *testing.T) {
	f := newCacheFixture(t)
	path := f.write(t.TempDir(), "doc.md", "This is obviously wrong.\n")

	f.lint(f.linter(""), path)

	if f.entries() != 0 {
		t.Fatalf("stored %d entries, want 0", f.entries())
	}
}

// saltFor builds the salt for a configuration the fixture's tests can mutate.
func (f *cacheFixture) saltFor(edit func(*core.Config)) string {
	f.t.Helper()

	cfg, err := core.NewConfig(&core.CLIFlags{IgnoreGlobal: true})
	if err != nil {
		f.t.Fatal(err)
	}
	cfg.AddStylesPath(f.styles)
	edit(cfg)

	salt, err := cacheSalt(cfg, "v1")
	if err != nil {
		f.t.Fatal(err)
	}

	return string(salt)
}

// Resolution drops what it has no field for, so the configuration files are
// hashed as well as the values read out of them.
func TestConfigFileContentIsInTheSalt(t *testing.T) {
	f := newCacheFixture(t)
	ini := f.write(t.TempDir(), ".vale.ini", "MinAlertLevel = suggestion\n")

	before := f.saltFor(func(cfg *core.Config) {
		cfg.ConfigFiles = []string{ini}
	})

	if err := os.WriteFile(ini, []byte("MinAlertLevel = error\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	after := f.saltFor(func(cfg *core.Config) {
		cfg.ConfigFiles = []string{ini}
	})

	if before == after {
		t.Error("editing a configuration file must change the salt")
	}
}

// A value that decides which alerts exist belongs in the salt; one that decides
// how they are printed does not.
func TestSaltTracksWhatChangesTheAlerts(t *testing.T) {
	f := newCacheFixture(t)
	base := f.saltFor(func(_ *core.Config) {})

	changes := map[string]func(*core.Config){
		"MinAlertLevel": func(cfg *core.Config) { cfg.MinAlertLevel = 2 },
		"RuleToLevel":   func(cfg *core.Config) { cfg.RuleToLevel = map[string]string{"Demo.Wordy": "error"} },
		"GBaseStyles":   func(cfg *core.Config) { cfg.GBaseStyles = []string{"Demo"} },
		"Vocab":         func(cfg *core.Config) { cfg.Vocab = []string{"Project"} },
		"Flags.Simple":  func(cfg *core.Config) { cfg.Flags.Simple = true },
		"Flags.Filter":  func(cfg *core.Config) { cfg.Flags.Filter = ".Level == 'error'" },
	}

	for name, edit := range changes {
		if f.saltFor(edit) == base {
			t.Errorf("%s must change the salt", name)
		}
	}

	presentation := map[string]func(*core.Config){
		"Flags.Output":    func(cfg *core.Config) { cfg.Flags.Output = "JSON" },
		"Flags.Sorted":    func(cfg *core.Config) { cfg.Flags.Sorted = true },
		"Flags.Relative":  func(cfg *core.Config) { cfg.Flags.Relative = true },
		"Flags.Normalize": func(cfg *core.Config) { cfg.Flags.Normalize = true },
	}

	for name, edit := range presentation {
		if f.saltFor(edit) != base {
			t.Errorf("%s must not change the salt", name)
		}
	}
}

// Flags are not part of the marshalled configuration, so each one has to be
// accounted for by hand. A flag added without a decision fails here rather
// than silently serving an answer it should have invalidated.
func TestEveryFlagIsClassified(t *testing.T) {
	// Named in the salt, because they decide which alerts exist.
	inSalt := map[string]bool{
		"Simple":  true,
		"Filter":  true,
		"Sources": true,
	}

	// Left out, because they feed a field the marshalled configuration already
	// carries, or because they decide only how the run is reported.
	outOfSalt := map[string]bool{
		"AlertLevel":    true, // resolved into Config.MinAlertLevel
		"Path":          true, // resolved into Config.ConfigFiles
		"IgnoreGlobal":  true, // same
		"Local":         true, // same
		"Built":         true, // sync only
		"Remote":        true, // sync only
		"InExt":         true, // stdin, which is never cached
		"InPath":        true, // same
		"Glob":          true, // selects files, not alerts
		"Output":        true,
		"NoColor":       true,
		"NoExit":        true,
		"NoCache":       true,
		"Normalize":     true,
		"PlainProgress": true,
		"Relative":      true,
		"Sorted":        true,
		"Wrap":          true,
		"Version":       true,
		"Help":          true,
	}

	flags := reflect.TypeOf(core.CLIFlags{})
	for i := range flags.NumField() {
		name := flags.Field(i).Name
		if !inSalt[name] && !outOfSalt[name] {
			t.Errorf("flag %q is not classified: decide whether it changes "+
				"which alerts exist, and add it to cacheSalt if it does", name)
		}
	}
}
