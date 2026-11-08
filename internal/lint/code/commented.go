package code

import (
	"context"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	sitter "github.com/smacker/go-tree-sitter"
)

// A comment line is commented-out code when the language's own grammar reads
// it, alone, without an error and as more than one token: the method of
// go-critic's commentedOutCode checker, carried from Go's parser to every
// grammar here. Prose almost never parses that way -- two words side by side
// are no statement in most languages -- and a single word, which always
// parses as a name, never counts.
//
// Gates come before the grammar, all erring toward prose, since a line
// wrongly taken for code reaches no rule while one wrongly taken for prose
// is only linted as it always was. A line is prose when:
//
//   - it holds nothing but words and sentence punctuation. Python reads
//     `not needed` and `a, b and c` as expressions. The cost: a line of bare
//     words, `return value` or `import os`, stays prose too.
//   - it ends in a full stop.
//   - it opens with a note's marker (TODO, FIXME, e.g., ...) or a URL, or,
//     where the grammar is strict, with a capitalised label (`Default:`).
//   - it is a list of choices (`active | disabled`), holds a placeholder
//     (`<target>`), or is one Markdown code span.
//   - it holds one word at most and no call, statement end, assignment,
//     step or variable: `(optional)`, a backquoted word, `[link]` and `**bold**` are
//     all expressions somewhere.
//   - it opens a markup tag and holds no `{` expression: `<br>` and
//     `<p>Some text</p>` are JSX.
//   - its grammar reads ordinary sentences as code -- a Ruby or Kotlin line
//     as a call with arguments, a YAML line as a scalar -- and it lacks the
//     language's CodeSignal or holds two words side by side past the first:
//     `Calls foo(bar) twice` is a Kotlin call and a sentence.

// codeMark matches a character no sentence needs: anything but a word
// character, a space or sentence punctuation.
var codeMark = regexp.MustCompile(`[^\w\s,.?!%'"-]`)

// sentenceEnd closes a line with a full stop, which no statement of any
// language here ends with, though Lua's grammar reads `Calls foo() first.`
// without an error, and Go reads a sentence's closing literal, `[]string{"a",
// "b"}.`, as a selector cut short. A stop after a space, `$a .`, is a
// concatenation running on.
var sentenceEnd = regexp.MustCompile(`[^\s.]\.$`)

// proseLabel opens a line with a capitalised label: `Default: 'top' |
// 'bottom'` and `Header: "(n) Label"` are labelled statements in C-like
// grammars, and labels in code are lower-case.
var proseLabel = regexp.MustCompile(`^[A-Z][A-Za-z]*:\s`)

// proseChoice is a list of words or quoted words between bars or slashes,
// `active | disabled | override`, which a grammar reads as an expression.
var proseChoice = regexp.MustCompile(`^(\w+|'[^']*'|"[^"]*")(\s*[|/]\s*(\w+|'[^']*'|"[^"]*"))+$`)

// prosePlaceholder is a word in angle brackets standing alone, `<target>`,
// as a grammar's notation or a usage line writes it.
var prosePlaceholder = regexp.MustCompile(`(^|\s)<[A-Za-z][\w-]*>(\s|$)`)

// proseSpan is a line that is one Markdown code span, a formula set apart
// in a doc comment, which JavaScript reads as a template literal.
var proseSpan = regexp.MustCompile("^`[^`]+`$")

// proseOpener opens a line that is a note to a reader, however it parses:
// a marker, an abbreviation, or a URL, which JavaScript reads as a label and
// a comment.
var proseOpener = regexp.MustCompile(
	`^((?i:todo|fixme|note|xxx|hack|bug|nb|see|warning)\b|(?i:e\.g\.|i\.e\.)|[a-z][\w+.-]*://)`)

// codeWord matches a word: a name, not a number.
var codeWord = regexp.MustCompile(`[A-Za-z_]\w*`)

// codeAct matches what makes a line of one word code: a call, a statement's
// end, an assignment, a step, a variable, or a closing bracket opening the
// line, as in `} else {`, which no sentence opens with.
var codeAct = regexp.MustCompile(`\w\s*\(|[;=]|\+\+|--|\$\w|^[)\]}]`)

// markupOpener opens a line with a markup tag; markupCode is what makes such
// a line JSX rather than markup in prose.
var (
	markupOpener = regexp.MustCompile(`^</?[A-Za-z]`)
	markupCode   = regexp.MustCompile(`\{`)
)

// codeSignal matches what a line of code holds and a sentence does not: an
// assignment or comparison, a call's or index's bracket right after a name,
// a statement's end, a sigil, an arrow or a scope operator.
var codeSignal = regexp.MustCompile(`[=;{}$]|\w[(\[]|->|=>|::|<-|\|\||&&`)

// leadWord is a line's first word where a second follows it: the keyword of
// `local x = 1` or `val x = 1`, which wordRun is not to read as prose.
var leadWord = regexp.MustCompile(`^[A-Za-z_]\w*\s+`)

// wordRun matches a word, or a closing bracket, followed by another word.
var wordRun = regexp.MustCompile(`[\w)\]]\s+[A-Za-z_]`)

// yamlCodeSignal matches a YAML entry: a key with at most one word of value,
// alone or as a list item. `Deprecated: use Run` is a label and a sentence,
// though YAML reads it as an entry.
var yamlCodeSignal = regexp.MustCompile(`^(-\s+)?[\w.-]+:(\s+[^\s#]+)?\s*$`)

// rCodeShape matches a whole line of R: an assignment, a call, or the head
// of a control block or function.
var rCodeShape = regexp.MustCompile(
	`^(` +
		`[\w.]+(\[.*\]|\$[\w.]+)*\s*(<<?-|=)\s*\S.*` +
		`|\S+\s*->>?\s*[\w.]+` +
		`|[\w.]+(::[\w.]+)?\(.*\)` +
		`|(if|for|while)\s*\(.*\)\s*\{?` +
		`|\}\s*else.*` +
		`)\s*$`)

// codeTails close a line that opens a block, so that `if ($ready) {` or
// `def run(self):` reads alone: a brace, Python's `pass`, Ruby's and Lua's
// `end`.
var codeTails = []string{"", "\n}", "\n  pass", "\nend"}

// codeHead opens the block a line closes, so that `} else {` reads alone.
const codeHead = "if (x) {\n"

// CommentedRun is a run of commented-out code in a comment's text.
type CommentedRun struct {
	First, Last int    // the run's first and last line, 1-based
	Column      int    // where the first line's code starts, 1-based, in runes
	Text        string // the first line's code
}

// HideCode returns c with each line of its text that is commented-out code
// blanked, every column kept where it was, and the runs those lines form. A
// line without a word between two code lines -- a lone `}`, an empty line --
// joins the run and is blanked with it, as does one after a run's last line
// that is not empty.
//
// A Live comment is code the build reads, not code taken out of it: it is
// blanked whole and forms no run. Code in a documentation comment is an
// example: it is blanked and forms no run either.
func (lang *Language) HideCode(c Comment) (Comment, []CommentedRun) {
	lines := strings.Split(c.Text, "\n")
	blank := func(i int) { lines[i] = strings.Repeat(" ", len(lines[i])) }
	if c.Live {
		for i := range lines {
			blank(i)
		}
		c.Text = strings.Join(lines, "\n")
		return c, nil
	}

	var (
		runs    []CommentedRun
		run     *CommentedRun
		pending []int
	)

	for i, isCode := range lang.CodeLines(c.Text) {
		switch {
		case isCode && run == nil:
			trimmed := strings.TrimLeftFunc(lines[i], unicode.IsSpace)
			lead := utf8.RuneCountInString(lines[i][:len(lines[i])-len(trimmed)])
			run = &CommentedRun{First: i + 1, Column: lead + 1, Text: strings.TrimSpace(trimmed)}
		case isCode:
			for _, p := range pending {
				blank(p)
			}
		case run != nil && strings.IndexFunc(lines[i], isWordRune) < 0:
			pending = append(pending, i)
			continue
		case run != nil:
			closeRun(run, pending, lines, blank)
			runs = append(runs, *run)
			run = nil
		}
		pending = nil
		if isCode {
			run.Last = i + 1
			blank(i)
		}
	}
	if run != nil {
		closeRun(run, pending, lines, blank)
		runs = append(runs, *run)
	}

	if len(runs) > 0 {
		c.Text = strings.Join(lines, "\n")
	}
	if c.documents() {
		return c, nil
	}
	return c, runs
}

// docOpener opens a documentation comment by its delimiter: Javadoc and its
// kin, Rust's and QDoc's, a Python docstring.
var docOpener = regexp.MustCompile(`^(/\*\*[^/]|/\*!|///|//!|"""|''')`)

// documents reports whether c documents code, so that the code in it is an
// example and not code taken out of the build: a comment its language reads
// as documenting a declaration, or one opened as documentation.
func (c Comment) documents() bool {
	return c.Doc != "" || docOpener.MatchString(c.Source)
}

// closeRun ends a run with the lines without a word that follow it, up to
// the first empty one: the `}` that closes the block a run opened.
func closeRun(run *CommentedRun, pending []int, lines []string, blank func(int)) {
	for _, p := range pending {
		if strings.TrimSpace(lines[p]) == "" {
			return
		}
		blank(p)
		run.Last = p + 1
	}
}

// cgoPreamble reports whether a Go comment is the cgo preamble: the C code
// right above `import "C"`.
func cgoPreamble(n *sitter.Node, src []byte) bool {
	next := n.NextSibling()
	if next == nil || next.Type() != "import_declaration" || next.StartPoint().Row != n.EndPoint().Row+1 {
		return false
	}
	spec := next.NamedChild(0)
	if spec == nil || spec.Type() != "import_spec" {
		return false
	}
	path := spec.ChildByFieldName("path")
	return path != nil && path.Content(src) == `"C"`
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// CodeLines reports, for each line of text, whether it is commented-out code.
func (lang *Language) CodeLines(text string) []bool {
	lines := strings.Split(text, "\n")
	found := make([]bool, len(lines))
	for i, line := range lines {
		found[i] = lang.isCode(strings.TrimSpace(line))
	}
	return found
}

func (lang *Language) isCode(line string) bool {
	if lang.readsAsProse(line) {
		return false
	}
	if lang.CodeUnparsed {
		return true
	}

	frames := lang.CodeFrames
	if frames == nil {
		frames = [][2]string{{"", ""}}
	}
	head := ""
	if strings.HasPrefix(line, "}") {
		head = codeHead
	}
	for _, frame := range frames {
		for _, tail := range codeTails {
			if lang.parsesAsCode(frame[0]+head, line, tail+frame[1]) {
				return true
			}
		}
	}
	return false
}

// readsAsProse reports whether a gate settles the line as prose before its
// grammar reads it.
func (lang *Language) readsAsProse(line string) bool {
	switch {
	case !codeMark.MatchString(line), sentenceEnd.MatchString(line), proseOpener.MatchString(line),
		proseChoice.MatchString(line), prosePlaceholder.MatchString(line), proseSpan.MatchString(line):
		return true
	case len(codeWord.FindAllString(line, 2)) < 2 && !codeAct.MatchString(line):
		return true
	case markupOpener.MatchString(line) && !markupCode.MatchString(line):
		return true
	case lang.CodeSignal == nil:
		return proseLabel.MatchString(line)
	}
	return !lang.CodeSignal.MatchString(line) || wordRun.MatchString(leadWord.ReplaceAllString(line, ""))
}

// parsesAsCode reports whether prefix+line+suffix parses without an error
// and the line itself holds more than one token.
func (lang *Language) parsesAsCode(prefix, line, suffix string) bool {
	parser := sitter.NewParser()
	parser.SetLanguage(lang.Parser)
	tree, err := parser.ParseCtx(context.Background(), nil, []byte(prefix+line+suffix))
	if err != nil {
		return false
	}
	root := tree.RootNode()
	if root.HasError() {
		return false
	}
	start := uint32(len(prefix))                                 //nolint:gosec // a frame of a few bytes
	return countTokens(root, start, start+uint32(len(line))) > 1 //nolint:gosec // one comment line, far below 4 GiB
}

// countTokens counts the leaves of the tree that lie between start and end:
// the tokens of the line, not of the frame around it.
func countTokens(node *sitter.Node, start, end uint32) int {
	if node.EndByte() <= start || node.StartByte() >= end {
		return 0
	}
	if node.ChildCount() == 0 {
		if node.EndByte() > node.StartByte() {
			return 1
		}
		return 0
	}
	count := 0
	for i := range int(node.ChildCount()) {
		count += countTokens(node.Child(i), start, end)
	}
	return count
}
