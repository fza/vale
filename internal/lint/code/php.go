package code

import (
	"regexp"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/php"
	"github.com/vale-cli/vale/v3/internal/core"
)

// PHP extracts comments, and the string literals a program shows a person: an
// exception's message, a log line, a command's output. A literal is reported
// under `literal`, as Go's are, so a rule reaches one only by naming the
// family.
//
// A heredoc or nowdoc is captured by its body, so its opener and closing
// marker never reach a rule, and its lines are dedented by the indentation
// they share, which is the indentation PHP itself removes.
func PHP() *Language {
	return &Language{
		Delims:  regexp.MustCompile(`//|/\*\*?|\*/|#`),
		Prefix:  cStylePrefix,
		Parser:  php.GetLanguage(),
		Queries: []core.Scope{{Name: "", Expr: "(comment) @comment", Type: ""}},
		Literals: []core.Scope{
			{Name: "", Expr: `[
  (string)
  (encapsed_string)
  (heredoc_body)
  (nowdoc_body)
] @literal`, Type: ""},
		},
		SkipLiteral: phpStructuralLiteral,
		LiteralText: map[string]bool{
			"string_content":  true,
			"escape_sequence": true,
			"nowdoc_string":   true,
		},
		Padding: func(s string) int {
			return computePadding(s, []string{"//", "/*", "#"})
		},
	}
}

// phpStructuralLiteral reports whether a PHP string literal is one the
// language reads rather than one a person does.
//
// Three positions hold a string no one reads aloud: an attribute's argument
// (`#[Flow\Scope('singleton')]`, `#[ORM\Column(type: 'string')]`), an array
// key, whether written in a literal or read through a subscript, and the path
// an `include` or `require` loads. The position alone settles each, so they
// never reach a rule.
func phpStructuralLiteral(node *sitter.Node) bool {
	parent := node.Parent()
	if parent == nil {
		return false
	}

	switch parent.Type() {
	case "array_element_initializer":
		return isArrayKey(parent, node)
	case "subscript_expression":
		return !sameNode(parent.Child(0), node)
	case "include_expression", "include_once_expression", "require_expression", "require_once_expression":
		return true
	}

	for ancestor := parent; ancestor != nil; ancestor = ancestor.Parent() {
		if ancestor.Type() == "attribute" {
			return true
		}
	}
	return false
}

// isArrayKey reports whether node is the key of a `key => value` element.
func isArrayKey(element, node *sitter.Node) bool {
	if !sameNode(element.Child(0), node) {
		return false
	}

	for i := 1; i < int(element.ChildCount()); i++ {
		if element.Child(i).Type() == "=>" {
			return true
		}
	}
	return false
}

func sameNode(a, b *sitter.Node) bool {
	return a != nil && b != nil &&
		a.StartByte() == b.StartByte() && a.EndByte() == b.EndByte() && a.Type() == b.Type()
}
