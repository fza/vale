//go:build js

package core

import "errors"

// A browser has no user-level configuration directory: every asset comes
// from the host through the in-memory filesystem.

// DefaultConfig returns the path to the user's default config file.
func DefaultConfig() (string, error) {
	return "", errors.New("no default config in a browser")
}

// DefaultStylesPath returns the path to the default styles directory.
func DefaultStylesPath() (string, error) {
	return "", errors.New("no default styles in a browser")
}
