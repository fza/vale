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

	l.setQueries(f, lang)

	comments, err := code.GetComments([]byte(f.Content), lang)
	if err != nil {
		return err
	}
	wholeFile := f.Content
	// Read before the loop, which replaces the file's text with each comment's.
	source := strings.Split(wholeFile, "\n")

	converted := l.rstBatch(f, comments, "")

	last := len(f.Alerts) // the file may hold alerts from cells before this one
	for i, comment := range comments {
		f.SetMetaScope(comment.Scope)
		if l.skipsComment(comment.Scope) {
			continue
		}
		// Only a comment opens with the symbol it documents, and only where
		// the language's convention asks it to. Masking anything else would
		// blank a real word whenever the code below happened to use one
		// spelled the same.
		text := comment.Text
		if lang.OpensWithName && !core.IsLiteral(comment.Scope) {
			text = maskDocOpener(source, comment)
		}

		err = eachDirectiveRun(f, text, func(run string) error {
			// Hidden run by run, so that code under `vale off` is not
			// flagged either.
			run, found := hideCode(f, lang, comment, run)

			// A batch converted the comment whole, which a run masked,
			// split at a directive, or cleared of code no longer is.
			html := ""
			if run == comment.Text {
				html = converted[i]
			}
			runErr := l.lintComment(f, run, comment.Format, html)
			if runErr != nil {
				return runErr
			}
			l.flagCode(f, found)
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

// setQueries says what to extract from a file. A view supplying its own
// queries says it in full, so nothing is added on top of one. A string
// literal is extracted only where a rule asks for it: the family sits outside
// `text`, so a style naming it nowhere can make no use of one, and a file
// keeps costing what it did.
func (l *Linter) setQueries(f *core.File, lang *code.Language) {
	switch found := updateQueries(f, l.Manager.Config); {
	case len(found) > 0:
		lang.Queries = found
	case l.Manager.HasScope(core.LiteralScope):
		code.WithLiterals(lang)
	}
}

// commentFormat is the markup a comment is read as: the one its query named,
// else fallback. A string literal is never markup, whatever is mapped onto
// the file: read as a document, it would be rebuilt under `text`, where every
// rule already written would match it.
func commentFormat(comment code.Comment, fallback string) string {
	switch {
	case core.IsLiteral(comment.Scope):
		return ""
	case comment.Format != "":
		return comment.Format
	}
	return fallback
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
		if commentFormat(c, fallback) != "rst" || l.skipsComment(c.Scope) {
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

// hideCode blanks the commented-out code in text, the comment's text or a
// run of it, unless f has it linted as prose, and returns the runs of it. A
// string literal is code already.
func hideCode(f *core.File, lang *code.Language, comment code.Comment, text string) (string, []code.CodeRun) {
	if f.Commented == core.CommentedLint || core.IsLiteral(comment.Scope) {
		return text, nil
	}
	comment.Text = text
	hidden, runs := lang.HideCode(comment)
	return hidden.Text, runs
}

// flagCode reports each run of commented-out code, when f has it flagged.
// The alerts are placed within the comment's text, as the comment's own
// alerts are until adjustAlerts moves them.
func (l *Linter) flagCode(f *core.File, runs []code.CodeRun) {
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
	for _, run := range runs {
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
		f.SetText(maskCode(maskURLs(text)))
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
		if directive, ok := core.NormalizeDirective(trimmed); ok {
			trimmed = directive
		}

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

// docOpener matches the run of names a comment opens with, which is the only
// position backticks cannot reach: the convention asks for the symbol bare. One
// comment may document several symbols at once, written as names separated by
// slashes, and every name in that run is asked for bare. A colon after the run
// makes the comment an annotation rather than documentation, so the words are a
// label and stay prose.
var docOpener = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*(?:\s*/\s*[A-Za-z_][A-Za-z0-9_]*)*)(?:[^:\w]|$)`)

// packageOpener matches the name in a package comment, which opens with the
// word `Package` before it.
var packageOpener = regexp.MustCompile(`^(Package\s+)([A-Za-z_][A-Za-z0-9_]*)(?:[^:\w]|$)`)

// maskDocOpener masks the word a comment opens with when the declaration
// beneath names it, so a documented symbol reads as code rather than as prose.
// Go, C, Swift and Rust all ask a doc comment to open with the name it
// documents, and no markup is allowed in that position.
//
// The name has to appear in the declaration for the mask to apply, so a comment
// opening with a misspelled name still reports: nothing beneath it matches.
func maskDocOpener(source []string, comment code.Comment) string {
	lines := strings.Split(comment.Text, "\n")

	opener := 0
	for opener < len(lines) && strings.TrimSpace(lines[opener]) == "" {
		opener++
	}
	if opener == len(lines) {
		return comment.Text
	}

	indent := len(lines[opener]) - len(strings.TrimLeft(lines[opener], " \t"))
	rest := lines[opener][indent:]

	// A package comment opens with the word `Package` and then one name, which
	// the convention asks for bare in that second position.
	if pm := packageOpener.FindStringSubmatch(rest); pm != nil {
		if !declares(source, comment, pm[2]) {
			return comment.Text
		}

		lines[opener] = mask(lines[opener], indent+len(pm[1]), len(pm[2]))

		return strings.Join(lines, "\n")
	}

	m := docOpener.FindStringSubmatch(rest)
	if m == nil {
		return comment.Text
	}

	names, offsets := runNames(m[1])
	if len(names) == 0 {
		return comment.Text
	}

	// The first name documents the declaration directly beneath, which is the
	// check a single-name opener already answers. Masking nothing when it fails
	// keeps a misspelled opener reporting.
	if !declares(source, comment, names[0]) {
		return comment.Text
	}

	// Every later name in the run is a sibling of that declaration rather than
	// the declaration itself, so it is read from the block below instead of
	// from one line. A name missing there stays prose and still reports.
	block := declarationBlock(source, comment, len(names)+blockSlack)

	for i, name := range names {
		if i > 0 && !namedInAny(block, name) {
			continue
		}

		lines[opener] = mask(lines[opener], indent+offsets[i], len(name))
	}

	return strings.Join(lines, "\n")
}

// blockSlack is how many lines beyond the run's own length the block may carry,
// covering the braces and blank lines a declaration puts between its names.
const blockSlack = 4

// mask covers a span of a line with `*`, Markdown's own mask for inline code,
// leaving its length unchanged so every offset taken before it still lands. A
// rule's space matches any run of whitespace, so a name blanked to spaces let a
// pattern join the words on either side of it, `Package` and the verb after
// the name; a `*` is no space and no word character, so nothing crosses it and
// nothing counts it as a word.
func mask(line string, at, width int) string {
	return line[:at] + strings.Repeat("*", width) + line[at+width:]
}

// runNames splits an opening run into its names, with each name's own offset
// inside the run. A run holding an empty part is not a run of names.
func runNames(run string) ([]string, []int) {
	var (
		names   []string
		offsets []int
		at      int
	)

	for _, part := range strings.Split(run, "/") {
		name := strings.TrimSpace(part)
		if name == "" {
			return nil, nil
		}

		names = append(names, name)
		offsets = append(offsets, at+strings.Index(part, name))
		at += len(part) + 1
	}

	return names, offsets
}

// declarationBlock reads the lines a declaration occupies below a comment,
// skipping blanks and comments, and stopping once it holds limit of them.
func declarationBlock(source []string, comment code.Comment, limit int) []string {
	below := commentEnd(comment)

	var block []string
	for i := below; i < len(source) && len(block) < limit; i++ {
		line := strings.TrimSpace(source[i])
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "*") ||
			strings.HasPrefix(line, "/*") || strings.HasPrefix(line, "#") {
			continue
		}

		block = append(block, line)
	}

	return block
}

// namedInAny reports whether any line of a block names the given word.
func namedInAny(block []string, name string) bool {
	for _, line := range block {
		if namedIn(line, name) {
			return true
		}
	}

	return false
}

// declares reports whether the source below a comment names the given word. It
// reads the first line that is neither blank nor a comment of its own, which is
// where a declaration sits in every language carrying this convention.
func declares(source []string, comment code.Comment, name string) bool {
	below := commentEnd(comment)

	for i := below; i < len(source) && i < below+4; i++ {
		line := strings.TrimSpace(source[i])
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "*") ||
			strings.HasPrefix(line, "/*") || strings.HasPrefix(line, "#") {
			continue
		}
		return namedIn(line, name)
	}
	return false
}

// commentEnd is the first source line below a comment, indexed from zero. The
// comment's own last line is the line before it, and a trailing newline adds no
// line, so it is trimmed first.
func commentEnd(comment code.Comment) int {
	return comment.Line - 1 + strings.Count(strings.TrimRight(comment.Source, "\n"), "\n") + 1
}

// namedIn reports whether a line carries the name as a whole word rather than
// as part of a longer one.
func namedIn(line, name string) bool {
	for at := 0; ; {
		i := strings.Index(line[at:], name)
		if i < 0 {
			return false
		}
		i += at

		before := i == 0 || !isWordByte(line[i-1])
		end := i + len(name)
		after := end == len(line) || !isWordByte(line[end])

		if before && after {
			return true
		}
		at = i + 1
	}
}

func isWordByte(b byte) bool {
	return b == '_' ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
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
