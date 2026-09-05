package lint

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/glob"
	"github.com/vale-cli/vale/v3/internal/lint/code"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

func updateQueries(f *core.File, views map[string]*core.View) ([]core.Scope, error) {
	var found []core.Scope

	for syntax, view := range views {
		sec, err := glob.Compile(syntax)
		if err != nil {
			return nil, err
		} else if sec.Match(f.Path) {
			found = view.Scopes
		}
	}

	return found, nil
}

// skipsComment reports whether `IgnoredScopes` excludes a comment of this
// scope -- `text.comment.block` for a Python docstring, say.
//
// Both paths that read comments consult this. Which one runs depends on
// whether a markup format is mapped onto the file, and asking for Markdown in
// your comments must not also cost you the ability to exclude one. See #858.
func (l *Linter) skipsComment(scope string) bool {
	ignored := l.Manager.Config.IgnoredScopes
	if core.StringInSlice(scope, ignored) {
		return true
	}
	// `comment` names every comment, and a string literal is not one.
	return !code.IsLiteral(scope) && core.StringInSlice("comment", ignored)
}

// blockScope is the scope a block of text extracted from a file carries.
//
// Everything extracted is prose and belongs to `text`, with one exception. A
// string literal is code that happens to hold words, so it forms a family of
// its own and a rule reaches it only by naming `literal`. Under `text` every
// rule already written would match one, and a source file's literals are
// mostly paths, wire values and map keys.
func blockScope(f *core.File) string {
	if bare := strings.TrimPrefix(f.MetaScope, "."); code.IsLiteral(bare) {
		return bare + f.RealExt
	}
	return "text" + f.MetaScope + f.RealExt
}

func (l *Linter) lintCode(f *core.File) error {
	lang, err := code.GetLanguageFromExt(f.RealExt)
	if err != nil {
		// No tree-sitter grammar available for this file type.
		return l.lintCodeOld(f)
	}

	found, err := updateQueries(f, l.Manager.Config.Views)
	if err != nil {
		return err
	}

	switch {
	// A view supplying its own queries says what to extract in full, so
	// nothing is added on top of one.
	case len(found) > 0:
		lang.Queries = found
	// A string literal is extracted only where a rule asks for it: the family
	// sits outside `text`, so a style naming it nowhere can make no use of
	// one, and a file keeps costing what it did.
	case l.Manager.HasScope(code.LiteralScope):
		code.WithLiterals(lang)
	}

	comments, err := code.GetComments([]byte(f.Content), lang)
	if err != nil {
		return err
	}
	wholeFile := f.Content
	// Read before the loop, which replaces the file's text with each comment's.
	source := strings.Split(wholeFile, "\n")

	last := 0
	for _, comment := range comments {
		f.SetMetaScope(comment.Scope)
		if l.skipsComment(comment.Scope) {
			continue
		}

		// Only a comment opens with the symbol it documents. Masking a
		// literal's first word would blank a real word whenever the code
		// below happened to declare one spelled the same.
		text := comment.Text
		if !code.IsLiteral(comment.Scope) {
			text = maskDocOpener(source, comment)
		}

		err = eachDirectiveRun(f, text, func(text string) error {
			f.SetText(maskCode(text))

			runErr := l.lintLines(f)
			if runErr != nil {
				return runErr
			}

			size := len(f.Alerts)
			if size != last {
				f.Alerts = adjustAlerts(f.Alerts, last, comment, lang)
			}
			last = size
			return nil
		})
		if err != nil {
			return err
		}
	}

	f.SetText(wholeFile)
	return nil
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

// maskDocOpener blanks the word a comment opens with when the declaration
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

// mask blanks a span of a line, leaving its length unchanged so every offset
// taken before it still lands.
func mask(line string, at, width int) string {
	return line[:at] + strings.Repeat(" ", width) + line[at+width:]
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

// fencedCode matches a fenced block, which a comment uses for an example it
// carries whole: a signature, a snippet, a sample of output.
var fencedCode = regexp.MustCompile("(?s)```.*?```")

// inlineCode matches a backtick-delimited span, which a comment uses for the
// notation it names: an identifier, a flag, a path, a wire value. A span never
// spans a line, so a run of backticks left open cannot swallow the rest of a
// comment. It holds at least one character, so the fence a comment opened and
// never closed stays the text it is rather than reading as an empty span.
var inlineCode = regexp.MustCompile("`[^`\n]+`")

// maskCode blanks what a comment marks as code, so a prose rule reads a comment
// the way it reads Markdown. A code comment carries no markup for a parser to
// skip, so a fenced block and a backticked span are masked here instead.
//
// Each becomes spaces of its own byte length, and a line break inside a fence
// survives as one. The text keeps every line and every column, which is what
// lets an alert map back onto the source through the comment's own strip table.
//
// A fence is masked first, so the backticks opening and closing it cannot pair
// with the ones a span inside it carries.
func maskCode(text string) string {
	if !strings.Contains(text, "`") {
		return text
	}

	text = fencedCode.ReplaceAllStringFunc(text, blankKeepingLines)
	return inlineCode.ReplaceAllStringFunc(text, blankKeepingLines)
}

func blankKeepingLines(span string) string {
	var blanked strings.Builder

	for _, b := range []byte(span) {
		if b == '\n' {
			blanked.WriteByte('\n')
			continue
		}
		blanked.WriteByte(' ')
	}

	return blanked.String()
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

// lintCodeOld lints source code by analyzing its comments.
//
// Deprecated: we now use tree-sitter to parse code and collect comments.
func (l *Linter) lintCodeOld(f *core.File) error {
	var line, match, txt string
	var lnLength, padding int
	var block bytes.Buffer

	lines := 0
	comments := core.CommentsByNormedExt[f.NormedExt]
	if len(comments) == 0 {
		return nil
	}

	scanner := bufio.NewScanner(strings.NewReader(f.Content))
	ignored := l.Manager.Config.IgnoredScopes

	skipAll := core.StringInSlice("comment", ignored)
	skipInline := core.StringInSlice("comment.line", ignored)
	skipBlock := core.StringInSlice("comment.block", ignored)

	scope := "%s" + f.RealExt
	inline := regexp.MustCompile(comments["inline"])
	blockStart := regexp.MustCompile(comments["blockStart"])
	blockEnd := regexp.MustCompile(comments["blockEnd"])
	ignore := false
	inBlock := false

	scanner.Split(core.SplitLines)
	for scanner.Scan() {
		line = core.Sanitize(scanner.Text() + "\n")
		lnLength = len(line)
		lines++
		if inBlock {
			// We're in a block comment.
			if match = blockEnd.FindString(line); len(match) > 0 {
				// We've found the end of the block.
				block.WriteString(line)
				txt = block.String()

				b := nlp.NewBlock(
					txt, txt, fmt.Sprintf(scope, "text.comment.block"))
				if !(skipAll || skipBlock) {
					if err := l.lintBlock(f, b, lines+1, 0, true); err != nil {
						return err
					}
				}

				block.Reset()
				inBlock = false
			} else {
				block.WriteString(line)
			}
		} else if match = inline.FindString(line); len(match) > 0 {
			// We've found an inline comment. We need padding here in order to
			// calculate the column span because, for example, a line like
			// 'print("foo") # ...' will be condensed to '# ...'.
			padding = lnLength - len(match)

			b := nlp.NewBlock(
				match, match, fmt.Sprintf(scope, "text.comment.line"))
			if !(skipAll || skipInline) {
				if err := l.lintBlock(f, b, lines, padding-1, true); err != nil {
					return err
				}
			}
		} else if match = blockStart.FindString(line); len(match) > 0 && !ignore {
			// We've found the start of a block comment.
			block.WriteString(line)
			inBlock = true
		} else if match = blockEnd.FindString(line); len(match) > 0 {
			ignore = !ignore
		}
	}
	return nil
}
