package check

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	rx "github.com/errata-ai/vale/v3/internal/regex"
)

// vocabEntry is one accepted term, compiled when the rule loads.
type vocabEntry struct {
	term string
	// lowered is the term's own text, folded, for the equality test.
	lowered string
	// pattern is nil when the term does not compile. Such a term still
	// participates in the equality test, and never matches as a pattern.
	pattern *rx.Regexp
}

// vocabMatcher reports how the project's vocabulary spells a word.
//
// The capitalization checks hand every accepted term to this type, which for a
// real project is a thousand or more patterns. Compiling those on demand meant
// compiling all of them for every word of every heading; they are compiled
// once here instead, and Vale's own prefilter (regex.Required) rules most of
// them out with a substring test before the engine is asked anything.
type vocabMatcher struct {
	entries []vocabEntry
	// terms holds the same terms in the same order, for callers that need the
	// raw list.
	terms []string
}

func newVocabMatcher(terms []string) *vocabMatcher {
	sorted := make([]string, len(terms))
	copy(sorted, terms)

	// NOTE: This is required to ensure that we have greedy alternation.
	sort.Slice(sorted, func(p, q int) bool {
		return len(sorted[p]) > len(sorted[q])
	})

	matcher := &vocabMatcher{
		entries: make([]vocabEntry, 0, len(sorted)),
		terms:   sorted,
	}

	for _, term := range sorted {
		entry := vocabEntry{term: term, lowered: strings.ToLower(term)}
		if pattern, err := rx.Compile(term); err == nil {
			entry.pattern = pattern
		}
		matcher.entries = append(matcher.entries, entry)
	}

	return matcher
}

func (m *vocabMatcher) empty() bool {
	return m == nil || len(m.entries) == 0
}

// match returns the spelling the vocabulary asks for, or "" when no term
// covers s.
//
// A term that equals s apart from case supplies its own spelling; a term whose
// pattern matches s leaves s as it was written.
func (m *vocabMatcher) match(s string) string {
	if m.empty() {
		return ""
	}

	lowered := strings.ToLower(s)
	for i := range m.entries {
		entry := &m.entries[i]
		if entry.lowered == lowered {
			return entry.term
		}
		if entry.pattern != nil && entry.pattern.MightMatch(lowered) &&
			isMatch(entry.pattern, s) {
			return s
		}
	}

	return ""
}

// candidates returns the terms that could still bear on any word of s, in the
// order match would consider them.
//
// It exists for converters that own their own vocabulary loop: they are given
// the few terms the prefilter cannot rule out rather than the whole list. The
// test is deliberately loose -- a term survives whenever the prefilter is
// inconclusive, and whenever s contains the term's own text, which is what the
// equality test needs.
func (m *vocabMatcher) candidates(s string) []string {
	if m.empty() {
		return nil
	}

	lowered := strings.ToLower(s)

	out := []string{}
	for i := range m.entries {
		entry := &m.entries[i]
		if entry.pattern == nil || entry.pattern.MightMatch(lowered) ||
			strings.Contains(lowered, entry.lowered) {
			out = append(out, entry.term)
		}
	}

	return out
}

// indicatorFunc decides whether the word after word should be capitalized.
type indicatorFunc func(word string, idx int) bool

// sentenceConverter rewrites a string in sentence case.
//
// The tokenizer has to know the vocabulary, because a term may span what would
// otherwise be several tokens, so the pattern is built from the vocabulary and
// compiled once when the rule loads rather than once per block.
type sentenceConverter struct {
	matcher   *vocabMatcher
	tokenRe   *rx.Regexp
	prefixRe  *regexp.Regexp
	indicator indicatorFunc
}

func newSentenceConverter(terms []string, prefix string, indicator indicatorFunc) (*sentenceConverter, error) {
	matcher := newVocabMatcher(terms)

	pattern := `[\p{N}\p{L}*]+[^\s]*`
	if !matcher.empty() {
		pattern = fmt.Sprintf(`\b(?:%s)\b|%s`,
			strings.Join(matcher.terms, "|"), pattern)
	}

	tokenRe, err := rx.Compile(`(?i)` + pattern)
	if err != nil {
		return nil, err
	}

	converter := &sentenceConverter{
		matcher:   matcher,
		tokenRe:   tokenRe,
		indicator: indicator,
	}

	if prefix != "" {
		prefixRe, perr := regexp.Compile(prefix)
		if perr != nil {
			return nil, perr
		}
		converter.prefixRe = prefixRe
	}

	return converter, nil
}

// Convert returns a copy of s in sentence case.
func (sc *sentenceConverter) Convert(s string) string {
	prefix := ""
	if sc.prefixRe != nil && sc.prefixRe.MatchString(s) {
		prefix = sc.prefixRe.FindString(s)
		s = strings.TrimPrefix(s, prefix)
	}

	// Tokenize before lowering anything, so vocabulary entries keep the case
	// they were written with.
	tokens := tokenize(sc.tokenRe, s)

	made := make([]string, 0, len(tokens))
	for i, token := range tokens {
		prev := ""
		if i-1 >= 0 {
			prev = tokens[i-1]
		}

		if entry := sc.matcher.match(token); entry != "" {
			made = append(made, entry)
		} else if i == 0 || sc.indicator(prev, i-1) {
			made = append(made, toTitle(token))
		} else {
			made = append(made, strings.ToLower(token))
		}
	}

	return prefix + strings.Join(made, " ")
}

// tokenize returns every match of re in s.
//
// A match error ends the scan and keeps what was found, rather than panicking
// as the shared helper does: a vocabulary entry is user-supplied, and one
// pathological pattern should not abort a conversion.
func tokenize(re *rx.Regexp, s string) []string {
	var out []string

	m, err := re.FindStringMatch(s)
	for err == nil && m != nil {
		out = append(out, m.String())
		m, err = re.FindNextMatch(m)
	}

	return out
}

// toTitle upper-cases the first rune of s and lowers the rest.
func toTitle(s string) string {
	if s == "" {
		return s
	}

	first, size := utf8.DecodeRuneInString(s)

	return string(unicode.ToTitle(first)) + strings.ToLower(s[size:])
}
