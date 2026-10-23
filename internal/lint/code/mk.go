package code

import (
	"regexp"

	"github.com/smacker/go-tree-sitter/bash"
	"github.com/vale-cli/vale/v3/internal/core"
)

// Makefile reads a build file's own comments.
//
// It parses with the shell grammar because the two share a comment syntax and
// no Makefile grammar ships with the parser set. A recipe line is shell in any
// case, and the grammar recovers across a rule header it cannot read, so a
// comment below one is captured exactly as a comment above it is.
func Makefile() *Language {
	return &Language{
		Delims: regexp.MustCompile(`#`),
		Parser: bash.GetLanguage(),
		Queries: []core.Scope{
			{Name: "", Expr: `(comment)+ @comment`, Type: ""},
		},
		Padding: func(s string) int {
			return computePadding(s, []string{"#"})
		},
	}
}
