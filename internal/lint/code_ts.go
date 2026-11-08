//go:build !js

// Native code linting: parse source with tree-sitter, extract the regions a
// language's queries capture (comments by default, or a `tree-sitter` View's
// scopes), and lint each. The grammars are CGo, so this file and the
// internal/lint/code package it imports are native-only; the wasm build falls
// back to the regex-based lintCodeOld (see code_js.go).
package lint

import (
	"fmt"
	"unicode/utf8"

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

	runs := hideCode(f, lang, comments)
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
		l.flagCode(f, runs, i)
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

// commentedCheck names the alert a run of commented-out code is reported as.
const commentedCheck = "Vale.CommentedCode"

// hideCode blanks the commented-out code in each comment, unless f has it
// linted as prose, and returns each comment's runs of it by index.
func hideCode(f *core.File, lang *code.Language, comments []code.Comment) [][]code.CommentedRun {
	if f.Commented == core.CommentedLint {
		return nil
	}
	runs := make([][]code.CommentedRun, len(comments))
	for i := range comments {
		comments[i], runs[i] = lang.HideCode(comments[i])
	}
	return runs
}

// flagCode reports each run of commented-out code in comment i, when f has
// it flagged. The alerts are placed within the comment's text, as the
// comment's own alerts are until adjustAlerts moves them.
func (l *Linter) flagCode(f *core.File, runs [][]code.CommentedRun, i int) {
	if f.Commented != core.CommentedFlag {
		return
	}
	if on, set := f.Checks[commentedCheck]; set && !on {
		return
	}
	level := f.Level(commentedCheck, "warning")
	if core.LevelToInt[level] < l.Manager.Config.MinAlertLevel {
		return
	}
	for _, run := range runs[i] {
		msg := "Commented-out code."
		if n := run.Last - run.First + 1; n > 1 {
			msg = fmt.Sprintf("Commented-out code, %d lines.", n)
		}
		f.Alerts = append(f.Alerts, core.Alert{
			Check: commentedCheck, Severity: level, Message: msg, Match: run.Text,
			Line: run.First,
			Span: []int{run.Column, run.Column + utf8.RuneCountInString(run.Text) - 1},
		})
	}
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
		f.SetText(maskURLs(comment.Text))
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
