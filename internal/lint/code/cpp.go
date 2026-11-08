package code

import (
	"regexp"

	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/vale-cli/vale/v3/internal/core"
)

func Cpp() *Language {
	return &Language{
		Delims:        regexp.MustCompile(`//|/\*!?|\*/`),
		Prefix:        cStylePrefix,
		Parser:        cpp.GetLanguage(),
		CodeFrames:    [][2]string{{"", ""}, {"void _() {\n", "\n}"}},
		Queries:       []core.Scope{{Name: "", Expr: "(comment) @comment", Type: ""}},
		Padding:       cStyle,
		Directive:     cDirective,
		OpensWithName: true,
	}
}
