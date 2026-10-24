//go:build !js

package core

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
)

// DefaultConfig returns the path to the default configuration file.
//
// We don't create this file automatically because there's no actual notion of
// a "default" configuration -- it's just a file loation.
//
// NOTE: if this file does not exist *and* the user has not specified a
// project-specific configuration file, Vale raises an error.
func DefaultConfig() (string, error) {
	root, err := xdg.ConfigFile("vale/.vale.ini")
	if err != nil {
		return "", fmt.Errorf("failed to find default config: %w", err)
	}
	return root, nil
}

// DefaultStylesPath returns the path to the default styles directory.
//
// NOTE: the default styles directory is only used if neither the
// project-specific nor the global configuration file specify a `StylesPath`.
func DefaultStylesPath() (string, error) {
	if fromEnv, hasEnv := os.LookupEnv("VALE_STYLES_PATH"); hasEnv {
		return fromEnv, nil
	}

	styles, err := xdg.DataFile("vale/styles/config.yml")
	if err != nil {
		return "", fmt.Errorf("failed to find default styles: %w", err)
	}

	return filepath.Dir(styles), nil
}
