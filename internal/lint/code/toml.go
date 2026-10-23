package code

import (
	"regexp"

	"github.com/smacker/go-tree-sitter/toml"
	"github.com/vale-cli/vale/v3/internal/core"
)

// TOML extracts `#` comments. A `#` inside a string is not one, which is
// what the grammar knows and a pattern does not.
func TOML() *Language {
	return &Language{
		Delims:  regexp.MustCompile(`#`),
		Parser:  toml.GetLanguage(),
		Queries: []core.Scope{{Name: "", Expr: "(comment) @comment", Type: ""}},
		Padding: func(s string) int {
			return computePadding(s, []string{"#"})
		},
	}
}
