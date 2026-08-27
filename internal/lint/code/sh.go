package code

import (
	"regexp"

	"github.com/smacker/go-tree-sitter/bash"
	"github.com/vale-cli/vale/v3/internal/core"
)

func Shell() *Language {
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
