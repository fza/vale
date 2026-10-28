package code_test

import (
	_ "embed"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/lint/code"
)

//go:embed testdata/literal/sample.php
var phpLiteralSource string

//go:embed testdata/literal/sample.ts
var tsLiteralSource string

//go:embed testdata/literal/sample.js
var jsLiteralSource string

//go:embed testdata/literal/sample.tsx
var tsxLiteralSource string

func literalComments(t *testing.T, ext, source string) []code.Comment {
	t.Helper()

	lang, err := code.GetLanguageFromExt(ext)
	if err != nil {
		t.Fatal(err)
	}
	code.WithLiterals(lang)

	comments, err := code.GetComments([]byte(source), lang)
	if err != nil {
		t.Fatal(err)
	}

	return comments
}

func literalTexts(comments []code.Comment) []string {
	var texts []string
	for _, comment := range comments {
		if core.IsLiteral(comment.Scope) {
			texts = append(texts, comment.Text)
		}
	}
	return texts
}

func mustFind(t *testing.T, comments []code.Comment, text string) code.Comment {
	t.Helper()

	comment, ok := find(comments, text)
	if !ok {
		t.Fatalf("%q should be extracted, got %q", text, literalTexts(comments))
	}
	return comment
}

func sourceColumn(comment code.Comment) int {
	strip, _ := comment.StripAt(1)
	return comment.Offset + strip
}

func TestLiteralScopePerLanguage(t *testing.T) {
	cases := []struct {
		name, ext, source, literal string
	}{
		{"php single-quoted", ".php", phpLiteralSource, "Import the orders"},
		{"php sprintf format", ".php", phpLiteralSource, "Could not read %s"},
		{"php list element", ".php", phpLiteralSource, "Read the queue first"},
		{"php array value", ".php", phpLiteralSource, "Write the header"},
		{"typescript single-quoted", ".ts", tsLiteralSource, "Open the menu"},
		{"typescript enum value", ".ts", tsLiteralSource, "Save as draft"},
		{"typescript call argument", ".ts", tsLiteralSource, "Saved the draft"},
		{"tsx", ".tsx", tsLiteralSource, "Close the menu"},
		{"javascript", ".js", jsLiteralSource, "Open the menu"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			comments := literalComments(t, tc.ext, tc.source)

			literal := mustFind(t, comments, tc.literal)
			if literal.Scope != "literal.line" {
				t.Errorf("a one-line string should be a literal line, got %q", literal.Scope)
			}

			comment := mustFind(t, comments, "Reads the queue once.")
			if comment.Scope != "text.comment.line" {
				t.Errorf("a comment should keep its own family, got %q", comment.Scope)
			}
		})
	}
}

func TestLiteralsAreNotExtractedUnaskedPerLanguage(t *testing.T) {
	cases := []struct{ ext, source string }{
		{".php", phpLiteralSource},
		{".ts", tsLiteralSource},
		{".tsx", tsLiteralSource},
		{".js", jsLiteralSource},
	}

	for _, tc := range cases {
		t.Run(tc.ext, func(t *testing.T) {
			lang, err := code.GetLanguageFromExt(tc.ext)
			if err != nil {
				t.Fatal(err)
			}

			comments, err := code.GetComments([]byte(tc.source), lang)
			if err != nil {
				t.Fatal(err)
			}

			if texts := literalTexts(comments); len(texts) > 0 {
				t.Errorf("no literal should be extracted unasked, got %q", texts)
			}
			if len(comments) == 0 {
				t.Error("comments should still be extracted")
			}
		})
	}
}

func TestStructuralLiteralsAreNotProse(t *testing.T) {
	cases := []struct {
		name, ext, source, literal string
	}{
		{"php require path", ".php", phpLiteralSource, "bootstrap/autoload.php"},
		{"php attribute argument", ".php", phpLiteralSource, "singleton"},
		{"php named attribute argument", ".php", phpLiteralSource, "string"},
		{"php array key", ".php", phpLiteralSource, "Content-Type"},
		{"php subscript key", ".php", phpLiteralSource, "Accept"},
		{"typescript directive", ".ts", tsLiteralSource, "use strict"},
		{"typescript import source", ".ts", tsLiteralSource, "vue"},
		{"typescript export source", ".ts", tsLiteralSource, "./store"},
		{"typescript ambient module", ".ts", tsLiteralSource, "shims-vue"},
		{"typescript literal type", ".ts", tsLiteralSource, "auditor"},
		{"typescript interface key", ".ts", tsLiteralSource, "title text"},
		{"typescript object key", ".ts", tsLiteralSource, "menu label"},
		{"typescript subscript key", ".ts", tsLiteralSource, "menu key"},
		{"typescript dynamic import", ".ts", tsLiteralSource, "./lazy"},
		{"tsx literal type", ".tsx", tsLiteralSource, "admin"},
		{"javascript import source", ".js", jsLiteralSource, "vue"},
		{"javascript object key", ".js", jsLiteralSource, "menu label"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, text := range literalTexts(literalComments(t, tc.ext, tc.source)) {
				if text == tc.literal {
					t.Errorf("%q is structural and should not be extracted", text)
				}
			}
		})
	}
}

// The code a string interpolates is masked one `*` per character, so the
// sentence around it still reads and every column stays where it was.
func TestInterpolationIsMasked(t *testing.T) {
	cases := []struct {
		name, ext, source, want string
	}{
		{"php variable and braced expression", ".php", phpLiteralSource, `Read ***** for ************ now\n`},
		{"typescript substitutions", ".ts", tsLiteralSource, "Could not read ******* for ************"},
		{"javascript substitution", ".js", jsLiteralSource, "Could not read *******"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			literal := mustFind(t, literalComments(t, tc.ext, tc.source), tc.want)

			quote := 1
			written := literal.Source[quote : len(literal.Source)-quote]
			if utf8.RuneCountInString(written) != utf8.RuneCountInString(literal.Text) {
				t.Error("masking should keep the literal's width in characters")
			}
		})
	}
}

// A character reference in a JSX attribute is text, not interpolated code.
func TestCharacterReferenceIsNotMasked(t *testing.T) {
	mustFind(t, literalComments(t, ".tsx", tsxLiteralSource), "Tom &amp; Jerry")
}

// A nowdoc interpolates nothing, so what looks like a variable is its text.
func TestNowdocIsNotMasked(t *testing.T) {
	literal := mustFind(t, literalComments(t, ".php", phpLiteralSource), "Nowdoc keeps $path as written.")

	if literal.Line != 24 {
		t.Errorf("the body should start on the line after the opener, got %d", literal.Line)
	}
}

// A heredoc is read by its body: neither the opener nor the closing marker is
// prose, and each line records the indentation it lost.
func TestHeredocIsReadByItsBody(t *testing.T) {
	literal := mustFind(t, literalComments(t, ".php", phpLiteralSource), "Usage: import *****\nThen check the log.\n")

	if literal.Scope != "literal.block" {
		t.Errorf("a heredoc spanning lines should be a literal block, got %q", literal.Scope)
	}
	if literal.Line != 20 {
		t.Errorf("the body should start on the line after the opener, got %d", literal.Line)
	}
	if column := sourceColumn(literal); column != 12 {
		t.Errorf("the first line should begin at the body's source column, got %d", column)
	}
	if second, _ := literal.StripAt(2); second != 14 {
		t.Errorf("a later line should record the indentation it lost, got %d", second)
	}
}

// The opening quote is the first line's strip, and an escape keeps its
// written width, so an alert lands on the column the source has.
func TestLiteralReportsTheSourceColumn(t *testing.T) {
	cases := []struct {
		name, ext, source, literal string
		line, column               int
	}{
		{"php double-quoted", ".php", phpLiteralSource, `Read ***** for ************ now\n`, 18, 14},
		{"php escape", ".php", phpLiteralSource, `It\'s done`, 26, 18},
		{"typescript template", ".ts", tsLiteralSource, "Could not read ******* for ************", 13, 17},
		{"typescript escape", ".ts", tsLiteralSource, `It\'s done`, 16, 15},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			literal := mustFind(t, literalComments(t, tc.ext, tc.source), tc.literal)

			if literal.Line != tc.line {
				t.Errorf("the literal should report its own line, got %d", literal.Line)
			}
			if column := sourceColumn(literal); column != tc.column {
				t.Errorf("the first line should begin past the opening quote, got %d", column)
			}
		})
	}
}

// A heredoc's body is content to its edges, so a quote opening and closing it
// stays.
func TestHeredocKeepsTheQuotesAtItsEdges(t *testing.T) {
	mustFind(t, literalComments(t, ".php", phpLiteralSource), "'Quoted' opens and closes 'this'")
}

// A template literal over several lines keeps its later lines' columns.
func TestTemplateLiteralBlock(t *testing.T) {
	literal := mustFind(t, literalComments(t, ".ts", tsLiteralSource), "Usage: import\nThen check the log.\n")

	if literal.Scope != "literal.block" {
		t.Errorf("a template spanning lines should be a literal block, got %q", literal.Scope)
	}
	if !strings.HasPrefix(literal.Source, "`") {
		t.Error("the source should keep the literal as written")
	}
	if second, _ := literal.StripAt(2); second != 2 {
		t.Errorf("a later line should pay its indentation alone, got %d", second)
	}
}
