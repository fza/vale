package code

import (
	"context"
	"regexp"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/lint/code/svelte"
)

// Svelte reads a component's code with the TypeScript grammar: every script
// block, and every JavaScript expression the markup holds -- `{...}` in text
// and attribute values, the heads of `{#if}`, `{#each}`, `{#await}`, `{#key}`
// and `{#snippet}`, and the tags `{@html}`, `{@render}`, `{@const}` and
// `{@debug}`. Beside it, the component's markup comments and the comments of
// its style are read with grammars of their own.
//
// The template's visible text is not read: an interface's copy is written in
// the language the interface speaks, which need not be the language the rules
// are written for. Strings in its expressions are read where a rule names the
// literal family, the same as a script's.
func Svelte() *Language {
	lang := TypeScript()
	lang.Isolate = svelteCode
	lang.Embedded = []*Language{svelteMarkup(), svelteStyle()}
	return lang
}

// svelteMarkup reads a component's `<!-- -->` comments. One addressed to a
// tool -- `svelte-ignore`, a formatter's or linter's switch -- is dropped.
//
// Any sentence is valid Svelte, as text between tags, so a commented-out
// line is markup by its shape alone: a tag holding an expression,
// `<Button on:click={save} />`, or a block tag, `{#if open}`.
func svelteMarkup() *Language {
	return &Language{
		Delims:       regexp.MustCompile(`<!--|-->`),
		Parser:       svelte.GetLanguage(),
		CodeSignal:   svelteTagCode,
		CodeUnparsed: true,
		Queries:      []core.Scope{{Name: "", Expr: "(comment) @comment", Type: ""}},
		Directive:    regexp.MustCompile(`^<!--[ \t]*(?:svelte-ignore\b|prettier-ignore\b|eslint-|biome-ignore\b)`),
		Padding: func(s string) int {
			return computePadding(s, []string{"<!--"})
		},
	}
}

// svelteTagCode matches a whole line of Svelte markup that is code: a tag
// holding an expression, or a block tag.
var svelteTagCode = regexp.MustCompile(`^(</?[A-Za-z][\w:.-]*\b[^>]*\{[^}]*\}[^>]*/?>|\{[#:/@][a-z]+\b[^}]*\})`)

// svelteStyle reads the comments of a component's `<style>` blocks with the
// CSS grammar. A block written in SCSS or Less also holds `//` line comments,
// which the grammar knows as its own node type.
func svelteStyle() *Language {
	lang := CSS()
	lang.Delims = regexp.MustCompile(`/\*!?|\*/|//`)
	lang.Queries = []core.Scope{
		{Name: "", Expr: "(comment) @comment", Type: ""},
		{Name: "", Expr: "(js_comment) @comment", Type: ""},
	}
	lang.Padding = func(s string) int {
		return computePadding(s, []string{"/*", "//"})
	}
	lang.Isolate = svelteStyles
	return lang
}

// svelteScriptTypes are the `type` values of a script block that holds
// JavaScript. Any other -- `application/ld+json`, a template -- holds data,
// and is not read.
var svelteScriptTypes = map[string]bool{
	"":                       true,
	"module":                 true,
	"text/javascript":        true,
	"application/javascript": true,
	"text/typescript":        true,
	"application/typescript": true,
	"ts":                     true,
}

// svelteCode returns the component with everything but its JavaScript
// blanked to spaces, line breaks kept, so the grammar reads each part at the
// line and column it holds in the file.
//
// A markup expression opens with a `(` written into the blank before it, the
// `{` that delimits it or the space after its keyword. Bare, `{'Save the
// file'}` would read as a statement holding nothing but a string, which is a
// directive such as `'use strict'` and no literal. The parenthesis is never
// closed: a closing one changes nothing the grammar reports.
func svelteCode(source []byte) []byte {
	blanked := blankAll(source)
	walkSvelte(source, func(node *sitter.Node) bool {
		switch node.Type() {
		case "script_element":
			if !svelteScriptTypes[scriptType(node, source)] {
				return false
			}
			if body := childOfType(node, "raw_text"); body != nil {
				keep(blanked, source, body)
			}
			return false
		case "svelte_raw_text":
			keep(blanked, source, node)
			openParenthesis(blanked, node)
			return false
		}
		return true
	})
	return blanked
}

// svelteStyles returns the component with everything but the contents of its
// style blocks blanked, as svelteCode does for its JavaScript.
func svelteStyles(source []byte) []byte {
	blanked := blankAll(source)
	walkSvelte(source, func(node *sitter.Node) bool {
		if node.Type() != "style_element" {
			return true
		}
		if body := childOfType(node, "raw_text"); body != nil {
			keep(blanked, source, body)
		}
		return false
	})
	return blanked
}

// keep copies the node's text into blanked.
func keep(blanked, source []byte, node *sitter.Node) {
	start, end := node.StartByte(), node.EndByte()
	copy(blanked[start:end], source[start:end])
}

// openParenthesis writes a `(` into the blank before the node. A line break
// there is left alone, so every line stays where it is.
func openParenthesis(blanked []byte, node *sitter.Node) {
	if start := int(node.StartByte()); start > 0 && blanked[start-1] == ' ' {
		blanked[start-1] = '('
	}
}

func blankAll(source []byte) []byte {
	blanked := make([]byte, len(source))
	for i, b := range source {
		if b == '\n' {
			blanked[i] = '\n'
		} else {
			blanked[i] = ' '
		}
	}
	return blanked
}

// walkSvelte parses the component and visits its nodes depth first; visit
// returns whether to descend into the node's children.
func walkSvelte(source []byte, visit func(*sitter.Node) bool) {
	parser := sitter.NewParser()
	parser.SetLanguage(svelte.GetLanguage())

	tree, err := parser.ParseCtx(context.Background(), nil, source)
	if err != nil {
		return
	}

	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if !visit(node) {
			return
		}
		for i := range int(node.ChildCount()) {
			walk(node.Child(i))
		}
	}
	walk(tree.RootNode())
}

// scriptType returns a script block's `type`, lowered and without quotes, or
// "" when it names none.
func scriptType(script *sitter.Node, source []byte) string {
	start := childOfType(script, "start_tag")
	if start == nil {
		return ""
	}
	for i := range int(start.ChildCount()) {
		attr := start.Child(i)
		if attr.Type() != "attribute" {
			continue
		}
		name := childOfType(attr, "attribute_name")
		if name == nil || !strings.EqualFold(name.Content(source), "type") {
			continue
		}
		value := childOfType(attr, "quoted_attribute_value")
		if value == nil {
			value = childOfType(attr, "attribute_value")
		}
		if value == nil {
			return ""
		}
		return strings.ToLower(strings.Trim(value.Content(source), `"'`))
	}
	return ""
}

func childOfType(node *sitter.Node, kind string) *sitter.Node {
	for i := range int(node.ChildCount()) {
		if child := node.Child(i); child.Type() == kind {
			return child
		}
	}
	return nil
}
