package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/vale-cli/vale/v3/internal/cache"
	"github.com/vale-cli/vale/v3/internal/core"
)

// cleanCache removes every cached result.
func cleanCache(_ []string, _ *core.CLIFlags) error {
	dir, err := cache.Dir()
	if err != nil {
		return core.NewE100("cache-clean", err)
	}

	size, err := cache.Size(dir)
	if err != nil {
		return core.NewE100("cache-clean", err)
	}

	if err = cache.Clean(dir); err != nil {
		return core.NewE100("cache-clean", err)
	}

	fmt.Printf("Removed %s from %s.\n", byteCount(size), dir)
	return nil
}

// cleanCacheLayout deletes the result cache's one-file-per-entry layout from
// the given directory. A run starts it as a process of its own, detached from
// itself, since deleting millions of files takes longer than a run may wait.
func cleanCacheLayout(args []string, _ *core.CLIFlags) error {
	if len(args) != 2 || args[0] != "v0" {
		return core.NewE100("cache-clean-layout", errors.New("usage: cache-clean-layout v0 <dir>"))
	}

	if err := cache.CleanOldLayout(args[1]); err != nil {
		return core.NewE100("cache-clean-layout", err)
	}

	return nil
}

// startOldLayoutClean starts `vale cache-clean-layout v0` on the cache
// directory as a process that outlives this one. It inherits no standard
// stream, so nothing waiting on this run's output waits on it too.
func startOldLayoutClean() error {
	dir, err := cache.Dir()
	if err != nil {
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}

	cmd := exec.Command(exe, "cache-clean-layout", "v0", dir) //nolint:gosec,noctx // our own binary, detached on purpose
	cmd.SysProcAttr = detachedProcess()
	if err = cmd.Start(); err != nil {
		return err
	}

	return cmd.Process.Release()
}

// byteCount renders n in the largest unit that leaves a whole part.
func byteCount(n int64) string {
	const unit = 1024

	if n < unit {
		return fmt.Sprintf("%d B", n)
	}

	div, exp := int64(unit), 0
	for size := n / unit; size >= unit; size /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
