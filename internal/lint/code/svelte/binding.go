// Package svelte is the tree-sitter grammar for Svelte components, the C
// sources of tree-sitter-grammars/tree-sitter-svelte v1.0.2 (commit 774a65ae,
// MIT, see LICENSE) built against the smacker/go-tree-sitter runtime.
//
// The grammar ships Go bindings of its own, but they target the
// tree-sitter/go-tree-sitter runtime, a second runtime beside the one every
// other grammar here uses. The grammar the runtime's module bundles predates
// Svelte 5 and reads neither `{#snippet}` nor `{@render}`. The sources are
// vendored unchanged; updating the grammar means replacing them.
package svelte

// #include "tree_sitter/parser.h"
// TSLanguage *tree_sitter_svelte();
import "C"

import (
	"unsafe"

	sitter "github.com/smacker/go-tree-sitter"
)

func GetLanguage() *sitter.Language {
	ptr := unsafe.Pointer(C.tree_sitter_svelte())
	return sitter.NewLanguage(ptr)
}
