package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func scopeConfig(t *testing.T, files map[string]string) *Config {
	t.Helper()

	root := t.TempDir()
	for name, body := range files {
		path := filepath.Join(root, ScopeDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cfg, err := NewConfig(&CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddStylesPath(root)
	return cfg
}

func TestLoadScopes(t *testing.T) {
	cfg := scopeConfig(t, map[string]string{
		"Sections.yml": "methods: 'doc(section:has(> h2:contains(\"Methods\")))'\nlead: text & doc(h1 + p)\n",
		"More.yaml":    "results: doc(section:has(> h2:contains(\"Results\")))\n",
		// The same name with the same expression, from another file, is fine.
		"Again.yml": "lead: text & doc(h1 + p)\n",
		"notes.txt": "not: read\n",
	})

	if err := cfg.LoadScopes(); err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"methods": `doc(section:has(> h2:contains("Methods")))`,
		"lead":    "text & doc(h1 + p)",
		"results": `doc(section:has(> h2:contains("Results")))`,
	}
	if len(cfg.Scopes) != len(want) {
		t.Fatalf("loaded %v, want %v", cfg.Scopes, want)
	}
	for name, expr := range want {
		if cfg.Scopes[name] != expr {
			t.Errorf("%s = %q, want %q", name, cfg.Scopes[name], expr)
		}
	}
}

func TestLoadScopesNoDirectory(t *testing.T) {
	cfg, err := NewConfig(&CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddStylesPath(t.TempDir())

	if err = cfg.LoadScopes(); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Scopes) != 0 {
		t.Errorf("loaded %v from nothing", cfg.Scopes)
	}
}

func TestLoadScopesRejects(t *testing.T) {
	for _, tt := range []struct{ name, body, want string }{
		{"reserved", "heading: doc(h1)\n", "a scope Vale defines"},
		{"bad name", "'my scope': doc(h1)\n", "not a scope name"},
		{"empty", "methods: ''\n", "needs a scope"},
		{"conflict", "methods: doc(h1)\n---\n", "already defined"},
		{"not a mapping", "- methods\n", "a mapping of names to scopes"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string]string{"Bad.yml": tt.body}
			if tt.name == "conflict" {
				files = map[string]string{
					"A.yml": "methods: doc(h1)\n",
					"B.yml": "methods: doc(h2)\n",
				}
			}
			cfg := scopeConfig(t, files)

			err := cfg.LoadScopes()
			if err == nil {
				t.Fatal("accepted")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("got %q, want it to mention %q", err, tt.want)
			}
		})
	}
}
