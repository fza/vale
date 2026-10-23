package core

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// reservedScopes are the names Vale gives its own scopes and terms. A named
// scope may not take one of them, or a rule could no longer say which it
// meant.
var reservedScopes = map[string]bool{
	"alt": true, "blockquote": true, "caption": true, "cell": true,
	"class": true, "code": true, "comment": true, "doc": true,
	"emphasis": true, "figure": true, "header": true, "heading": true,
	"in": true, "link": true, "list": true, "meta": true, "paragraph": true,
	"raw": true, "sentence": true, "strong": true, "summary": true,
	"table": true, "text": true, "quote": true,
}

var scopeName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

// LoadScopes reads every named scope on the search paths into c.Scopes.
//
// A named scope is an entry in a YAML mapping under `config/scopes/`, from a
// name to a scope expression, and a rule uses the name where it would write
// the expression. Every file found is loaded: a package ships its names
// beside the rules that use them, and nothing in a configuration has to
// mention them.
func (c *Config) LoadScopes() error {
	if c.scopesLoaded {
		return nil
	}
	c.scopesLoaded = true

	for _, root := range c.SearchPaths() {
		dir := filepath.Join(root, ScopeDir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || (!strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml")) {
				continue
			}
			if err = c.loadScopeFile(filepath.Join(dir, name)); err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *Config) loadScopeFile(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return NewE201FromPosition(err.Error(), path, 1)
	}

	var defined map[string]string
	if err = yaml.Unmarshal(b, &defined); err != nil {
		return NewE201FromPosition(fmt.Sprintf("a mapping of names to scopes: %s", err), path, 1)
	}

	for name, scope := range defined {
		switch {
		case !scopeName.MatchString(name):
			return NewE201FromTarget(
				fmt.Sprintf("'%s' is not a scope name: letters, digits, '_' and '-' only", name), name, path)
		case reservedScopes[name]:
			return NewE201FromTarget(
				fmt.Sprintf("'%s' is a scope Vale defines", name), name, path)
		case strings.TrimSpace(scope) == "":
			return NewE201FromTarget("a named scope needs a scope", name, path)
		}

		if seen, ok := c.Scopes[name]; ok && seen != scope {
			return NewE201FromTarget(
				fmt.Sprintf("'%s' is already defined as '%s'", name, seen), name, path)
		}
		c.Scopes[name] = strings.TrimSpace(scope)
	}

	return nil
}
