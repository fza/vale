//go:build js

// The wasm build has no tree-sitter, which is CGo. A code file falls back to
// comment-regex linting, which is pure Go, so a `tree-sitter` View degrades
// to the language's default comment scopes rather than failing.
package lint

import "github.com/vale-cli/vale/v3/internal/core"

func (l *Linter) lintCode(f *core.File) error {
	return l.lintCodeOld(f)
}
