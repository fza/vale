package system

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemFS is an in-memory FS implementation. Paths are normalized to
// forward-slash form and treated as absolute under a single virtual
// root, so a Vale config with `StylesPath = /styles` and a MemFS
// populated under `/styles/<package>/<rule>.yml` behaves identically
// to a host-OS install.
type MemFS struct {
	mu    sync.RWMutex
	files map[string][]byte
}

// NewMemFS returns an empty in-memory filesystem.
func NewMemFS() *MemFS {
	return &MemFS{files: map[string][]byte{}}
}

func memClean(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if name == "" {
		return "/"
	}
	if !strings.HasPrefix(name, "/") {
		name = "/" + name
	}
	return path.Clean(name)
}

// AddFile writes the supplied bytes at the given virtual path.
func (m *MemFS) AddFile(name string, data []byte) {
	cleaned := memClean(name)
	m.mu.Lock()
	defer m.mu.Unlock()
	// Copy so the caller can't mutate stored bytes from underneath us.
	cp := make([]byte, len(data))
	copy(cp, data)
	m.files[cleaned] = cp
}

// Remove deletes the file at the given virtual path. It is a no-op if
// the path does not exist.
func (m *MemFS) Remove(name string) {
	cleaned := memClean(name)
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, cleaned)
}

// Reset empties the filesystem.
func (m *MemFS) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files = map[string][]byte{}
}

// List returns every file path currently stored, sorted lexicographically.
func (m *MemFS) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.files))
	for p := range m.files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// AddZip extracts every entry in the supplied ZIP archive under dest.
// Directory entries are skipped; their existence is inferred from the
// files inside them.
func (m *MemFS) AddZip(dest string, data []byte) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	dest = memClean(dest)
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		target := memClean(path.Join(dest, f.Name)) //nolint:gosec // G305: zip-slip prevented by the dest-prefix guard on the next line
		// Guard against zip-slip: skip any entry whose name escapes dest.
		if target != dest && !strings.HasPrefix(target, dest+"/") {
			continue
		}
		rc, ferr := f.Open()
		if ferr != nil {
			return ferr
		}
		body, ferr := io.ReadAll(rc)
		rc.Close()
		if ferr != nil {
			return ferr
		}
		m.AddFile(target, body)
	}
	return nil
}

func (m *MemFS) get(name string) ([]byte, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	data, ok := m.files[memClean(name)]
	return data, ok
}

// ReadFile implements FS.
func (m *MemFS) ReadFile(name string) ([]byte, error) {
	data, ok := m.get(name)
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	return cp, nil
}

// Open implements FS.
func (m *MemFS) Open(name string) (io.ReadCloser, error) {
	data, err := m.ReadFile(name)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// Readlink implements FS. The in-memory store has no symlinks.
func (m *MemFS) Readlink(name string) (string, error) {
	return "", &fs.PathError{Op: "readlink", Path: name, Err: errors.New("not a symlink")}
}

// memFileInfo is the FileInfo implementation returned by Stat / Walk for
// MemFS entries.
type memFileInfo struct {
	name  string
	size  int64
	isDir bool
}

func (i memFileInfo) Name() string       { return i.name }
func (i memFileInfo) Size() int64        { return i.size }
func (i memFileInfo) Mode() fs.FileMode  { return iif(i.isDir, fs.ModeDir|0o555, 0o444) }
func (i memFileInfo) ModTime() time.Time { return time.Time{} }
func (i memFileInfo) IsDir() bool        { return i.isDir }
func (i memFileInfo) Sys() any           { return nil }

func iif(cond bool, a, b fs.FileMode) fs.FileMode {
	if cond {
		return a
	}
	return b
}

func (m *MemFS) directoryExists(name string) bool {
	prefix := memClean(name)
	if prefix == "/" {
		// Root always exists.
		return true
	}
	prefix += "/"
	m.mu.RLock()
	defer m.mu.RUnlock()
	for p := range m.files {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// Stat implements FS.
func (m *MemFS) Stat(name string) (fs.FileInfo, error) {
	cleaned := memClean(name)
	if data, ok := m.get(cleaned); ok {
		return memFileInfo{name: path.Base(cleaned), size: int64(len(data))}, nil
	}
	if m.directoryExists(cleaned) {
		return memFileInfo{name: path.Base(cleaned), isDir: true}, nil
	}
	return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
}

// Exists implements FS.
func (m *MemFS) Exists(name string) bool {
	if name == "" {
		return false
	}
	cleaned := memClean(name)
	if _, ok := m.get(cleaned); ok {
		return true
	}
	return m.directoryExists(cleaned)
}

// IsDir implements FS.
func (m *MemFS) IsDir(name string) bool {
	cleaned := memClean(name)
	if _, ok := m.get(cleaned); ok {
		return false
	}
	return m.directoryExists(cleaned)
}

// Walk implements FS. We iterate over the entries in lexicographic order
// and synthesize directory callbacks at the boundary between entries so
// the walkFn observes the same shape it would on disk: paths carry the
// root as the caller wrote it, so a relative root yields relative paths
// (which is what lets a caller take them relative to that root again),
// and SkipDir leaves the rest of that directory unvisited.
func (m *MemFS) Walk(root string, fn filepath.WalkFunc) error {
	given := root
	root = memClean(root)
	if !m.Exists(root) {
		return &fs.PathError{Op: "walk", Path: given, Err: fs.ErrNotExist}
	}

	// A caller that named the root without its leading slash gets paths
	// without one, the way filepath.Walk reports them.
	emit := func(p string) string {
		if strings.HasPrefix(given, "/") || strings.HasPrefix(given, "\\") {
			return p
		}
		return strings.TrimPrefix(p, "/")
	}

	m.mu.RLock()
	entries := make([]string, 0, len(m.files))
	for p := range m.files {
		if root == "/" || p == root || strings.HasPrefix(p, root+"/") {
			entries = append(entries, p)
		}
	}
	m.mu.RUnlock()
	sort.Strings(entries)

	// Emit the root directory first.
	rootInfo := memFileInfo{name: path.Base(root), isDir: true}
	if err := fn(emit(root), rootInfo, nil); err != nil {
		if errors.Is(err, filepath.SkipDir) {
			return nil
		}
		return err
	}

	emittedDirs := map[string]struct{}{root: {}}
	var skipped []string
	isSkipped := func(p string) bool {
		for _, dir := range skipped {
			if strings.HasPrefix(p, dir+"/") {
				return true
			}
		}
		return false
	}

	for _, file := range entries {
		if isSkipped(file) {
			continue
		}

		// Walk every parent directory between root and the file so the
		// callback sees directory entries in pre-order, just like
		// filepath.Walk.
		dir := path.Dir(file)
		var parents []string
		for cur := dir; cur != root && cur != "/" && cur != "."; cur = path.Dir(cur) {
			if _, seen := emittedDirs[cur]; seen {
				break
			}
			parents = append([]string{cur}, parents...)
		}

		skipParent := false
		for _, p := range parents {
			emittedDirs[p] = struct{}{}
			info := memFileInfo{name: path.Base(p), isDir: true}
			if err := fn(emit(p), info, nil); err != nil {
				if errors.Is(err, filepath.SkipDir) {
					skipped = append(skipped, p)
					skipParent = true
					break
				}
				return err
			}
		}
		if skipParent {
			continue
		}

		// Emit the file itself. SkipDir from a file skips its directory, as
		// filepath.Walk documents.
		data, _ := m.get(file)
		info := memFileInfo{name: path.Base(file), size: int64(len(data))}
		if err := fn(emit(file), info, nil); err != nil {
			if errors.Is(err, filepath.SkipDir) {
				skipped = append(skipped, dir)
				continue
			}
			return err
		}
	}
	return nil
}

// Glob implements FS. Patterns are matched against entries in the
// directory portion of the pattern with `path.Match` semantics, which
// is sufficient for everything the linter uses (currently `*.dic`).
func (m *MemFS) Glob(pattern string) ([]string, error) {
	pattern = memClean(pattern)
	dir, file := path.Split(pattern)
	dir = strings.TrimSuffix(dir, "/")
	if dir == "" {
		dir = "/"
	}
	var matches []string
	m.mu.RLock()
	defer m.mu.RUnlock()
	for p := range m.files {
		if path.Dir(p) != dir {
			continue
		}
		ok, err := path.Match(file, path.Base(p))
		if err != nil {
			return nil, err
		}
		if ok {
			matches = append(matches, p)
		}
	}
	sort.Strings(matches)
	return matches, nil
}

// GlobRecursive implements FS. The pattern is interpreted as a basename
// pattern that matches anywhere under root.
func (m *MemFS) GlobRecursive(root, pattern string) ([]string, error) {
	root = memClean(root)
	var matches []string
	m.mu.RLock()
	defer m.mu.RUnlock()
	for p := range m.files {
		if root != "/" && !strings.HasPrefix(p, root+"/") {
			continue
		}
		ok, err := path.Match(pattern, path.Base(p))
		if err != nil {
			return nil, err
		}
		if ok {
			matches = append(matches, p)
		}
	}
	sort.Strings(matches)
	return matches, nil
}
