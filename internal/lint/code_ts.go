//go:build !js

// Native code linting: parse source with tree-sitter, extract the regions a
// language's queries capture (comments by default, or a `tree-sitter` View's
// scopes), and lint each. The grammars are CGo, so this file and the
// internal/lint/code package it imports are native-only; the wasm build falls
// back to the regex-based lintCodeOld (see code_js.go).
package lint

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/lint/code"
)

// languageFor returns the tree-sitter grammar for a file. When the file's own
// extension has none, the format it is read as supplies it: `NormedExt` holds
// what `[formats]` or `--ext` resolved the file to, so `es = js` parses a
// `.es` file as JavaScript.
func (l *Linter) languageFor(f *core.File) (*code.Language, error) {
	lang, err := code.GetLanguageFromExt(f.RealExt)
	if err == nil {
		return lang, nil
	}
	if mapped, mappedErr := code.GetLanguageFromExt(f.NormedExt); mappedErr == nil {
		return mapped, nil
	}
	return nil, err
}

func (l *Linter) lintCode(f *core.File) error {
	lang, err := l.languageFor(f)
	if err != nil {
		// No tree-sitter grammar available for this file type.
		return l.lintCodeOld(f)
	}

	if found := updateQueries(f, l.Manager.Config); len(found) > 0 {
		lang.Queries = found
	}

	comments, err := code.GetComments([]byte(f.Content), lang)
	if err != nil {
		return err
	}
	wholeFile := f.Content

	converted := l.rstBatch(f, comments, "")

	last := len(f.Alerts) // the file may hold alerts from cells before this one
	for i, comment := range comments {
		f.SetMetaScope(comment.Scope)
		if l.skipsComment(comment.Scope) {
			continue
		}
		err = l.lintComment(f, comment, comment.Format, converted[i])
		if err != nil {
			return err
		}
		f.Alerts = dropMasked(f.Alerts, last, comment)

		size := len(f.Alerts)
		if size != last {
			f.Alerts = adjustAlerts(f.Alerts, last, comment)
		}
		last = size
	}

	f.SetText(wholeFile)
	if f.Summary.Len() > 0 {
		// The comments read as markup added up to a summary; see lintSizedScopes.
		return l.lintSummary(f)
	}
	return nil
}

// rstBatch converts every comment written in reStructuredText in one
// Docutils call, and returns each one's HTML by index. fallback is the
// format a comment has when its query named none. Nothing is returned when
// fewer than two comments qualify or the batch fails, and each comment is
// then converted on its own.
func (l *Linter) rstBatch(f *core.File, comments []code.Comment, fallback string) map[int]string {
	var (
		docs []string
		idx  []int
	)

	// Each comment is prepared as the fragment it will be linted as; see
	// lintComment.
	wholeFile, kind, ext := f.Content, f.Format, f.NormedExt
	f.Format = "fragment"
	f.SetNormedExt("rst")
	defer func() {
		f.Format, f.NormedExt = kind, ext
		f.SetText(wholeFile)
	}()

	for i, c := range comments {
		format := c.Format
		if format == "" {
			format = fallback
		}
		if format != "rst" || l.skipsComment(c.Scope) {
			continue
		}

		f.SetText(c.Text)
		s, err := l.prepareRST(f)
		if err != nil {
			return nil
		}
		docs = append(docs, s)
		idx = append(idx, i)
	}

	if len(docs) < 2 {
		return nil
	}
	parts, err := l.convertRSTBatch(docs)
	if err != nil {
		return nil
	}

	out := make(map[int]string, len(idx))
	for k, i := range idx {
		out[i] = parts[k]
	}
	return out
}

// dropMasked removes the alerts, from last on, that the comment's
// documentation convention masks: the symbol a tag names, the body of an
// example. The alerts are still placed within the comment's text.
func dropMasked(alerts []core.Alert, last int, comment code.Comment) []core.Alert {
	if len(comment.Masks) == 0 {
		return alerts
	}
	kept := alerts[:last]
	for _, a := range alerts[last:] {
		if !comment.Masked(a.Line, a.Span[0], a.Span[1]) {
			kept = append(kept, a)
		}
	}
	return kept
}

// lintComment lints one comment as the markup named -- `md`, `rst`, `html`,
// `org`, `adoc`, or `qdoc` -- or as plain lines when none is.
//
// A View's scope names the markup its comments are written in, so a fenced
// block in a Rust `///` comment is code and a `<pre>` in a Javadoc block is
// not prose. The file is a fragment of that markup while the comment is
// read, and itself again afterwards.
//
// html is the comment already converted, when it was part of a batch (see
// rstBatch), and empty otherwise.
func (l *Linter) lintComment(f *core.File, comment code.Comment, format, html string) error {
	if format == "" {
		f.SetText(maskCode(maskURLs(comment.Text)))
		return l.lintLines(f)
	}

	kind, ext := f.Format, f.NormedExt
	f.Format = "fragment"
	f.SetNormedExt(format)
	f.SetText(comment.Text)

	var err error
	switch f.NormedExt {
	case ".md":
		err = l.lintMarkdown(f)
	case ".rst":
		if html != "" {
			err = l.lintRSTHTML(f, html)
		} else {
			err = l.lintRST(f)
		}
	case ".html":
		err = l.lintHTML(f)
	case ".adoc":
		err = l.lintADoc(f)
	case ".org":
		err = l.lintOrg(f)
	case ".qdoc":
		err = l.lintQDocFragment(f)
	default:
		err = fmt.Errorf("unsupported markup format '%s'", format)
	}

	f.Format, f.NormedExt = kind, ext
	return err
}

// fencedCode matches a fenced block, which a comment uses for an example it
// carries whole: a signature, a snippet, a sample of output.
var fencedCode = regexp.MustCompile("(?s)```.*?```")

// inlineCode matches a backtick-delimited span, which a comment uses for the
// notation it names: an identifier, a flag, a path, a wire value. A span never
// spans a line, so a run of backticks left open cannot swallow the rest of a
// comment. It holds at least one character, so the fence a comment opened and
// never closed stays the text it is rather than reading as an empty span.
var inlineCode = regexp.MustCompile("`[^`\n]+`")

// maskCode masks what a comment marks as code, so a prose rule reads a comment
// the way it reads Markdown. A code comment carries no markup for a parser to
// skip, so a fenced block and a backticked span are masked here instead.
//
// Each character becomes a `*`, Markdown's own mask for inline code, and a line
// break inside a fence survives as one. A rule's space matches any run of
// whitespace, so a span blanked to spaces let a pattern join the words on
// either side of it; a `*` is no space and no word character, so nothing
// crosses it and nothing counts it as a word. The text keeps every line and
// every column, counted in characters as an alert counts them, which is what
// lets an alert map back onto the source through the comment's own strip table.
//
// A fence is masked first, so the backticks opening and closing it cannot pair
// with the ones a span inside it carries.
func maskCode(text string) string {
	if !strings.Contains(text, "`") {
		return text
	}

	text = fencedCode.ReplaceAllStringFunc(text, maskKeepingLines)
	return inlineCode.ReplaceAllStringFunc(text, maskKeepingLines)
}

func maskKeepingLines(span string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		return '*'
	}, span)
}

// lintCodeOld lints source code by analyzing its comments.
//
// Deprecated: we now use tree-sitter to parse code and collect comments.

// lintDataComments lints the comments of a data file that has a grammar --
// YAML or TOML -- and does nothing for one that has no comments to read.
func (l *Linter) lintDataComments(f *core.File) error {
	if _, err := code.GetLanguageFromExt(f.RealExt); err == nil {
		return l.lintCode(f)
	}
	return nil
}
