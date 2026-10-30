package lint

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/vale-cli/vale/v3/internal/cache"
	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/system"
)

// cachedExts are the markup extensions whose alerts depend on nothing but the
// file's own bytes.
//
// Markdown and HTML are parsed in memory. The rest are not cached, and the
// reason is the same for all of them: their converter reads the filesystem
// itself. DITA resolves maps and content references, XML applies an external
// stylesheet, and AsciiDoc, reStructuredText and Typst are converted by a
// separate program whose own include handling decides what it reads. A cache
// entry is only sound when the key names everything the answer depends on, and
// for those formats the key would have to name files Vale never sees.
var cachedExts = map[string]bool{
	".md":   true,
	".html": true,
}

// cachedAlert is an alert as the output formats consume it.
//
// Alert carries several fields that only the linting run itself uses -- what
// text to skip before a match, how many times a rule may report, whether an
// alert was suppressed. None of them survives into the result, so none of them
// is stored.
type cachedAlert struct {
	Action      core.Action `json:"action"`
	Span        []int       `json:"span"`
	Check       string      `json:"check"`
	Description string      `json:"description"`
	Link        string      `json:"link"`
	Message     string      `json:"message"`
	Severity    string      `json:"severity"`
	Match       string      `json:"match"`
	Line        int         `json:"line"`
}

// EnableCache makes the linter reuse the alerts of files that have not changed
// since it last saw them.
//
// version stands for the binary: a build that reports differently must not
// read what an earlier one wrote.
func (l *Linter) EnableCache(dir, version string) error {
	salt, err := cacheSalt(l.Manager.Config, version)
	if err != nil {
		return err
	}

	scope, err := newCacheScope(l.Manager.Config)
	if err != nil {
		return err
	}

	c, err := cache.Open(dir, salt)
	if err != nil {
		return err
	}
	l.cache = c
	l.cacheScope = scope

	return nil
}

// rootRelative rewrites every occurrence of root in the JSON body as a
// placeholder, which leaves the paths beneath it relative to it.
//
// The configuration is not walked field by field: whichever field records a
// path under the root is rewritten, including fields added after this was
// written. A path outside the root is left as it is.
func rootRelative(body []byte, root string) []byte {
	quoted, err := json.Marshal(root)
	if err != nil {
		return body
	}
	escaped := quoted[1 : len(quoted)-1]

	separator, err := json.Marshal(string(filepath.Separator))
	if err != nil {
		return body
	}

	body = bytes.ReplaceAll(body, append(append([]byte{}, escaped...), separator[1:len(separator)-1]...), []byte("<root>/"))

	// A section's pattern names the root with forward slashes whatever the
	// platform's separator.
	slashed, err := json.Marshal(filepath.ToSlash(root) + "/")
	if err != nil {
		return body
	}
	body = bytes.ReplaceAll(body, slashed[1:len(slashed)-1], []byte("<root>/"))

	return bytes.ReplaceAll(body, append(append([]byte{'"'}, escaped...), '"'), []byte(`"<root>"`))
}

// rootRelativeText rewrites every path under root in a configuration file as
// rootRelative does in the resolved configuration, in either separator's
// spelling.
func rootRelativeText(body []byte, root string) []byte {
	for _, prefix := range []string{root + string(filepath.Separator), filepath.ToSlash(root) + "/"} {
		body = bytes.ReplaceAll(body, []byte(prefix), []byte("<root>/"))
	}

	return body
}

// CloseCache does the cache's upkeep, within its budget, and releases it. It is
// meant for after a run has reported, so that no caller waits on it.
//
// startClean starts a process deleting the cache's earlier layout, which takes
// longer than a run may wait; see cache.Cache.Sweep.
func (l *Linter) CloseCache(startClean func() error) {
	if l.cache == nil {
		return
	}

	l.cache.Sweep(cache.SweepBudget, startClean)
	l.cache.Close()
}

func (l *Linter) flushCache() {
	if l.cache != nil {
		l.cache.Flush()
	}
}

// cacheSalt hashes everything shared by every file of a run, save the resolved
// configuration, which names paths and is hashed per checkout; see
// cacheScope.config.
//
// The styles path is hashed as a tree, which covers the rules, the
// vocabularies, the ignore lists and the dictionaries without naming any of
// them.
func cacheSalt(cfg *core.Config, version string) ([]byte, error) {
	h := sha256.New()

	fmt.Fprintf(h, "vale %s\n", version)

	// A development build reports no version, so the version alone cannot tell
	// two of them apart and one would read what the other wrote. The running
	// binary's own size and modification time separate them, and leave a
	// released binary hashing the same on every run.
	if exe, exeErr := os.Executable(); exeErr == nil {
		if info, statErr := os.Stat(exe); statErr == nil {
			fmt.Fprintf(h, "binary %d %d\n", info.Size(), info.ModTime().UnixNano())
		}
	}

	// Flags is not part of the marshalled configuration. These two decide
	// which rules run and how a file is read. Every other flag either decides
	// how alerts are printed or feeds a field the marshalled configuration
	// already carries, which TestEveryFlagIsClassified holds to.
	fmt.Fprintf(h, "simple %t\n", cfg.Flags.Simple)
	fmt.Fprintf(h, "filter %s\n", cfg.Flags.Filter)
	if system.FileExists(cfg.Flags.Filter) {
		body, readErr := os.ReadFile(cfg.Flags.Filter)
		if readErr != nil {
			return nil, readErr
		}
		fmt.Fprintf(h, "filter-body %x\n", sha256.Sum256(body))
	}

	for _, path := range cfg.Paths {
		treeHash, treeErr := hashTree(path)
		if treeErr != nil {
			return nil, treeErr
		}
		fmt.Fprintf(h, "styles %x\n", treeHash)
	}

	return h.Sum(nil), nil
}

// hashTree hashes every file under root, by path relative to root and by
// content, so that the same styles hash the same wherever they are checked out.
func hashTree(root string) ([32]byte, error) {
	type entry struct {
		rel  string
		sum  [32]byte
		size int64
	}

	var entries []entry

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			//nolint:nilerr // an unreadable path cannot contribute a rule either
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		body, readErr := os.ReadFile(path)
		if readErr != nil {
			//nolint:nilerr // same reasoning as above
			return nil
		}

		entries = append(entries, entry{
			rel:  filepath.ToSlash(rel),
			sum:  sha256.Sum256(body),
			size: int64(len(body)),
		})

		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return [32]byte{}, nil
		}
		return [32]byte{}, err
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].rel < entries[j].rel
	})

	h := sha256.New()
	for _, e := range entries {
		fmt.Fprintf(h, "%s %d %x\n", e.rel, e.size, e.sum)
	}

	return [32]byte(h.Sum(nil)), nil
}

// cacheable reports whether src's alerts follow from its own bytes alone.
func (l *Linter) cacheable(src string) bool {
	if l.cache == nil {
		return false
	}

	ext, format := core.FormatFromExt(src, l.Manager.Config.Formats)
	if format == "code" {
		return true
	}

	return format == "markup" && cachedExts[ext]
}

// cacheKey addresses src's alerts by the configuration as its checkout sees
// it, by its path, and by its content.
//
// The path is part of the key because the configuration assigns rules by path:
// the same bytes under two names are two different questions. Which form of the
// path stands in the key is cacheScope.address's call. A section's condition
// reads the path relative to the configuration's root, so when there is one,
// that form is part of the key as well.
func (l *Linter) cacheKey(src string, content []byte) cache.Key {
	root := l.cacheScope.contentRoot(src)

	var conditionPath string
	if l.cacheScope.conditions {
		conditionPath = core.NewFileFields(src, "", false, l.Manager.Config).Path
	}

	return l.cache.Key(
		l.cacheScope.config(root),
		[]byte(l.cacheScope.address(src, root)),
		[]byte(conditionPath),
		content,
	)
}

// cachedResult returns the alerts stored for src, if any.
func (l *Linter) cachedResult(src string, key cache.Key) (*core.File, bool) {
	data, found := l.cache.Get(key)
	if !found {
		return nil, false
	}

	var stored []cachedAlert
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, false
	}

	alerts := make([]core.Alert, 0, len(stored))
	for _, a := range stored {
		alerts = append(alerts, core.Alert{
			Action:      a.Action,
			Span:        a.Span,
			Check:       a.Check,
			Description: a.Description,
			Link:        a.Link,
			Message:     a.Message,
			Severity:    a.Severity,
			Match:       a.Match,
			Line:        a.Line,
		})
	}

	return &core.File{Path: src, Alerts: alerts}, true
}

// storeResult remembers a file's alerts.
func (l *Linter) storeResult(key cache.Key, file *core.File) {
	stored := make([]cachedAlert, 0, len(file.Alerts))
	for _, a := range file.Alerts {
		stored = append(stored, cachedAlert{
			Action:      a.Action,
			Span:        a.Span,
			Check:       a.Check,
			Description: a.Description,
			Link:        a.Link,
			Message:     a.Message,
			Severity:    a.Severity,
			Match:       a.Match,
			Line:        a.Line,
		})
	}

	data, err := json.Marshal(stored)
	if err != nil {
		return
	}

	l.cache.Put(key, data)
}
