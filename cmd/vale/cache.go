package main

import (
	"fmt"

	"github.com/errata-ai/vale/v3/internal/cache"
	"github.com/errata-ai/vale/v3/internal/core"
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
