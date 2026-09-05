package code

import (
	"bytes"
	"regexp"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

type QueryEngine struct {
	tree   *sitter.Tree
	lang   *Language
	cutset string

	// delims is the language's Delims pattern in leftmost-longest mode, so
	// that `#=` is read as one opener rather than `#` and a stray `=`.
	delims *regexp.Regexp
}

// defaultCutset is the indentation a comment is dedented by when its language
// doesn't name its own. Tabs belong here alongside spaces: a block comment
// indented with tabs that keeps them reads as an indented code block in
// Markdown, so its body is never linted -- see #1130.
const defaultCutset = " \t"

func NewQueryEngine(tree *sitter.Tree, lang *Language) *QueryEngine {
	cutset := lang.Cutset
	if cutset == "" {
		cutset = defaultCutset
	}

	delims := regexp.MustCompile(lang.Delims.String())
	delims.Longest()

	return &QueryEngine{
		tree:   tree,
		lang:   lang,
		cutset: cutset,
		delims: delims,
	}
}

func (qe *QueryEngine) run(meta string, q *sitter.Query, source []byte) []Comment {
	var comments []Comment

	if meta != "" {
		meta = "." + meta
	}

	qc := sitter.NewQueryCursor()
	qc.Exec(q, qe.tree.RootNode())

	for {
		m, ok := qc.NextMatch()
		if !ok {
			break
		}

		m = qc.FilterPredicates(m, source)
		for _, c := range m.Captures {
			name := q.CaptureNameForId(c.Index)

			// A capture named with a leading underscore exists for a
			// predicate to test, not to be linted -- the convention
			// tree-sitter itself uses for internal captures. Without this,
			// the only way to test one node and extract another is to test
			// the node you extract, which forces a query to capture more
			// than the prose it wants.
			if strings.HasPrefix(name, "_") {
				continue
			}

			// A capture named `prose` is bare content -- Elixir's
			// `quoted_content`, the body of a string -- and holds no
			// decoration at all, so nothing may be taken off it. Everything
			// else -- `comment`, `docstring` -- carries its delimiters.
			prose := name == "prose"

			// A capture named `literal` is a string the program itself
			// carries: a flag's help text, an error's message. It is quoted
			// rather than delimited, and it belongs to a family of its own
			// so that a rule written for prose cannot reach it.
			literal := name == LiteralScope
			if literal && qe.lang.SkipLiteral != nil && qe.lang.SkipLiteral(c.Node) {
				continue
			}

			rText := c.Node.Content(source)
			row := int(c.Node.StartPoint().Row)
			offset := int(c.Node.StartPoint().Column)

			// The Lua grammar's comment tokens swallow the whitespace --
			// newlines included -- that precedes them; shift the node past
			// it so Line and Offset point at the delimiter.
			trimmed := strings.TrimLeft(rText, " \t\n")
			if cut := len(rText) - len(trimmed); cut > 0 {
				pre := rText[:cut]
				if n := strings.Count(pre, "\n"); n > 0 {
					row += n
					offset = cut - (strings.LastIndexByte(pre, '\n') + 1)
				} else {
					offset += cut
				}
				rText = trimmed
			}

			cText := rText
			switch {
			case literal:
				cText = trimQuotes(rText)
			case prose:
			default:
				cText = qe.stripDelims(rText)
			}

			var strip []int

			// A literal is code that happens to hold words, so it forms its
			// own family. Under `text` every rule already written would match
			// one, which is every path, wire value and map key in the file.
			family := "text.comment"
			if literal {
				family = LiteralScope
			}

			scope := family + meta + ".line"
			// A trailing newline is part of some grammars' tokens; only a
			// newline between content makes a comment a block.
			if strings.Count(strings.TrimRight(cText, "\n"), "\n") > 0 {
				scope = family + meta + ".block"

				// Blank the per-line decoration before measuring indentation,
				// so ` * text` is dedented as three spaces rather than read as
				// a Markdown list item. Blanking keeps the width, so nothing
				// has moved yet; the dedent below takes it off and records how
				// much.
				if !prose && !literal {
					cText = qe.blankPrefixes(cText)
				}

				// Dedent like Python's inspect.cleandoc: the first line sits on
				// (or just after) the opening delimiter, so its leading
				// whitespace is incidental and is trimmed fully; the remaining
				// lines are dedented only by the indentation common to them.
				// This removes a comment's base indentation while preserving
				// relative indentation, which is significant for markup such as
				// RST literal blocks and Markdown indented code. See #1028.
				lines := strings.Split(cText, "\n")
				common := commonIndent(lines[1:], qe.cutset)

				buf := bytes.Buffer{}
				strip = make([]int, len(lines))
				for i, line := range lines {
					var out string
					if i == 0 {
						out = strings.TrimLeft(line, qe.cutset)
					} else {
						out = stripIndent(line, common, qe.cutset)
					}

					// What came off this line, so an alert reported against it
					// can be put back exactly. Re-deriving this downstream from
					// the source line is what makes a non-whitespace cutset
					// report the wrong column.
					strip[i] = len(line) - len(out)

					buf.WriteString(out)
					buf.WriteString("\n")
				}

				cText = buf.String()
			}

			// What the opening quote costs a column is recorded as a strip
			// rather than as an offset, because only the first line pays it.
			// A raw string's later lines begin at the margin, and carrying the
			// quote's column into them would push every alert on them right by
			// however far in the string began.
			if literal {
				if strip == nil {
					strip = []int{offset + 1}
				} else {
					strip[0] += offset + 1
				}
				offset = 0
			}

			comments = append(comments, Comment{
				Line:   row + 1,
				Offset: offset,
				Scope:  scope,
				Text:   cText,
				Source: rText,
				Strip:  strip,
			})
		}
	}

	return comments
}

// stripDelims takes a comment's delimiters off its edges: the marker or
// opener at the very start -- repeated, so a `##` banner comes off whole --
// and one closer against the end, trailing whitespace aside.
//
// Only the edges: a delimiter character inside the prose -- the `#` of a
// Markdown heading, the `//` of a URL -- is content, and stripping it shifted
// every alert to its right by its width. Per-line decoration inside a block
// is blankPrefixes's job.
func (qe *QueryEngine) stripDelims(s string) string {
	// The opener, repeated so a doubled marker comes off whole.
	for {
		loc := qe.delims.FindStringIndex(s)
		if loc == nil || loc[0] != 0 || loc[1] == 0 {
			break
		}
		s = s[loc[1]:]
	}

	// The closer: one match running to the end, trailing whitespace aside.
	body := strings.TrimRight(s, " \t\n")
	tail := s[len(body):]
	if locs := qe.delims.FindAllStringIndex(body, -1); len(locs) > 0 {
		if last := locs[len(locs)-1]; last[1] == len(body) {
			s = body[:last[0]] + tail
		}
	}
	return s
}

// blankPrefixes replaces each line's comment decoration with spaces.
//
// The width is deliberately unchanged: blanking moves nothing, so the dedent
// that follows is the only step that has to account for a column, and a
// language without decoration behaves exactly as it did before.
func (qe *QueryEngine) blankPrefixes(s string) string {
	if qe.lang.Prefix == nil {
		return s
	}

	lines := strings.Split(s, "\n")
	for i, line := range lines {
		loc := qe.lang.Prefix.FindStringSubmatchIndex(line)
		if loc == nil {
			continue
		}

		// Group 1 is the decoration itself. The pattern looks past it to
		// decide whether it is decoration at all -- `*` followed by a space
		// starts a line, `*` followed by a letter starts emphasis -- but only
		// the group is blanked, so the character that settled the question is
		// left where it was.
		lo, hi := loc[0], loc[1]
		if len(loc) > 3 && loc[2] >= 0 {
			lo, hi = loc[2], loc[3]
		}

		lines[i] = line[:lo] + strings.Repeat(" ", hi-lo) + line[hi:]
	}

	return strings.Join(lines, "\n")
}

// commonIndent returns the length, in bytes, of the longest run of leading
// `cutset` characters shared by every non-blank line. Blank lines (those that
// are empty after trimming) are ignored so they don't force the indent to zero.
func commonIndent(lines []string, cutset string) int {
	common := -1
	for _, line := range lines {
		if strings.TrimLeft(line, cutset) == "" {
			continue
		}
		n := len(line) - len(strings.TrimLeft(line, cutset))
		if common == -1 || n < common {
			common = n
		}
	}
	if common < 0 {
		return 0
	}
	return common
}

// stripIndent removes up to `n` leading `cutset` characters from `line` (a line
// with fewer than `n` leading cutset characters -- e.g. a blank line -- loses
// only what it has).
func stripIndent(line string, n int, cutset string) string {
	if n <= 0 {
		return line
	}
	cut := len(line) - len(strings.TrimLeft(line, cutset))
	if cut > n {
		cut = n
	}
	return line[cut:]
}

// trimQuotes takes the quoting off a string literal, leaving its content.
//
// The quote cannot go in a language's Delims: a quote written inside a comment
// is content, and a pattern wide enough to strip a literal's would strip that
// too. A literal's own node has the quote at each edge and nowhere else, so it
// comes off here.
func trimQuotes(s string) string {
	if len(s) < 2 {
		return s
	}

	open := s[0]
	if open != '"' && open != '`' && open != '\'' {
		return s
	}
	if s[len(s)-1] != open {
		return s
	}

	return s[1 : len(s)-1]
}
