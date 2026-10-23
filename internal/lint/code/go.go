package code

import (
	"regexp"

	"github.com/smacker/go-tree-sitter/golang"
	"github.com/vale-cli/vale/v3/internal/core"
)

func Go() *Language {
	return &Language{
		Delims:  regexp.MustCompile(`//|/\*|\*/`),
		Parser:  golang.GetLanguage(),
		Queries: []core.Scope{{Name: "", Expr: "(comment) @comment", Type: ""}},
		Padding: cStyle,
		// A doc comment opens with the name it documents and links to
		// others in brackets; a `//go:` line is a directive.
		Directive:     goDirective,
		Doc:           GoDoc,
		DocName:       goDocName,
		OpensWithName: true,
	}
}
