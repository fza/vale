package nlp

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Quotations are prose no format marks up: to Markdown, a line of dialogue
// is characters in a paragraph. They are paired here by their marks, the way
// a reader pairs them, for the `quote` scope and the `quote_words` metric.
// The marks stay inside the quotation.
//
// An opening mark with no closing mark runs to the end of its paragraph,
// which is the convention for speech that continues into the next one.

// quoteClosers maps an opening quotation mark to the mark that closes it.
var quoteClosers = map[rune]rune{'“': '”', '"': '"', '‘': '’'}

// OpeningQuote finds the first opening mark in s at or after from. It
// returns where the mark begins, its width, and the mark that closes it,
// or -1.
//
// A straight mark opens only after a space, a bracket, or a dash, since
// the same character closes and doubles as an inch sign.
func OpeningQuote(s string, from int) (int, int, rune) {
	for i := from; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if closer, ok := quoteClosers[r]; ok && opensQuote(s, i, r) {
			return i, size, closer
		}
		i += size
	}
	return -1, 0, 0
}

func opensQuote(s string, at int, mark rune) bool {
	if at == 0 {
		return true
	}
	prev, _ := utf8.DecodeLastRuneInString(s[:at])
	if mark == '"' {
		return unicode.IsSpace(prev) || strings.ContainsRune("([{—–-/", prev)
	}
	return !unicode.IsLetter(prev) && !unicode.IsDigit(prev)
}

// ClosingQuote finds the mark that closes a quotation in s at or after
// from, returning the index just past it, or -1.
//
// A curly single mark is also an apostrophe: it closes only when no letter
// follows it, so "don’t" and "Kenny’s" stay inside the quotation.
func ClosingQuote(s string, closer rune, from int) int {
	for i := from; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == closer {
			next, _ := utf8.DecodeRuneInString(s[i+size:])
			if closer != '’' || !unicode.IsLetter(next) {
				return i + size
			}
		}
		i += size
	}
	return -1
}

// QuoteSpans returns the byte ranges of the quotations in plain text, marks
// included. Pairing runs paragraph by paragraph.
func QuoteSpans(s string) [][2]int {
	var spans [][2]int
	i := 0
	for i < len(s) {
		at, size, closer := OpeningQuote(s, i)
		if at < 0 {
			break
		}
		stop := len(s)
		if j := strings.Index(s[at:], "\n\n"); j >= 0 {
			stop = at + j
		}
		end := ClosingQuote(s[:stop], closer, at+size)
		if end < 0 {
			end = stop
		}
		spans = append(spans, [2]int{at, end})
		i = end
	}
	return spans
}

// QuotedWords counts the words inside the quotations of s.
func QuotedWords(s string) int {
	n := 0
	for _, span := range QuoteSpans(s) {
		n += len(strings.Fields(s[span[0]:span[1]]))
	}
	return n
}
