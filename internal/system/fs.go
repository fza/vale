// Package system provides a thin, swappable filesystem abstraction so the
// linter can read styles, dictionaries, vocabularies and other assets from
// the host OS (the default) or from an in-memory map (the WebAssembly host
// supplies one via the JavaScript API in cmd/valew).
//
// All non-test code paths that previously called os.ReadFile, os.Open,
// filepath.Walk, filepath.Glob, etc. should go through the package-level
// helpers in this file. The pluggable backend is swapped with SetDefault.
package system

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/bmatcuk/doublestar/v4"
)

// FS is the surface the linter needs from a filesystem. It intentionally
// follows the os/filepath naming so swapping callers is mechanical.
type FS interface {
	ReadFile(name string) ([]byte, error)
	Open(name string) (io.ReadCloser, error)
	Stat(name string) (fs.FileInfo, error)
	Readlink(name string) (string, error)
	Walk(root string, fn filepath.WalkFunc) error
	Glob(pattern string) ([]string, error)
	GlobRecursive(root, pattern string) ([]string, error)
	Exists(name string) bool
	IsDir(name string) bool
}

// Default is the active backend. The wasm entry point swaps in a MemFS;
// every other build keeps the OS-backed implementation so native tests
// and CLIs continue to behave as before.
var Default FS = OSFS{}

// SetDefault replaces the active filesystem backend.
func SetDefault(fsys FS) { Default = fsys }

// ReadFile returns the contents of the named file from the active backend.
func ReadFile(name string) ([]byte, error) { return Default.ReadFile(name) }

// Open opens the named file from the active backend.
func Open(name string) (io.ReadCloser, error) { return Default.Open(name) }

// Stat returns FileInfo for the named path from the active backend.
func Stat(name string) (fs.FileInfo, error) { return Default.Stat(name) }

// Readlink returns the destination of a symlink (or an error from
// non-OS backends, which have no concept of links).
func Readlink(name string) (string, error) { return Default.Readlink(name) }

// Glob returns the names matching a shell-style pattern in a single
// directory.
func Glob(pattern string) ([]string, error) { return Default.Glob(pattern) }

// GlobRecursive returns every file under root whose basename matches the
// supplied shell-style pattern.
func GlobRecursive(root, pattern string) ([]string, error) {
	return Default.GlobRecursive(root, pattern)
}

// OSFS is the host-OS implementation of FS, used by default.
type OSFS struct{}

func (OSFS) ReadFile(name string) ([]byte, error) { return os.ReadFile(name) }

func (OSFS) Open(name string) (io.ReadCloser, error) { return os.Open(name) }

func (OSFS) Stat(name string) (fs.FileInfo, error) { return os.Stat(name) }

func (OSFS) Readlink(name string) (string, error) { return os.Readlink(name) }

func (OSFS) Walk(root string, fn filepath.WalkFunc) error { return walk(root, root, fn) }

func (OSFS) Glob(pattern string) ([]string, error) { return filepath.Glob(pattern) }

func (OSFS) GlobRecursive(root, pattern string) ([]string, error) {
	return doublestar.FilepathGlob(filepath.Join(root, "**", pattern))
}

func (OSFS) Exists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}

func (OSFS) IsDir(name string) bool {
	fi, err := os.Stat(name)
	return err == nil && fi.IsDir()
}
