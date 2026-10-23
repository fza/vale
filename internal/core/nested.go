package core

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// NestedConfigName is the one name a configuration file carries in a directory
// other than the one a run's root configuration was found in.
//
// The root search still accepts every name in `configNames`. A nested lookup
// happens in each directory a run touches, so each accepted name would cost
// another stat per directory, and the rest of those names are spellings the
// project standardized away from.
const NestedConfigName = ".vale.ini"

// nestedConfig is one configuration file read from a directory beneath the
// root, with the path prefix its sections are anchored to.
type nestedConfig struct {
	path   string
	anchor string
}

// discoverNested returns the configuration files that apply to inputs,
// shallowest first, so that a deeper one's sections are registered later and
// therefore win.
//
// A file argument contributes the configuration of every directory between the
// root and itself. A directory argument is walked for its directories alone.
// Anchors are taken from the argument's own spelling, because that is what a
// file is matched against.
func discoverNested(root string, inputs []string) []nestedConfig {
	seen := map[string]bool{}
	var found []nestedConfig

	consider := func(dir string) {
		anchor := filepath.ToSlash(filepath.Clean(dir))
		if anchor == "." || seen[anchor] {
			return
		}
		seen[anchor] = true

		path := filepath.Join(dir, NestedConfigName)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			if abs, absErr := filepath.Abs(path); absErr != nil || abs != root {
				found = append(found, nestedConfig{path: path, anchor: anchor})
			}
		}
	}

	for _, input := range inputs {
		if IsDirPath(input) {
			walkDirs(input, consider)
			continue
		}
		for dir := filepath.Dir(input); ; dir = filepath.Dir(dir) {
			consider(dir)
			if parent := filepath.Dir(dir); parent == dir {
				break
			}
		}
	}

	sort.SliceStable(found, func(i, j int) bool {
		return strings.Count(found[i].anchor, "/") < strings.Count(found[j].anchor, "/")
	})

	return found
}

// walkDirs calls fn for root and every directory beneath it, skipping the ones
// a lint run skips.
func walkDirs(root string, fn func(string)) {
	//nolint:errcheck // an unreadable directory contributes no configuration
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // same reasoning
		}
		if !d.IsDir() {
			return nil
		}
		if ShouldIgnoreDirectory(path) {
			return filepath.SkipDir
		}
		fn(path)
		return nil
	})
}

// IsDirPath reports whether path names a directory that exists.
func IsDirPath(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// anchorLabel is a nested section's name as a pattern matching the files
// beneath the directory that declared it.
//
// `**` stands for no directory as readily as for several, so a section reaches
// the declaring directory's own files and everything below it.
func anchorLabel(anchor, sec string) string {
	if anchor == "" || anchor == "." {
		return sec
	}
	if negated := strings.HasPrefix(sec, "!"); negated {
		return "!" + anchor + "/**/" + strings.TrimPrefix(sec, "!")
	}
	return anchor + "/**/" + sec
}

// loadNested reads each nested configuration into cfg.
//
// A nested file carries glob sections and nothing else: every other part of an
// INI file reaches past the directory it describes, and which directory won
// would follow the order the paths were discovered in.
func loadNested(cfg *Config, nested []nestedConfig, dry bool) error {
	for _, n := range nested {
		uCfg, err := shadowLoad(n.path)
		if err != nil {
			return err
		}

		for _, reserved := range reservedSections {
			if reserved == "DEFAULT" {
				continue
			}
			if keys := uCfg.Section(reserved).KeyStrings(); len(keys) > 0 {
				return nestedKeyError(keys[0], reserved, n.path)
			}
		}
		if keys := uCfg.Section("").KeyStrings(); len(keys) > 0 {
			return nestedKeyError(keys[0], "", n.path)
		}

		for _, sec := range uCfg.SectionStrings() {
			if StringInSlice(sec, reservedSections) {
				continue
			}
			if err = processSection(uCfg, sec, anchorLabel(n.anchor, sec), cfg, dry); err != nil {
				return err
			}
		}

		cfg.AddConfigFile(n.path)
		cfg.NestedFiles = append(cfg.NestedFiles, n.path)
	}

	return nil
}

func nestedKeyError(key, section, path string) error {
	where := "outside a section"
	if section != "" {
		where = fmt.Sprintf("in [%s]", section)
	}

	return NewE201FromTarget(fmt.Sprintf(
		"'%s' %s applies to a whole run, which a configuration in a directory "+
			"cannot decide; set it in the root configuration. A nested file "+
			"carries glob sections alone -- use [**] for every file here.",
		key, where), key, path)
}
