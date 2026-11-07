package code_test

import (
	_ "embed"
	"strings"
	"testing"

	"github.com/vale-cli/vale/v3/internal/lint/code"
)

//go:embed testdata/svelte/runes.svelte
var svelteRunes string

//go:embed testdata/svelte/legacy.svelte
var svelteLegacy string

func TestSvelteReadsScriptsMarkupCodeAndStyleComments(t *testing.T) {
	cases := []struct {
		name, source, text, scope string
		line, column              int
	}{
		{"module script comment", svelteRunes, "Shared by every instance.", "text.comment.line", 2, 5},
		{"module script literal", svelteRunes, "Shared label", "literal.line", 3, 24},
		{"instance script literal", svelteRunes, "Untitled list", "literal.line", 7, 29},
		{"markup comment", svelteRunes, "Lists every item once. ", "text.comment.line", 12, 5},
		{"literal inside a quoted attribute", svelteRunes, "Active state", "literal.line", 14, 26},
		{"attribute expression literal", svelteRunes, "Attribute value", "literal.line", 14, 59},
		{"event handler literal", svelteRunes, "Handler text", "literal.line", 14, 113},
		{"comment in a block head", svelteRunes, "Shown when filled. ", "text.comment.line", 15, 25},
		{"literal in an each body", svelteRunes, "Each text", "literal.line", 16, 57},
		{"else-if condition literal", svelteRunes, "Else-if text", "literal.line", 17, 20},
		{"await head literal", svelteRunes, "Await text", "literal.line", 19, 14},
		{"key head literal", svelteRunes, "Key text", "literal.line", 20, 7},
		{"snippet parameter default", svelteRunes, "Snippet default", "literal.line", 21, 31},
		{"render tag literal", svelteRunes, "Render text", "literal.line", 22, 24},
		{"html tag literal", svelteRunes, "<b>Raw markup</b>", "literal.line", 23, 8},
		{"const tag literal", svelteRunes, "Const text", "literal.line", 24, 21},
		{"style block comment", svelteRunes, "Styles the heading. ", "text.comment.line", 27, 5},
		{"style line comment", svelteRunes, "Spacing follows the grid.", "text.comment.line", 29, 5},
		{"context module doc comment", svelteLegacy, "* Loads the page data. ", "text.comment.line", 2, 4},
		{"context module literal", svelteLegacy, "Preloaded heading", "literal.line", 4, 23},
		{"reactive statement comment", svelteLegacy, "Recomputed when the user changes.", "text.comment.line", 10, 5},
		{"directive handler literal", svelteLegacy, "Clicked once", "literal.line", 14, 28},
		{"plain style comment", svelteLegacy, "Keeps the card narrow. ", "text.comment.line", 20, 5},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			found := mustFind(t, literalComments(t, ".svelte", tc.source), tc.text)

			if found.Scope != tc.scope {
				t.Errorf("the part's own scope should be kept, got %q", found.Scope)
			}
			if found.Line != tc.line {
				t.Errorf("the line should be the component's, got %d", found.Line)
			}
			if column := sourceColumn(found); column != tc.column {
				t.Errorf("the column should be the component's, got %d", column)
			}
		})
	}
}

func TestSvelteSkipsVisibleTextDataAndToolComments(t *testing.T) {
	skipped := []string{
		"Visible heading", "Nothing here", "Fallback header",
		"Structured data", "svelte-ignore", "Generated content", "cdn.example.com",
	}

	for _, source := range []string{svelteRunes, svelteLegacy} {
		for _, comment := range literalComments(t, ".svelte", source) {
			for _, text := range skipped {
				if strings.Contains(comment.Text, text) {
					t.Errorf("%q should not be read; it is visible text, data or a tool's switch", comment.Text)
				}
			}
		}
	}
}

func TestSvelteKeepsAPlainStringOutOfTheDirectivePrologue(t *testing.T) {
	source := "<p title={'Hover text'}>{'Shown text'}</p>\n" +
		"<p>{\n  'Multi-line text'\n}</p>\n" +
		"{@html\n  'Tag on its own line'\n}\n"
	comments := literalComments(t, ".svelte", source)
	lines := map[string]int{"Hover text": 1, "Shown text": 1, "Multi-line text": 3, "Tag on its own line": 6}
	for text, line := range lines {
		if found := mustFind(t, comments, text); found.Line != line {
			t.Errorf("%q should stay on its own line, got %d", text, found.Line)
		}
	}
}

// An expression left open is a syntax error the Svelte compiler refuses too.
// The grammar reads no expression from it, and everything before it is read
// as usual.
func TestSvelteReadsWhatPrecedesAnExpressionLeftOpen(t *testing.T) {
	source := "<!-- Before the break. -->\n<p>{t('Read before')}</p>\n<p>{'Left open"
	comments := literalComments(t, ".svelte", source)
	mustFind(t, comments, "Read before")
	mustFind(t, comments, "Before the break. ")
}

// Each grammar reads its own parts; the comments come back in the order they
// stand in the file, as one grammar's do.
func TestSvelteReturnsTheComponentsPartsInLineOrder(t *testing.T) {
	comments := literalComments(t, ".svelte", svelteRunes)
	for i := 1; i < len(comments); i++ {
		if comments[i].Line < comments[i-1].Line {
			t.Fatalf("%q should not follow %q, which stands below it", comments[i].Text, comments[i-1].Text)
		}
	}
}

func TestSvelteWithoutLiteralRulesReadsCommentsAlone(t *testing.T) {
	lang, err := code.GetLanguageFromExt(".svelte")
	if err != nil {
		t.Fatal(err)
	}
	comments, err := code.GetComments([]byte(svelteRunes), lang)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) == 0 {
		t.Fatal("the comments should still be read")
	}
	for _, comment := range comments {
		if strings.HasPrefix(comment.Scope, "literal") {
			t.Errorf("a literal should be read only where a rule asks for one, got %q", comment.Text)
		}
	}
}
