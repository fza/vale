package code_test

import (
	"strings"
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/lint/code"
)

const goLiteralSource = "package main\n" +
	"\n" +
	"import \"flag\"\n" +
	"\n" +
	"// Handler reads the pool.\n" +
	"type Handler struct {\n" +
	"\tName string `json:\"name\"`\n" +
	"}\n" +
	"\n" +
	"func main() {\n" +
	"\tflag.String(\"out\", \"\", \"Write the report here\")\n" +
	"}\n"

func goComments(t *testing.T, source string) []code.Comment {
	t.Helper()

	lang, err := code.GetLanguageFromExt(".go")
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

func find(comments []code.Comment, text string) (code.Comment, bool) {
	for _, comment := range comments {
		if comment.Text == text {
			return comment, true
		}
	}
	return code.Comment{}, false
}

// A string literal is reported under its own family, never under `text`, so a
// rule written for prose cannot reach one.
func TestGoLiteralScopeIsNotText(t *testing.T) {
	comment, ok := find(goComments(t, goLiteralSource), "Write the report here")
	if !ok {
		t.Fatal("a help string should be extracted as a literal")
	}

	if comment.Scope != "literal.line" {
		t.Errorf("got scope %q, want %q", comment.Scope, "literal.line")
	}
	if !core.IsLiteral(comment.Scope) {
		t.Errorf("%q should name the literal family", comment.Scope)
	}
}

// The opening quote costs a column, and the alert that lands in the extracted
// text has to come back to the one the source has.
func TestGoLiteralReportsTheSourceColumn(t *testing.T) {
	comment, ok := find(goComments(t, goLiteralSource), "Write the report here")
	if !ok {
		t.Fatal("a help string should be extracted as a literal")
	}

	if comment.Line != 11 {
		t.Errorf("got line %d, want 11", comment.Line)
	}

	// The literal opens at column 24 (zero-based), so its content starts at
	// 25: the literal's own column, and the quote recorded as a strip.
	strip, ok := comment.StripAt(1)
	if !ok {
		t.Fatal("a literal should record what its quoting cost")
	}
	if strip != 1 {
		t.Errorf("the opening quote should be the first line's strip, got %d", strip)
	}
	if comment.Offset+strip != 25 {
		t.Errorf("the first line should begin at the content's source column, got %d", comment.Offset+strip)
	}
}

// A raw string's later lines begin at the margin, so only its first line pays
// for the opening quote.
func TestGoRawLiteralKeepsLaterLinesAtTheMargin(t *testing.T) {
	source := "package main\n\nconst banner = `Usage.\n\n  Read the report here.\n`\n"

	comments := goComments(t, source)

	var found bool
	for _, comment := range comments {
		if comment.Scope != "literal.block" {
			continue
		}
		found = true

		if comment.Line != 3 {
			t.Errorf("got line %d, want 3", comment.Line)
		}
		if first, _ := comment.StripAt(1); comment.Offset+first != 16 {
			t.Errorf("the first line should begin past the opening quote, got %d", comment.Offset+first)
		}
		// The block is dedented by the indentation its lines share, and
		// that is the whole of what the third line paid.
		if third, _ := comment.StripAt(3); third != 2 {
			t.Errorf("got third-line strip %d, want 2", third)
		}
	}

	if !found {
		t.Fatal("a multi-line raw string should be extracted as a literal block")
	}
}

// An import path and a struct tag are read by the compiler, not by a person.
func TestGoStructuralLiteralsAreNotProse(t *testing.T) {
	for _, comment := range goComments(t, goLiteralSource) {
		if !core.IsLiteral(comment.Scope) {
			continue
		}
		if comment.Text == "flag" {
			t.Error("an import path should not be extracted as prose")
		}
		if comment.Text == `json:"name"` {
			t.Error("a struct tag should not be extracted as prose")
		}
	}
}

// A comment keeps the family it always had, and never gains the literal one.
func TestGoCommentScopeIsUnchanged(t *testing.T) {
	var found bool

	for _, comment := range goComments(t, goLiteralSource) {
		if !strings.HasPrefix(comment.Text, "Handler reads the pool.") {
			continue
		}
		found = true

		if comment.Scope != "text.comment.line" {
			t.Errorf("got scope %q, want %q", comment.Scope, "text.comment.line")
		}
	}

	if !found {
		t.Fatal("a line comment should still be extracted")
	}
}

// A caller that never asks for a literal walks the tree for comments alone.
func TestLiteralsAreNotExtractedUnasked(t *testing.T) {
	lang, err := code.GetLanguageFromExt(".go")
	if err != nil {
		t.Fatal(err)
	}

	comments, err := code.GetComments([]byte(goLiteralSource), lang)
	if err != nil {
		t.Fatal(err)
	}

	for _, comment := range comments {
		if core.IsLiteral(comment.Scope) {
			t.Errorf("a literal was extracted unasked: %q", comment.Text)
		}
	}
}

// Asking adds the language's literal queries to the ones it already runs.
func TestWithLiteralsAddsToTheCommentQueries(t *testing.T) {
	lang, err := code.GetLanguageFromExt(".go")
	if err != nil {
		t.Fatal(err)
	}

	before := len(lang.Queries)
	code.WithLiterals(lang)

	if len(lang.Queries) != before+len(lang.Literals) {
		t.Errorf("got %d queries, want %d", len(lang.Queries), before+len(lang.Literals))
	}
	if len(lang.Literals) == 0 {
		t.Error("Go should offer a literal query")
	}
}
