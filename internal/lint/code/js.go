package code

import (
	"regexp"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/vale-cli/vale/v3/internal/core"
)

func JavaScript() *Language {
	return &Language{
		Delims: regexp.MustCompile(`//|/\*\*?|\*/`),
		Prefix: cStylePrefix,
		Parser: javascript.GetLanguage(),
		// NOTE: a Cutset of " *" was tried here and cannot work: it makes the
		// dedent read `*` as indentation, which eats a Markdown list, and the
		// alert adjustment has no way to know a non-whitespace character was
		// removed. Prefix above handles the decoration instead.
		Queries:     []core.Scope{{Name: "", Expr: "(comment) @comment", Type: ""}},
		Literals:    jsLiterals,
		SkipLiteral: jsStructuralLiteral,
		LiteralText: jsLiteralText,
		Padding:     cStyle,
		Directive:   jsDirective,
		Doc:         JSDoc,
	}
}

// jsLiterals captures the strings a JavaScript or TypeScript program shows a
// person -- an error's message, a log line -- under `literal`, as Go's are.
// JavaScript, TypeScript and TSX share the node types.
var jsLiterals = []core.Scope{
	{Name: "", Expr: `[
  (string)
  (template_string)
] @literal`, Type: ""},
}

var jsLiteralText = map[string]bool{
	"string_fragment":          true,
	"escape_sequence":          true,
	"html_character_reference": true,
}

// jsStructuralLiteral reports whether a JavaScript or TypeScript string
// literal is one the language reads rather than one a person does.
//
// The position alone settles each case: the module an `import` or `export`
// names or a dynamic `import()` loads, an ambient `declare module '...'`, a
// literal type (`type Role = 'auditor' | 'admin'`), the key of an object, an
// interface, a class member or an enum member, a subscript's key, and a
// directive such as `'use strict'`, which is a string standing as a statement
// of its own.
func jsStructuralLiteral(node *sitter.Node) bool {
	parent := node.Parent()
	if parent == nil {
		return false
	}

	switch parent.Type() {
	case "import_statement", "export_statement", "import_require_clause",
		"module", "literal_type", "computed_property_name", "expression_statement":
		return true
	case "pair", "pair_pattern":
		return sameNode(parent.ChildByFieldName("key"), node)
	case "property_signature", "public_field_definition", "method_definition",
		"method_signature", "abstract_method_signature", "enum_assignment":
		return sameNode(parent.ChildByFieldName("name"), node)
	case "enum_body":
		return true
	case "subscript_expression":
		return sameNode(parent.ChildByFieldName("index"), node)
	case "arguments":
		call := parent.Parent()
		return call != nil && call.Type() == "call_expression" &&
			call.ChildByFieldName("function") != nil &&
			call.ChildByFieldName("function").Type() == "import"
	default:
		return false
	}
}
