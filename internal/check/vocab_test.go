package check

import (
	"fmt"
	"strings"
	"testing"

	rx "github.com/errata-ai/vale/v3/internal/regex"
	"github.com/jdkato/prose/v3/strcase"
)

// projectVocab is a vocabulary shaped like a real one: a term per accepted
// word, each admitting an optional possessive.
func projectVocab(n int) []string {
	words := []string{
		"Addr", "Args", "Argv", "Backend", "Bonjour", "Callee", "Chdir",
		"Classify", "Cobertura", "Daemon", "Endpoint", "Failover", "Gateway",
		"Hostname", "Idempotent", "Kubernetes", "Localhost", "Middleware",
		"Namespace", "Observability", "Preflight", "Quorum", "Runtime",
		"Sidecar", "Throughput", "Upstream", "Volumes", "Workspace",
	}

	out := make([]string, 0, n)
	for i := 0; len(out) < n; i++ {
		out = append(out, fmt.Sprintf("%s%d('s)?", words[i%len(words)], i/len(words)))
	}

	return out
}

var vocabHeadings = []string{
	"Top-level entities",
	"Non-member predicates",
	"Client's key share and finish",
	"Find the thief: Introduction",
	"Creating a connection to Event Store",
	"Using errata-ai/vale",
	"Use the Package Builder to install",
	"1. An important heading",
	"An important Heading",
	"this isn't in sentence case",
	"Community Leader responsibilities",
	"Addr0 and Args0's place in the Backend0",
	"Kubernetes0, Namespace0 and the Gateway0",
	"A heading with no vocabulary in it at all",
	"MIXED case AND Punctuation -- with an em dash",
	"Ünïcödé léading rune",
	"",
}

var vocabSets = [][]string{
	nil,
	{"Event Store"},
	{"errata-ai/vale", "Package Builder", "Community Leader"},
	append(projectVocab(64), "Event Store", "Package Builder", "Community Leader"),
	projectVocab(512),
}

// The Vale-side converter replaces strcase's, so it has to agree with it
// everywhere. strcase is kept here as the oracle rather than as a dependency of
// the check itself.
func TestSentenceConverterMatchesStrcase(t *testing.T) {
	indicator := wasIndicator([]string{":"})

	for i, vocab := range vocabSets {
		ours, err := newSentenceConverter(vocab, "", indicator)
		if err != nil {
			t.Fatalf("vocab set %d: %v", i, err)
		}

		theirs := strcase.NewSentenceConverter(
			strcase.UsingVocab(append([]string{}, vocab...)),
			strcase.UsingIndicator(strcase.IndicatorFunc(indicator)),
		)

		for _, heading := range vocabHeadings {
			want := theirs.Convert(heading)
			if got := ours.Convert(heading); got != want {
				t.Errorf("vocab set %d, %q:\n got = %q\nwant = %q",
					i, heading, got, want)
			}
		}
	}
}

// The prefilter is only allowed to skip work, never to change an answer.
func TestVocabMatcherPrefilterIsSound(t *testing.T) {
	for i, vocab := range vocabSets {
		matcher := newVocabMatcher(vocab)

		for _, heading := range vocabHeadings {
			for _, word := range strings.Fields(heading) {
				want := naiveMatch(matcher, word)
				if got := matcher.match(word); got != want {
					t.Errorf("vocab set %d, %q: got = %q, want = %q",
						i, word, got, want)
				}
			}
		}
	}
}

// naiveMatch is match without the prefilter.
func naiveMatch(m *vocabMatcher, s string) string {
	for i := range m.entries {
		entry := &m.entries[i]
		if strings.EqualFold(entry.term, s) {
			return entry.term
		}
		if entry.pattern != nil && isMatch(entry.pattern, s) {
			return s
		}
	}
	return ""
}

// candidates feeds a converter that runs the vocabulary loop itself, so it has
// to keep every term that could answer for any word of the string.
func TestVocabCandidatesKeepEveryMatch(t *testing.T) {
	for i, vocab := range vocabSets {
		matcher := newVocabMatcher(vocab)

		for _, heading := range vocabHeadings {
			kept := map[string]bool{}
			for _, term := range matcher.candidates(heading) {
				kept[term] = true
			}

			for _, word := range strings.Fields(heading) {
				answer := naiveMatch(matcher, word)
				if answer == "" || answer == word {
					continue
				}
				if !kept[answer] {
					t.Errorf("vocab set %d, %q: dropped %q, which answers %q",
						i, heading, answer, word)
				}
			}
		}
	}
}

// candidates preserves the order match considers terms in, which is what
// decides who answers first when more than one term applies.
func TestVocabCandidatesKeepOrder(t *testing.T) {
	matcher := newVocabMatcher(projectVocab(128))

	position := map[string]int{}
	for i, term := range matcher.terms {
		position[term] = i
	}

	got := matcher.candidates(strings.Join(vocabHeadings, " "))
	for i := 1; i < len(got); i++ {
		if position[got[i-1]] >= position[got[i]] {
			t.Fatalf("out of order at %d: %q then %q", i, got[i-1], got[i])
		}
	}
}

// A term that does not compile is still allowed to answer for a word it equals.
func TestVocabMatcherKeepsUncompilableTerms(t *testing.T) {
	matcher := newVocabMatcher([]string{"a(", "Backend"})

	if _, err := rx.Compile("a("); err == nil {
		t.Fatal("expected 'a(' not to compile")
	}
	if got := matcher.match("A("); got != "a(" {
		t.Errorf("got = %q, want = %q", got, "a(")
	}
	if got := matcher.match("backend"); got != "Backend" {
		t.Errorf("got = %q, want = %q", got, "Backend")
	}
}

func TestToTitle(t *testing.T) {
	cases := map[string]string{
		"":         "",
		"word":     "Word",
		"WORD":     "Word",
		"wORD":     "Word",
		"ünïcödé":  "Ünïcödé",
		"ǳenith":   "ǲenith",
		"1. first": "1. first",
	}

	for in, want := range cases {
		if got := toTitle(in); got != want {
			t.Errorf("toTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
