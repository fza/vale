package code

import (
	"regexp"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/vale-cli/vale/v3/internal/core"
)

// Go extracts comments, and the string literals a program prints to a person:
// a flag's help text, an error's message, a prompt.
//
// A literal is reported under `literal` rather than under `text`, so a rule
// reaches one only by naming the family. Nothing else would do: a source file
// holds far more strings that are not prose than strings that are -- a wire
// value, a map key, a path, a test fixture -- and putting them where an
// existing rule looks would report every one of them.
func Go() *Language {
	return &Language{
		Delims: regexp.MustCompile(`//|/\*|\*/`),
		Parser: golang.GetLanguage(),
		Queries: []core.Scope{
			{Name: "", Expr: "(comment) @comment", Type: ""},
		},
		Literals: []core.Scope{
			{Name: "", Expr: `[
  (interpreted_string_literal)
  (raw_string_literal)
] @literal`, Type: ""},
		},
		Padding:     cStyle,
		SkipLiteral: goStructuralLiteral,
	}
}

// goStructuralLiteral reports whether a Go string literal is one the compiler
// reads rather than one a person does.
//
// Go spells two structural things as strings: an import path, and the struct
// tag that names a field to an encoder. Both are identifiers in quotes, and a
// rule asking for prose has no way to tell them from the help text beside
// them, so they never reach one.
func goStructuralLiteral(node *sitter.Node) bool {
	parent := node.Parent()
	if parent == nil {
		return false
	}

	switch parent.Type() {
	case "import_spec", "field_declaration":
		return true
	default:
		return false
	}
}
