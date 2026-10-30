package lint

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/system"
)

// globMeta are the characters that make a pattern more than a literal string.
const globMeta = `*?[{\`

// cacheScope decides how the cache addresses a file, so that two checkouts of
// one tree -- worktrees, say -- share their entries wherever their answers
// cannot differ.
//
// A file's checkout is the nearest directory above it holding a `.git`, found
// by reading the filesystem and never by running git. Within it the resolved
// configuration names its paths relative to the checkout, and a file is
// addressed relative to the checkout when no section's pattern can tell the
// checkouts apart.
//
// The configuration's own root -- where `.vale.ini` was found -- is not the
// checkout: a nested directory, or a home directory, can hold the
// configuration as well.
type cacheScope struct {
	// resolved is the configuration as Vale resolved it, which folds in the
	// ini files, the command line and the defaults in one pass.
	resolved []byte

	// sources are the bodies of the files the configuration was read from.
	// Resolution drops what it has no field for -- a section that names no
	// styles, checks or levels leaves no trace of itself -- so the files
	// answer for whatever the fields do not.
	sources [][]byte

	// patterns are what a file's path is matched against: section names and
	// `[formats]` keys.
	patterns []string

	// blind is set when every pattern looks at the end of a file's name
	// alone, so where the file sits cannot change its answer.
	blind bool

	// conditions is set when a section carries a condition, which reads the
	// file's path relative to the configuration's root.
	conditions bool

	mu      sync.Mutex
	roots   map[string]string
	configs map[string]checkoutConfig
}

// checkoutConfig is the configuration as one checkout sees it.
type checkoutConfig struct {
	hash [32]byte

	// anchored is set when no pattern can match a file of the checkout by
	// anything above the checkout: each either looks at a file's name
	// alone, starts with the checkout's own path, or is a relative pattern,
	// which never matches an absolute path.
	anchored bool
}

func newCacheScope(cfg *core.Config) (*cacheScope, error) {
	resolved, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}

	scope := &cacheScope{
		resolved:   resolved,
		blind:      true,
		conditions: len(cfg.SecToCond) > 0,
		roots:      map[string]string{},
		configs:    map[string]checkoutConfig{},
	}

	paths := append([]string{}, cfg.ConfigFiles...)
	if cfg.Flags.Sources != "" {
		// `--sources` names files without recording them as configuration
		// files, so they are collected here rather than above.
		paths = append(paths, strings.Split(cfg.Flags.Sources, ",")...)
	}
	for _, path := range paths {
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		scope.sources = append(scope.sources, body)
	}

	for label := range cfg.SecToPat {
		scope.patterns = append(scope.patterns, label)
	}
	for key := range cfg.Formats {
		if strings.ContainsAny(key, globMeta+"/") {
			scope.patterns = append(scope.patterns, key)
		}
	}
	for _, pattern := range scope.patterns {
		scope.blind = scope.blind && nameOnly(pattern)
	}

	return scope, nil
}

// contentRoot returns the checkout holding src, or empty when it lies in none.
//
// Every directory visited on the way up is remembered, so a run over a tree
// asks the filesystem about each directory once.
func (s *cacheScope) contentRoot(src string) string {
	abs, err := filepath.Abs(src)
	if err != nil {
		return ""
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var visited []string
	root := ""
	for dir := filepath.Dir(abs); ; {
		if known, found := s.roots[dir]; found {
			root = known
			break
		}
		visited = append(visited, dir)

		if isCheckout(dir) {
			root = dir
			break
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	for _, dir := range visited {
		s.roots[dir] = root
	}

	return root
}

// isCheckout reports whether dir holds a git checkout: a `.git` directory with
// a HEAD in it, or the `.git` file a linked worktree or a submodule carries,
// which points at the repository with a `gitdir:` line.
func isCheckout(dir string) bool {
	dotGit := filepath.Join(dir, ".git")

	info, err := os.Stat(dotGit)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return system.FileExists(filepath.Join(dotGit, "HEAD"))
	}

	f, err := os.Open(dotGit)
	if err != nil {
		return false
	}
	defer f.Close()

	head := make([]byte, len("gitdir:"))
	read, _ := f.Read(head)

	return bytes.Equal(head[:read], []byte("gitdir:"))
}

// config returns the hash of the configuration as the checkout at root sees
// it: the resolved configuration and the files it was read from, with every
// path under the checkout written relative to it.
func (s *cacheScope) config(root string) []byte {
	seen := s.checkout(root)
	return seen.hash[:]
}

func (s *cacheScope) checkout(root string) checkoutConfig {
	s.mu.Lock()
	defer s.mu.Unlock()

	if seen, found := s.configs[root]; found {
		return seen
	}

	h := sha256.New()
	resolved := s.resolved
	if root != "" {
		resolved = rootRelative(resolved, root)
	}
	fmt.Fprintf(h, "config %x\n", sha256.Sum256(resolved))

	for _, body := range s.sources {
		if root != "" {
			body = rootRelativeText(body, root)
		}
		fmt.Fprintf(h, "ini %x\n", sha256.Sum256(body))
	}

	seen := checkoutConfig{hash: [32]byte(h.Sum(nil))}
	if root != "" {
		seen.anchored = true
		for _, pattern := range s.patterns {
			seen.anchored = seen.anchored && anchoredTo(pattern, root)
		}
	}
	s.configs[root] = seen

	return seen
}

// address is the form of src's path that stands in its key.
//
// A path is matched against the patterns as the run spelled it: relative to
// the working directory, or absolute. That spelling is the address, unless the
// patterns cannot tell one checkout from another. Then the address is the path
// relative to the checkout, so the same file in two checkouts is one question:
// from any working directory when every pattern looks at a file's name alone,
// and for an absolute spelling when the patterns are anchored.
func (s *cacheScope) address(src, root string) string {
	spelled := "spelled " + filepath.ToSlash(src)
	if root == "" {
		return spelled
	}

	rel, inside := system.RelInside(root, src)
	if !inside {
		return spelled
	}

	if s.blind || (filepath.IsAbs(src) && s.checkout(root).anchored) {
		return "checkout " + filepath.ToSlash(rel)
	}

	return spelled
}

// nameOnly reports whether pattern's verdict on a path depends on the end of a
// file's name alone: `*`, `*.md`, `*.{md,mdx}`, `**/*.md`, or the negation of
// one.
//
// A pattern without `**` is matched by a `*` that crosses directories, so a
// leading `*` followed by a literal ending looks at the last few characters
// only. A pattern with `**` is matched by a `*` that does not, so only the
// `**/` prefix reaches across directories. Past that prefix any `*` but the
// leading one rules a pattern out, which takes every other `**` with it.
func nameOnly(label string) bool {
	pattern, _ := core.SplitSection(label)
	pattern = strings.TrimPrefix(strings.TrimPrefix(pattern, "!"), "**/")

	ending, found := strings.CutPrefix(pattern, "*")
	return found && !strings.ContainsAny(ending, `*?[]\/`)
}

// anchoredTo reports whether pattern can match a file of the checkout at root
// by nothing above the checkout.
func anchoredTo(label, root string) bool {
	if nameOnly(label) {
		return true
	}

	pattern, _ := core.SplitSection(label)
	pattern = strings.TrimPrefix(pattern, "!")

	if !strings.ContainsAny(root, globMeta+"]") {
		for _, prefix := range []string{root + string(filepath.Separator), filepath.ToSlash(root) + "/"} {
			if strings.HasPrefix(pattern, prefix) {
				return true
			}
		}
	}

	return relativeLiteral(pattern)
}

// relativeLiteral reports whether pattern starts with literal characters that
// no absolute path starts with: not a separator, not `~`, and not a volume
// name such as `C:`.
func relativeLiteral(pattern string) bool {
	return len(pattern) >= 2 && !strings.ContainsAny(pattern[:2], globMeta+`/~:`)
}
