package lint

import (
	"strings"
	"testing"
)

// A plain-text manuscript is read as a document when a line of its own
// names a chapter; everything else stays one block of prose.
func TestTextToHTML(t *testing.T) {
	doc, ok := textToHTML("The Long Way Home\n\nChapter One\n\nIt rained. \"Fine,\" he said.\n\n* * *\n\nHe waited.\n\nPart II\n\n7\n\nThe end & after.\n")
	if !ok {
		t.Fatal("a manuscript with chapters was not read as a document")
	}
	want := "<h1>The Long Way Home</h1>\n<h2>Chapter One</h2>\n<p>It rained. &#34;Fine,&#34; he said.</p>\n<hr>\n<p>He waited.</p>\n<h1>Part II</h1>\n<h2>7</h2>\n<p>The end &amp; after.</p>\n"
	if doc != want {
		t.Errorf("got:\n%s\nwant:\n%s", doc, want)
	}

	for _, text := range []string{
		"Plain prose.\n\nMore prose.\n",
		// A chapter word inside a paragraph is prose, and so is a line that
		// reads as a sentence.
		"Chapter one was long, and the chapter after it longer.\n",
		"Chapter One. It began.\n",
		// A title alone is not a manuscript.
		"The Long Way Home\n\nIt rained.\n",
	} {
		if _, ok = textToHTML(text); ok {
			t.Errorf("%q was read as a document", text)
		}
	}

	// A first line is the title only when a chapter follows it.
	doc, _ = textToHTML("It rained all day.\n\nHe waited.\n\nChapter Two\n\nMore.\n")
	if !strings.HasPrefix(doc, "<p>It rained all day.</p>") {
		t.Errorf("a first paragraph before prose became a title: %s", doc)
	}
}
