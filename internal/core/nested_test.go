package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vale-cli/vale/v3/internal/glob"
)

func TestAnchorLabel(t *testing.T) {
	cases := []struct {
		anchor, sec, want string
	}{
		{"docs", "*.md", "docs/**/*.md"},
		{"docs/api", "BOARD.md", "docs/api/**/BOARD.md"},
		{"docs", "!*.md", "!docs/**/*.md"},
		{".", "*.md", "*.md"},
		{"", "*.md", "*.md"},
	}

	for _, c := range cases {
		if got := anchorLabel(c.anchor, c.sec); got != c.want {
			t.Errorf("anchorLabel(%q, %q) = %q, want %q", c.anchor, c.sec, got, c.want)
		}
	}
}

// An anchored section covers the declaring directory's own files and every
// file beneath it, and no sibling's.
func TestAnchoredSectionReach(t *testing.T) {
	pat, err := glob.Compile(anchorLabel("docs", "*.md"))
	if err != nil {
		t.Fatal(err)
	}

	covered := []string{"docs/a.md", "docs/api/a.md", "docs/api/v2/a.md"}
	for _, p := range covered {
		if !pat.Match(p) {
			t.Errorf("%q should be covered", p)
		}
	}
	for _, p := range []string{"other/a.md", "docsx/a.md", "a.md"} {
		if pat.Match(p) {
			t.Errorf("%q should not be covered", p)
		}
	}
}

func nestedFixture(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	for _, dir := range []string{
		"docs/api", "docs/guide", "other", "node_modules/pkg",
	} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{"docs", "docs/api", "node_modules/pkg", ""} {
		path := filepath.Join(root, dir, NestedConfigName)
		if err := os.WriteFile(path, []byte("[*.md]\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// A file argument contributes the configuration of every directory between the
// root and itself, and nothing from a directory it does not sit under.
func TestDiscoverNestedFromFile(t *testing.T) {
	root := nestedFixture(t)
	rootINI, err := filepath.Abs(filepath.Join(root, NestedConfigName))
	if err != nil {
		t.Fatal(err)
	}

	found := discoverNested(rootINI, []string{filepath.Join(root, "docs/api/a.md")})

	var anchors []string
	for _, n := range found {
		anchors = append(anchors, filepath.Base(filepath.Dir(n.path)))
	}
	if len(anchors) != 2 || anchors[0] != "docs" || anchors[1] != "api" {
		t.Fatalf("got %v, want [docs api] shallowest first", anchors)
	}
}

// A directory argument is walked, and the directories a lint run skips are
// skipped here too.
func TestDiscoverNestedFromDirectory(t *testing.T) {
	root := nestedFixture(t)
	rootINI, err := filepath.Abs(filepath.Join(root, NestedConfigName))
	if err != nil {
		t.Fatal(err)
	}

	found := discoverNested(rootINI, []string{root})

	for _, n := range found {
		if abs, _ := filepath.Abs(n.path); abs == rootINI {
			t.Error("the root configuration must not be read again as a nested one")
		}
		if filepath.Base(filepath.Dir(filepath.Dir(n.path))) == "node_modules" {
			t.Error("node_modules must be skipped")
		}
	}
	if len(found) != 2 {
		t.Fatalf("got %d nested files, want 2", len(found))
	}
	if depth := len(found[0].anchor); depth > len(found[1].anchor) {
		t.Error("nested files must come shallowest first")
	}
}

// One directory reached by several arguments is read once.
func TestDiscoverNestedReadsADirectoryOnce(t *testing.T) {
	root := nestedFixture(t)
	rootINI, _ := filepath.Abs(filepath.Join(root, NestedConfigName))

	inputs := []string{
		filepath.Join(root, "docs/api/a.md"),
		filepath.Join(root, "docs/api/b.md"),
		filepath.Join(root, "docs/guide/c.md"),
	}
	if found := discoverNested(rootINI, inputs); len(found) != 2 {
		t.Fatalf("got %d nested files, want 2", len(found))
	}
}
