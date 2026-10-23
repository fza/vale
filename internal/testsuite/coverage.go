package testsuite

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/vale-cli/vale/v3/internal/core"
)

// Rules names every rule under the given files and directories, the way a
// project run would name it.
//
// A rule that matches nothing loads, runs, and reports success, so a case
// that expects nothing from it proves nothing. The rules are listed so that
// Uncovered can say which never produced an alert in any case.
func (r *Runner) Rules(paths []string) ([]string, error) {
	if len(paths) == 0 {
		paths = []string{"."}
	}

	var names []string
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}

		if !info.IsDir() {
			if ok, rErr := isRule(path); rErr != nil {
				return nil, rErr
			} else if ok {
				names = append(names, isolatedName(path, r.searchPaths()))
			}
			continue
		}

		err = filepath.WalkDir(path, func(fp string, d fs.DirEntry, wErr error) error {
			if wErr != nil {
				return wErr
			}
			name := d.Name()
			if d.IsDir() {
				// A dot or underscore directory is inert to the style loader
				// too: what is parked there is not a rule that could fire.
				if fp != path && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
					return filepath.SkipDir
				}
				return nil
			}
			if ok, rErr := isRule(fp); rErr != nil {
				return rErr
			} else if ok {
				names = append(names, isolatedName(fp, r.searchPaths()))
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	sort.Strings(names)
	return names, nil
}

// isRule reports whether the file is a rule: YAML with an `extends`, and not
// a test file.
func isRule(path string) (bool, error) {
	name := filepath.Base(path)
	if core.IsTestFile(name) || (!strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml")) {
		return false, nil
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}

	var rule struct {
		Extends string `yaml:"extends"`
	}
	if err = yaml.Unmarshal(b, &rule); err != nil {
		return false, nil //nolint:nilerr // foreign YAML is not a rule, as Load treats it
	}
	return rule.Extends != "", nil
}

// Uncovered returns the rules that produced no alert in any result.
func Uncovered(rules []string, results []Result) []string {
	fired := map[string]bool{}
	for _, r := range results {
		for _, line := range strings.Split(r.Got, "\n") {
			// `line:col:check:message`; the check is the third field.
			if parts := strings.SplitN(line, ":", 4); len(parts) == 4 {
				fired[parts[2]] = true
			}
		}
	}

	var missing []string
	for _, rule := range rules {
		if !fired[rule] {
			missing = append(missing, rule)
		}
	}
	return missing
}
