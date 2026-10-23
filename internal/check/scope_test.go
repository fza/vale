package check

import (
	"fmt"
	"strings"
	"testing"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

func TestSelectors(t *testing.T) {
	s1 := Selector{Value: []string{"text.comment.line.py"}}
	s2 := Selector{Value: []string{"text.comment"}}
	// s3 := Selector{Value: "text.comment.line.rb"}

	sec := []string{"text", "comment", "line", "py"}
	if !core.AllStringsInSlice(sec, s1.Sections()) {
		t.Errorf("expected = %v, got = %v", sec, s1.Sections())
	}

	if s2.Has("py") {
		t.Errorf("expected `false`, got `true`")
	}

	for _, part := range s1.Sections() {
		if !s1.Has(part) {
			t.Errorf("expected `true`, got `false`")
		}
	}
}

// TestScopeMatches pins which blocks a rule's declared scope reaches.
//
// Matching is by containment: a rule matches a block when every section of the
// rule's scope appears somewhere in the block's. The block scopes below are the
// ones the linter actually builds -- `text.<element>` from the parser,
// `paragraph.<scope>` from splitting, `sentence.<scope>` from segmentation --
// so each row is one cell of the scope/element matrix. See #1124 and #1132.
func TestScopeMatches(t *testing.T) {
	cases := []struct {
		rule  []string
		block string
		want  bool
	}{
		// `paragraph` names splitting's wrapper, carried only by body text.
		{[]string{"paragraph"}, "paragraph.text.md", true},
		{[]string{"paragraph"}, "text.md", false},
		{[]string{"paragraph"}, "text.heading.h2.md", false},
		{[]string{"paragraph"}, "text.table.cell.md", false},
		{[]string{"paragraph"}, "text.list.md", false},
		{[]string{"paragraph"}, "text.blockquote.md", false},
		{[]string{"paragraph"}, "sentence.text.md", false},

		// `sentence` names segmentation's wrapper, carried by every kind of
		// prose -- which is what #1124 fixed.
		{[]string{"sentence"}, "sentence.text.md", true},
		{[]string{"sentence"}, "sentence.text.heading.h2.md", true},
		{[]string{"sentence"}, "sentence.text.list.md", true},
		{[]string{"sentence"}, "text.md", false},
		{[]string{"sentence"}, "paragraph.text.md", false},

		// Element scopes reach their element, and no other. They do not reach
		// the element's sentence fragments: the full block carries every
		// section a fragment does, so a rule that doesn't ask for `sentence`
		// loses nothing -- and running it per fragment reported `a.` as a
		// whole heading (#1150).
		{[]string{"heading"}, "text.heading.h2.md", true},
		{[]string{"heading"}, "sentence.text.heading.h2.md", false},
		{[]string{"text"}, "sentence.text.md", false},
		{[]string{"heading.h2"}, "text.heading.h2.md", true},
		{[]string{"heading.h3"}, "text.heading.h2.md", false},
		{[]string{"heading"}, "text.md", false},
		{[]string{"table.header"}, "text.table.header.md", true},
		{[]string{"table.header"}, "text.table.cell.md", false},
		{[]string{"table.cell"}, "text.table.cell.md", true},
		{[]string{"table"}, "text.table.header.md", true},
		{[]string{"table"}, "text.table.cell.md", true},
		{[]string{"list"}, "text.list.md", true},
		{[]string{"list"}, "text.md", false},
		{[]string{"blockquote"}, "text.blockquote.md", true},
		{[]string{"blockquote"}, "text.md", false},

		// Inline and raw scopes are siblings of `text`, not children of it:
		// an ordinary rule must not run a second time over each fragment.
		{[]string{"link"}, "link.md", true},
		{[]string{"link"}, "text.md", false},
		{[]string{"code"}, "code.md", true},
		{[]string{"raw"}, "raw.md", true},
		{[]string{"raw"}, "text.md", false},
		{[]string{"text"}, "link.md", false},
		{[]string{"text"}, "raw.md", false},

		// Negation excludes an element and keeps everything else.
		{[]string{"~blockquote"}, "text.md", true},
		{[]string{"~blockquote"}, "text.blockquote.md", false},
		{[]string{"~blockquote & ~heading"}, "text.md", true},
		{[]string{"~blockquote & ~heading"}, "text.heading.h2.md", false},
		{[]string{"~blockquote & ~heading"}, "text.blockquote.md", false},

		// Several scopes are a union.
		{[]string{"heading", "list"}, "text.list.md", true},
		{[]string{"heading", "list"}, "text.blockquote.md", false},
	}

	for _, c := range cases {
		name := fmt.Sprintf("%v vs %s", c.rule, c.block)
		t.Run(name, func(t *testing.T) {
			blk := nlp.NewLinedBlock("", "text", c.block, 1)
			if got := NewScope(c.rule).Matches(blk); got != c.want {
				t.Errorf("NewScope(%v).Matches(%q) = %v, want %v",
					c.rule, c.block, got, c.want)
			}
		})
	}
}

// A negated inline scope is recorded so a rule can leave that element's
// text out, and the rule still runs on the block that holds the element.
func TestScopeExcluded(t *testing.T) {
	s := NewScope([]string{"~heading & ~link"})
	if len(s.Excluded) != 1 || s.Excluded[0] != "link" {
		t.Errorf("Excluded = %v, want [link]", s.Excluded)
	}
	if !s.Matches(nlp.Block{Scope: "text.md"}) {
		t.Error("~heading & ~link should reach a paragraph")
	}
	if s.Matches(nlp.Block{Scope: "text.heading.h2.md"}) {
		t.Error("~heading & ~link should not reach a heading")
	}
	if s.Matches(nlp.Block{Scope: "link.md"}) {
		t.Error("~heading & ~link should not reach a link fragment")
	}
	if got := NewScope([]string{"~heading"}).Excluded; len(got) != 0 {
		t.Errorf("~heading excludes no inline scope, got %v", got)
	}
	if got := NewScope([]string{"~quote"}).Excluded; len(got) != 1 || got[0] != "quote" {
		t.Errorf("~quote should exclude the quote scope, got %v", got)
	}
}

// A named scope stands in for its expression wherever a term could go: alone,
// negated, or in a chain. A chain has no opposite, so negating one is refused.
func TestExpandScopes(t *testing.T) {
	names := map[string]string{
		"methods": `doc(section:has(> h2:contains("Methods")))`,
		"lead":    "text & doc(h1 + p)",
	}

	for _, tt := range []struct {
		name  string
		scope interface{}
		want  []string
	}{
		{"none", nil, nil},
		{"unnamed string", "heading", []string{"heading"}},
		{"unnamed list", []interface{}{"heading", "~list"}, []string{"heading", "~list"}},
		{"alone", "methods", []string{`doc(section:has(> h2:contains("Methods")))`}},
		{"negated", "~methods", []string{`~doc(section:has(> h2:contains("Methods")))`}},
		{"in a chain", "sentence & methods", []string{`sentence & doc(section:has(> h2:contains("Methods")))`}},
		{"chain alone", "lead", []string{"text & doc(h1 + p)"}},
		{"chain in a chain", "list & lead", []string{"list & text & doc(h1 + p)"}},
		{"list of names", []string{"methods", "list"}, []string{`doc(section:has(> h2:contains("Methods")))`, "list"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := expandScopes(tt.scope, names)
			if err != nil {
				t.Fatal(err)
			}
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}

	if _, err := expandScopes("~lead", names); err == nil {
		t.Error("negating a chain was accepted")
	}
	if _, err := expandScopes(42, names); err == nil {
		t.Error("a number was accepted as a scope")
	}
}

// The selector syntax a rule may write is Selectors Level 4; what cascadia
// does not parse is spelled in what it does, and what it parses is passed
// through as written.
func TestStandardSelector(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{`h2`, `h2`},
		{`:is(h2, h3)`, `:not(:not(h2, h3))`},
		{`:where(h2, h3)`, `:not(:not(h2, h3))`},
		{`:IS(h2)`, `:not(:not(h2))`},
		{`section:has(> :is(h1, h2):contains("m"))`, `section:has(> :not(:not(h1, h2)):contains("m"))`},
		{`:is(h2:not(:contains("x")), h3) + p`, `:not(:not(h2:not(:contains("x")), h3)) + p`},
		{`h2:contains(":is(")`, `h2:contains(":is(")`},
		{`h2:contains(':is(')`, `h2:contains(':is(')`},
		{`:is(:is(h1, h2), h3)`, `:not(:not(:not(:not(h1, h2)), h3))`},
		{`section:has(> h2, > h3)`, `section:has(> h2, > h3)`},
	} {
		if got := standardSelector(tt.in); got != tt.want {
			t.Errorf("standardSelector(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// The forms the documentation promises compile, and select what they say.
func TestCompileSelectorLevel4(t *testing.T) {
	doc, err := html.Parse(strings.NewReader(`<body>` +
		`<section id="a"><h2>Methods</h2><p>x</p></section>` +
		`<section id="b"><h3>Intro</h3><p>y</p></section>` +
		`<section id="c"><p>z</p><h2>Late</h2></section></body>`))
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		sel  string
		want []string
	}{
		{`section:has(> h2)`, []string{"a", "c"}},
		{`section:has(> h2, > h3)`, []string{"a", "b", "c"}},
		{`section:has(> h2, h3)`, []string{"a", "b", "c"}},
		{`section:has(+ section)`, []string{"a", "b"}},
		{`section:has(~ section)`, []string{"a", "b"}},
		{`section:has(> p + h2)`, []string{"c"}},
		{`section:has(> h2 + p)`, []string{"a"}},
		{`section:haschild(h2, h3)`, []string{"a", "b", "c"}},
		{`section:not(:has(> h2, > h3))`, nil},
		{`section:has(> :is(h2, h3):contains("i"))`, []string{"b"}},
		{`section:is(:has(> h3), :has(> p + h2))`, []string{"b", "c"}},
		{`section:where(:has(> h3), :has(> p + h2))`, []string{"b", "c"}},
		{`section:has(> :is(h2, h3) + p)`, []string{"a", "b"}},
	} {
		m, cErr := compileSelector(tt.sel)
		if cErr != nil {
			t.Errorf("%s: %v", tt.sel, cErr)
			continue
		}

		var got []string
		for _, n := range cascadia.QueryAll(doc, m) {
			for _, a := range n.Attr {
				if a.Key == "id" {
					got = append(got, a.Val)
				}
			}
		}
		if fmt.Sprint(got) != fmt.Sprint(tt.want) {
			t.Errorf("%s selected %v, want %v", tt.sel, got, tt.want)
		}
	}

	for _, sel := range []string{`:is(`, `section:has(>)`, `:is(h2`} {
		if _, cErr := compileSelector(sel); cErr == nil {
			t.Errorf("%q was accepted", sel)
		}
	}
}
