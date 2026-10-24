//go:build js && wasm

package main

import (
	"encoding/json"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"syscall/js"

	"github.com/vale-cli/vale/v3/internal/check"
	"github.com/vale-cli/vale/v3/internal/core"
)

// A resolved configuration: what each section's rules come to once its
// styles and toggles combine, for a host showing a configuration rather than
// running it.
type resolvedConfig struct {
	StylesPath string            `json:"stylesPath"`
	Global     resolvedGlobal    `json:"global"`
	Sections   []resolvedSection `json:"sections"`
	Error      string            `json:"error,omitempty"`
}

type resolvedGlobal struct {
	MinAlertLevel string   `json:"minAlertLevel,omitempty"`
	Vocab         []string `json:"vocab"`
	BasedOnStyles []string `json:"basedOnStyles"`
}

type resolvedSection struct {
	Glob       string         `json:"glob"`
	Styles     []string       `json:"styles"`
	Rules      []resolvedRule `json:"rules"`
	Active     int            `json:"active"`
	Overridden int            `json:"overridden"`
	Disabled   int            `json:"disabled"`
}

type resolvedRule struct {
	Ref   string `json:"ref"`
	State string `json:"state"` // active | disabled | override
	Level string `json:"level,omitempty"`
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func union(a, b []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range append(append([]string{}, a...), b...) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// ruleState resolves one Style.Rule against the toggles: a level override
// wins, then a disable in the section or globally, else it's active.
func ruleState(cfg *core.Config, glob, ref string) (string, string) {
	if lvl, ok := cfg.SLevels[glob][ref]; ok {
		return "override", lvl
	}
	if lvl, ok := cfg.RuleToLevel[ref]; ok {
		return "override", lvl
	}
	if v, ok := cfg.SChecks[glob][ref]; ok && !v {
		return "disabled", ""
	}
	if v, ok := cfg.GChecks[ref]; ok && !v {
		return "disabled", ""
	}
	return "active", ""
}

// ValeResolveConfig(configINI) returns the configuration resolved: its
// StylesPath, the global settings, and for each section the rules its styles
// bring and the state each is in.
func ValeResolveConfig(_ js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		return errResult("ValeResolveConfig(configINI) requires 1 argument")
	}
	res := resolvedConfig{Sections: []resolvedSection{}}

	cfg, err := core.LoadConfigFromString(args[0].String())
	if err != nil {
		res.Error = err.Error()
		return toResult(res)
	}
	res.StylesPath = cfg.StylesPath()
	res.Global = resolvedGlobal{
		Vocab:         orEmpty(cfg.Vocab),
		BasedOnStyles: orEmpty(cfg.GBaseStyles),
	}
	if cfg.MinAlertLevel >= 0 && cfg.MinAlertLevel < len(core.AlertLevels) {
		res.Global.MinAlertLevel = core.AlertLevels[cfg.MinAlertLevel]
	}

	// A rule switched off is never loaded, so the manager alone would miss
	// it: the files on the StylesPath name every rule a style has, and the
	// manager names the built-in style's.
	mgr, err := check.NewManager(cfg)
	if err != nil {
		res.Error = err.Error()
		return toResult(res)
	}
	known := map[string]bool{}
	for name := range mgr.Rules() {
		known[name] = true
	}
	for _, p := range cfg.SearchPaths() {
		for _, name := range styleRules(cfg, p) {
			known[name] = true
		}
	}
	loaded := make([]string, 0, len(known))
	for name := range known {
		loaded = append(loaded, name)
	}
	sort.Strings(loaded)

	// The global section registers no key of its own; it is a section to
	// the reader when it bases on styles.
	globs := cfg.RuleKeys
	if len(cfg.GBaseStyles) > 0 {
		globs = append([]string{"*"}, globs...)
	}

	seen := map[string]bool{}
	for _, glob := range globs {
		if seen[glob] {
			continue
		}
		seen[glob] = true
		styles := union(cfg.GBaseStyles, cfg.SBaseStyles[glob])
		sec := resolvedSection{Glob: glob, Styles: orEmpty(styles), Rules: []resolvedRule{}}
		for _, style := range styles {
			for _, ref := range loaded {
				if !strings.HasPrefix(ref, style+".") {
					continue
				}
				state, level := ruleState(cfg, glob, ref)
				sec.Rules = append(sec.Rules, resolvedRule{Ref: ref, State: state, Level: level})
				switch state {
				case "active":
					sec.Active++
				case "override":
					sec.Overridden++
				case "disabled":
					sec.Disabled++
				}
			}
		}
		res.Sections = append(res.Sections, sec)
	}
	return toResult(res)
}

// styleRules names every rule under the styles in root, by file.
func styleRules(cfg *core.Config, root string) []string {
	var names []string
	_ = cfg.FS.Walk(root, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		base := info.Name()
		if info.IsDir() {
			// `config/` holds assets, not rules; a `.`- or `_`-prefixed
			// directory is not a style.
			if path != root && (base == core.ConfigDir || strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(base, ".yml") && !strings.HasSuffix(base, ".yaml") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) < 2 {
			return nil
		}
		if name, nerr := core.CheckName(filepath.Join(root, parts[0]), path); nerr == nil {
			names = append(names, name)
		}
		return nil
	})
	return names
}

func toResult(v interface{}) interface{} {
	b, err := json.Marshal(v)
	if err != nil {
		return errResult(err.Error())
	}
	return map[string]interface{}{"result": string(b)}
}
