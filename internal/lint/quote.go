package lint

import (
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/vale-cli/vale/v3/internal/nlp"
)

// Quotations are paired by nlp.QuoteSpans and wrapped in a `q` in the
// document view, or carried as runs of a plain-text block.

// quoteSkip names the elements whose text is never a quotation.
var quoteSkip = map[string]bool{
	"head": true, "pre": true, "code": true, "tt": true, "kbd": true,
	"samp": true, "script": true, "style": true, "textarea": true, "q": true,
}

// wrapQuotes encloses each quotation under n in a `q` element.
//
// Pairing is done among an element's own text nodes, so a quotation may
// hold inline markup -- "we *knew*" -- and one that opens in a text node
// and never closes runs to the end of the element. An element that already
// is a quotation, or holds code, is left alone.
func wrapQuotes(n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && !quoteSkip[c.Data] {
			wrapQuotes(c)
		}
	}

	c := n.FirstChild
	for c != nil {
		if c.Type != html.TextNode {
			c = c.NextSibling
			continue
		}
		at, size, closer := nlp.OpeningQuote(c.Data, 0)
		if at < 0 {
			c = c.NextSibling
			continue
		}

		// The text before the mark stays where it is.
		rest := c
		if at > 0 {
			rest = &html.Node{Type: html.TextNode, Data: c.Data[at:]}
			c.Data = c.Data[:at]
			n.InsertBefore(rest, c.NextSibling)
		}
		q := &html.Node{Type: html.ElementNode, Data: "q", DataAtom: atom.Q}
		n.InsertBefore(q, rest)

		// Move nodes into the quotation until a text node closes it.
		var after *html.Node
		from := size
		for m := rest; m != nil; {
			next := m.NextSibling
			if m.Type == html.TextNode {
				if end := nlp.ClosingQuote(m.Data, closer, from); end >= 0 {
					if end < len(m.Data) {
						after = &html.Node{Type: html.TextNode, Data: m.Data[end:]}
						m.Data = m.Data[:end]
						n.InsertBefore(after, next)
					} else {
						after = next
					}
					n.RemoveChild(m)
					q.AppendChild(m)
					break
				}
			}
			n.RemoveChild(m)
			q.AppendChild(m)
			m, from = next, 0
		}
		c = after
	}
}
