package lint

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/vale-cli/vale/v3/internal/cache"
	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/glob"
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

// stored reports whether a linter of version finds an entry for path as it is
// now.
func (f *cacheFixture) stored(version, path string) bool {
	f.t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		f.t.Fatal(err)
	}

	l := f.linter(version)
	defer l.CloseCache(nil)

	_, found := l.cache.Get(l.cacheKey(path, content))
	return found
}

// empty reports whether nothing has been written to the cache directory.
func (f *cacheFixture) empty() bool {
	f.t.Helper()

	size, err := cache.Size(f.cache)
	if err != nil {
		f.t.Fatal(err)
	}

	return size == 0
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
	if !f.stored("v1", path) {
		t.Fatal("the run should have stored the file's alerts")
	}
}

// A file the cache cannot read is linted the way an uncached run lints it,
// rather than ending the run, and nothing is stored for it.
func TestUnreadableFileIsLintedAsUncached(t *testing.T) {
	f := newCacheFixture(t)
	path := f.write(t.TempDir(), "doc.md", "This is obviously wrong.\n")
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("the file stays readable, as it does for root")
	}

	f.lint(f.linter("v1"), path)
	if !f.empty() {
		t.Error("an unreadable file should leave no cache entry behind")
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
	if f.stored("v1", second) {
		t.Fatal("the same bytes under another name should not be found")
	}

	f.lint(f.linter("v1"), second)
	if !f.stored("v1", first) || !f.stored("v1", second) {
		t.Fatal("each name should have an entry of its own")
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
	if f.stored("v2", path) {
		t.Fatal("another version should not read what this one wrote")
	}

	f.lint(f.linter("v2"), path)
	if !f.stored("v1", path) || !f.stored("v2", path) {
		t.Fatal("each version should have an entry of its own")
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

	if !f.empty() {
		t.Fatal("an uncached linter should write nothing")
	}
}

// saltFor builds what a configuration the fixture's tests can mutate adds to
// every key: the salt and the resolved configuration.
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
	scope, err := newCacheScope(cfg)
	if err != nil {
		f.t.Fatal(err)
	}

	return string(salt) + string(scope.config(""))
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
		"Simple":   true,
		"Filter":   true,
		"Sources":  true,
		"NoNested": true,
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
		"Counts":        true, // JSON reporting only
		"Apply":         true, // `vale fix` writes to disk; linting is unaffected
		"Coverage":      true, // `vale test` only
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

const (
	gitHead           = "ref: refs/heads/main\n"
	everyMarkdownFile = "[*.md]\nBasedOnStyles = Demo\n"
	wrongDocument     = "This is obviously wrong.\n"
)

func wordyRule(token string) string {
	return "extends: existence\nmessage: \"Avoid '%s'.\"\nlevel: warning\ntokens:\n  - " + token + "\n"
}

// writeTree writes files, keyed by their slash-separated path, under root.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()

	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// checkout writes one project -- a git checkout holding a configuration, a
// style and a document -- under a fresh directory, as each worktree of one tree
// holds it. config is the configuration past its first two lines; `{root}`
// in it stands for the checkout's own path.
func checkout(t *testing.T, token, config string) string {
	t.Helper()

	root := t.TempDir()
	writeTree(t, root, map[string]string{
		".git/HEAD":             gitHead,
		".vale.ini":             "StylesPath = styles\nMinAlertLevel = suggestion\n" + strings.ReplaceAll(config, "{root}", filepath.ToSlash(root)),
		"styles/Demo/Wordy.yml": wordyRule(token),
		"docs/doc.md":           wrongDocument,
	})

	return root
}

func configOf(root string) string {
	return filepath.Join(root, ".vale.ini")
}

func docOf(root string) string {
	return filepath.Join(root, "docs", "doc.md")
}

// projectLinter reads the configuration at config and caches into dir.
func projectLinter(t *testing.T, config, dir string) *Linter {
	t.Helper()

	cfg, err := core.ReadPipeline(&core.CLIFlags{Path: config, IgnoreGlobal: true}, false)
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

// lintWith lints doc, spelled as given, and returns its alerts' severities.
func lintWith(t *testing.T, config, dir, doc string) []string {
	t.Helper()

	linted, err := projectLinter(t, config, dir).Lint([]string{doc}, "*")
	if err != nil {
		t.Fatal(err)
	}

	var severities []string
	for _, file := range linted {
		for _, a := range file.Alerts {
			severities = append(severities, a.Severity)
		}
	}

	return severities
}

// storedFor reports whether a linter reading config finds an entry for doc,
// spelled as given.
func storedFor(t *testing.T, config, dir, doc string) bool {
	t.Helper()

	content, err := os.ReadFile(doc)
	if err != nil {
		t.Fatal(err)
	}
	l := projectLinter(t, config, dir)
	_, found := l.cache.Get(l.cacheKey(doc, content))

	return found
}

// sharedWith lints the first checkout's document, then reports whether the
// second checkout's linter finds an entry for its own copy.
func sharedWith(t *testing.T, first, second string) bool {
	t.Helper()

	dir := t.TempDir()
	lintWith(t, configOf(first), dir, docOf(first))

	return storedFor(t, configOf(second), dir, docOf(second))
}

// Two checkouts of one tree are one project wherever they sit, so the second
// starts with the first one's entries.
func TestCheckoutsOfOneTreeShareEntries(t *testing.T) {
	first := checkout(t, "obviously", everyMarkdownFile)
	second := checkout(t, "obviously", everyMarkdownFile)

	if !sharedWith(t, first, second) {
		t.Fatal("a second checkout of the same tree should find the first one's entries")
	}
}

// A linked worktree marks its root with a `.git` file pointing at the
// repository rather than with a `.git` directory.
func TestLinkedWorktreeSharesWithItsMainCheckout(t *testing.T) {
	main := checkout(t, "obviously", everyMarkdownFile)
	linked := checkout(t, "obviously", everyMarkdownFile)
	if err := os.RemoveAll(filepath.Join(linked, ".git")); err != nil {
		t.Fatal(err)
	}
	writeTree(t, linked, map[string]string{".git": "gitdir: " + filepath.Join(main, ".git", "worktrees", "linked") + "\n"})

	if !sharedWith(t, main, linked) {
		t.Fatal("a linked worktree should find its main checkout's entries")
	}
}

// Only a real checkout marks where a project starts. Elsewhere a file is
// addressed by its absolute path, which no other copy shares.
func TestNoCheckoutNoSharing(t *testing.T) {
	markers := map[string]map[string]string{
		"no .git":                nil,
		".git without HEAD":      {".git/config": ""},
		".git file without link": {".git": "not a link\n"},
	}

	for name, mark := range markers {
		t.Run(name, func(t *testing.T) {
			var roots []string
			for range 2 {
				root := checkout(t, "obviously", everyMarkdownFile)
				if err := os.RemoveAll(filepath.Join(root, ".git")); err != nil {
					t.Fatal(err)
				}
				writeTree(t, root, mark)
				roots = append(roots, root)
			}

			if sharedWith(t, roots[0], roots[1]) {
				t.Fatal("copies outside any checkout should not share entries")
			}
		})
	}
}

func TestCheckoutsWithOtherStylesDoNotShare(t *testing.T) {
	first := checkout(t, "obviously", everyMarkdownFile)
	second := checkout(t, "definitely", everyMarkdownFile)

	if sharedWith(t, first, second) {
		t.Fatal("checkouts whose styles differ should not share entries")
	}
}

func TestCheckoutsWithOtherConfigurationDoNotShare(t *testing.T) {
	first := checkout(t, "obviously", everyMarkdownFile)
	second := checkout(t, "obviously", "IgnoredScopes = code\n"+everyMarkdownFile)

	if sharedWith(t, first, second) {
		t.Fatal("checkouts whose configuration differs should not share entries")
	}
}

// A section naming an absolute path outside the checkout could match one
// checkout and not another, so the cache falls back to absolute addresses.
func TestSectionOutsideTheCheckoutKeepsCheckoutsApart(t *testing.T) {
	elsewhere := filepath.ToSlash(filepath.Join(t.TempDir(), "*.md"))
	config := everyMarkdownFile + "[" + elsewhere + "]\nDemo.Wordy = error\n"

	first := checkout(t, "obviously", config)
	second := checkout(t, "obviously", config)

	if sharedWith(t, first, second) {
		t.Fatal("checkouts with a section outside them should not share entries")
	}
}

// A section naming a path inside its own checkout reads the same in every
// checkout once the checkout's path is taken out of it.
func TestSectionInsideTheCheckoutIsShared(t *testing.T) {
	config := everyMarkdownFile + "[{root}/docs/*.md]\nDemo.Wordy = error\n"

	first := checkout(t, "obviously", config)
	second := checkout(t, "obviously", config)

	if !sharedWith(t, first, second) {
		t.Fatal("checkouts whose sections name their own paths should share entries")
	}
}

// A section's pattern is matched against the path as the run spelled it, so
// `[docs/*.md]` applies to `docs/doc.md` and not to the same file spelled
// absolute. The two spellings are two questions.
func TestSpellingsOfOnePathAreTwoQuestions(t *testing.T) {
	root := checkout(t, "obviously", everyMarkdownFile+"[docs/*.md]\nDemo.Wordy = error\n")
	dir := t.TempDir()

	if got := lintWith(t, configOf(root), dir, docOf(root)); !reflect.DeepEqual(got, []string{"warning"}) {
		t.Fatalf("the absolute spelling should match the name pattern alone, got %v", got)
	}

	t.Chdir(root)
	if got := lintWith(t, configOf(root), dir, filepath.Join("docs", "doc.md")); !reflect.DeepEqual(got, []string{"error"}) {
		t.Fatalf("the relative spelling should match the directory pattern as well, got %v", got)
	}
}

// When every section looks at a file's name alone, where the file sits and
// how the run spelled it cannot change its answer.
func TestNamePatternsShareAcrossWorkingDirectories(t *testing.T) {
	first := checkout(t, "obviously", everyMarkdownFile)
	second := checkout(t, "obviously", everyMarkdownFile)
	dir := t.TempDir()

	t.Chdir(filepath.Join(first, "docs"))
	lintWith(t, configOf(first), dir, "doc.md")

	if !storedFor(t, configOf(second), dir, docOf(second)) {
		t.Fatal("a file linted from inside its directory should be shared with another checkout")
	}
}

// A `[formats]` key is matched against the path like a section, so one that
// names a directory makes where a file sits part of its answer.
func TestFormatPatternsCountAsSections(t *testing.T) {
	config := "[formats]\ndocs/*.mdx = md\n" + everyMarkdownFile
	first := checkout(t, "obviously", config)
	second := checkout(t, "obviously", config)
	dir := t.TempDir()

	t.Chdir(filepath.Join(first, "docs"))
	lintWith(t, configOf(first), dir, "doc.md")

	if storedFor(t, configOf(second), dir, docOf(second)) {
		t.Fatal("a format pattern naming a directory should keep spellings apart")
	}
}

// A configuration outside every checkout, a home directory's say, names the
// same paths in each of them.
func TestConfigurationOutsideTheCheckoutsIsShared(t *testing.T) {
	home := t.TempDir()
	writeTree(t, home, map[string]string{
		".vale.ini":             "StylesPath = " + filepath.ToSlash(filepath.Join(home, "styles")) + "\n" + everyMarkdownFile,
		"styles/Demo/Wordy.yml": wordyRule("obviously"),
	})

	var docs []string
	for range 2 {
		root := t.TempDir()
		writeTree(t, root, map[string]string{".git/HEAD": gitHead, "docs/doc.md": wrongDocument})
		docs = append(docs, docOf(root))
	}

	dir := t.TempDir()
	lintWith(t, configOf(home), dir, docs[0])

	if !storedFor(t, configOf(home), dir, docs[1]) {
		t.Fatal("checkouts linted with one configuration outside them should share entries")
	}
}

// Worktrees inside a main checkout, linted with its configuration, are each a
// checkout of their own.
func TestWorktreesInsideAMainCheckoutShare(t *testing.T) {
	main := checkout(t, "obviously", everyMarkdownFile+"[docs/*.md]\nDemo.Wordy = error\n")

	var docs []string
	for _, name := range []string{"one", "two"} {
		root := filepath.Join(main, ".worktrees", name)
		writeTree(t, root, map[string]string{
			".git":        "gitdir: " + filepath.Join(main, ".git", "worktrees", name) + "\n",
			"docs/doc.md": wrongDocument,
		})
		docs = append(docs, docOf(root))
	}

	dir := t.TempDir()
	lintWith(t, configOf(main), dir, docs[0])

	if !storedFor(t, configOf(main), dir, docs[1]) {
		t.Fatal("worktrees inside a main checkout should share entries")
	}
}

// A section's condition reads the path relative to the configuration's root,
// which is the same in two checkouts carrying their own configuration and not
// for a configuration outside them.
func TestConditionsReadThePathFromTheConfiguration(t *testing.T) {
	const conditional = everyMarkdownFile + "[*.md if .Dir == \"docs\"]\nDemo.Wordy = error\n"

	t.Run("inside the checkouts", func(t *testing.T) {
		if !sharedWith(t, checkout(t, "obviously", conditional), checkout(t, "obviously", conditional)) {
			t.Fatal("checkouts carrying the same conditional configuration should share entries")
		}
	})

	t.Run("outside the checkouts", func(t *testing.T) {
		home := t.TempDir()
		writeTree(t, home, map[string]string{
			".vale.ini":             "StylesPath = " + filepath.ToSlash(filepath.Join(home, "styles")) + "\n" + conditional,
			"styles/Demo/Wordy.yml": wordyRule("obviously"),
		})

		var docs []string
		for range 2 {
			root := t.TempDir()
			writeTree(t, root, map[string]string{".git/HEAD": gitHead, "docs/doc.md": wrongDocument})
			docs = append(docs, docOf(root))
		}

		dir := t.TempDir()
		lintWith(t, configOf(home), dir, docs[0])

		if storedFor(t, configOf(home), dir, docs[1]) {
			t.Fatal("a condition reading an absolute path should keep checkouts apart")
		}
	})
}

// A pattern called name-only must give the same verdict on a file wherever it
// sits and however it is spelled.
func TestNameOnlyPatternsIgnoreWhereAFileSits(t *testing.T) {
	cases := map[string]bool{
		"*":                              true,
		"*.md":                           true,
		"*.{md,mdx}":                     true,
		"**/*.md":                        true,
		"!*.md":                          true,
		"*.md if .Meta.draft":            true,
		"*.md if .Path == \"docs/a.md\"": true,
		"docs/*.md":                      false,
		"**.md":                          false,
		"*/docs/*.md":                    false,
		"*docs*.md":                      false,
		"/abs/*.md":                      false,
		"README.md":                      false,
		"[ab]*.md":                       false,
	}

	prefixes := []string{"", "docs/", "a/b/c/", "/r/", "/r/docs/deep/", "C:/r/"}
	names := []string{"doc.md", "doc.mdx", "doc.txt", "README", "docs.md"}

	for label, want := range cases {
		t.Run(label, func(t *testing.T) {
			if got := nameOnly(label); got != want {
				t.Fatalf("the pattern should be classified the other way")
			}
			if !want {
				return
			}

			pattern, _ := core.SplitSection(label)
			compiled, err := glob.Compile(pattern)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range names {
				verdict := compiled.Match(name)
				for _, prefix := range prefixes {
					if compiled.Match(prefix+name) != verdict {
						t.Errorf("the verdict on %s should not change under %q", name, prefix)
					}
				}
			}
		})
	}
}

// An anchored pattern cannot match a file of the checkout by anything above
// the checkout.
func TestAnchoredPatterns(t *testing.T) {
	cases := []struct {
		pattern string
		want    bool
	}{
		{"*.md", true},
		{"docs/*.md", true},
		{"/r/docs/*.md", true},
		{"!/r/docs/*.md", true},
		{"/r2/*.md", false},
		{"/elsewhere/*.md", false},
		{"~/docs/*.md", false},
		{"C:/r/*.md", false},
		{"d*.md", false},
		{"{a,b}/*.md", false},
		{"*/docs/*.md", false},
	}

	for _, c := range cases {
		t.Run(c.pattern, func(t *testing.T) {
			if got := anchoredTo(c.pattern, "/r"); got != c.want {
				t.Fatalf("the pattern should be classified the other way")
			}
		})
	}
}
