package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
)

// The command a run starts to delete the cache's one-file-per-entry layout
// removes that layout and nothing else.
func TestCacheCleanLayoutRemovesTheOldLayout(t *testing.T) {
	dir := t.TempDir()

	shard := filepath.Join(dir, "3f")
	if err := os.MkdirAll(shard, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{filepath.Join(shard, "3f00"), filepath.Join(dir, "trim.txt"), filepath.Join(dir, "manifest")} {
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := cleanCacheLayout([]string{"v0", dir}, &core.CLIFlags{}); err != nil {
		t.Fatal(err)
	}

	for _, gone := range []string{shard, filepath.Join(dir, "trim.txt")} {
		if _, err := os.Stat(gone); err == nil {
			t.Errorf("%s should be removed", gone)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest")); err != nil {
		t.Error("the packed layout should be kept")
	}
}

func TestCacheCleanLayoutRefusesAnUnknownLayout(t *testing.T) {
	for _, args := range [][]string{nil, {"v1", t.TempDir()}, {"v0"}} {
		if err := cleanCacheLayout(args, &core.CLIFlags{}); err == nil {
			t.Errorf("%v should be refused", args)
		}
	}
}
