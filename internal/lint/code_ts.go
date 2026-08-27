//go:build !js

// Native code linting: parse source with tree-sitter, extract the regions a
// language's queries capture (comments by default, or a `tree-sitter` View's
// scopes), and lint each. The grammars are CGo, so this file and the
// internal/lint/code package it imports are native-only; the wasm build falls
// back to the regex-based lintCodeOld (see code_js.go).
package lint

import (
	"fmt"
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
		err = eachDirectiveRun(f, comment.Text, func(run string) error {
			// A batch converted the comment whole, which a run split at a
			// directive no longer is.
			html := ""
			if run == comment.Text {
				html = converted[i]
			}
			runErr := l.lintComment(f, run, comment.Format, html)
			if runErr != nil {
				return runErr
			}
			f.Alerts = dropMasked(f.Alerts, last, comment)

			size := len(f.Alerts)
			if size != last {
				f.Alerts = adjustAlerts(f.Alerts, last, comment)
			}
			last = size
			return nil
		})
		if err != nil {
			return err
		}
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

// lintComment lints one comment's text as the markup named -- `md`, `rst`, `html`,
// `org`, `adoc`, or `qdoc` -- or as plain lines when none is.
//
// A View's scope names the markup its comments are written in, so a fenced
// block in a Rust `///` comment is code and a `<pre>` in a Javadoc block is
// not prose. The file is a fragment of that markup while the comment is
// read, and itself again afterwards.
//
// html is the comment already converted, when it was part of a batch (see
// rstBatch), and empty otherwise.
func (l *Linter) lintComment(f *core.File, text, format, html string) error {
	if format == "" {
		f.SetText(maskURLs(text))
		return l.lintLines(f)
	}

	kind, ext := f.Format, f.NormedExt
	f.Format = "fragment"
	f.SetNormedExt(format)
	f.SetText(text)

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

// eachDirectiveRun splits a comment at the `vale` control lines it carries and
// hands each run to lint, so a directive works in source the way it works in
// markup. A run is linted under the toggle state its own lines sit beneath,
// which is what makes a rule-specific directive suppress that rule alone.
//
// Every run keeps the comment's full height: a line outside it, and a line the
// directive suppressed, is blank rather than absent. The line count and every
// column survive, which is what lets an alert map back onto the source through
// the comment's own strip table.
//
// A run holding nothing to lint is skipped rather than measured.
//
// The toggle state reaches the file itself, so a directive carries into later
// comments rather than ending with the one it sits in.
func eachDirectiveRun(f *core.File, text string, lint func(string) error) error {
	if !strings.Contains(text, "vale ") && !f.Comments["off"] {
		return lint(text)
	}

	lines := strings.Split(text, "\n")
	run := make([]string, len(lines))
	pending := false

	flush := func() error {
		if !pending {
			return nil
		}
		joined := strings.Join(run, "\n")
		for i := range run {
			run[i] = ""
		}
		pending = false
		return lint(joined)
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if core.IsCommentControl(trimmed) {
			err := flush()
			if err != nil {
				return err
			}
			f.UpdateComments(trimmed)
			continue
		}

		if f.Comments["off"] || trimmed == "" {
			continue
		}

		run[i] = line
		pending = true
	}

	return flush()
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
