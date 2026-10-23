package lint

import (
	"html"
	"regexp"
	"strings"
)

// A manuscript in plain text marks its chapters by convention: a line of its
// own reading "Chapter One", "Prologue", or "Part II", or a bare number. Read
// as a document, those lines are headings and what follows each is its
// section, so a rule scoped to `heading` or a `doc(...)` selection reaches
// a `.txt` manuscript as it does a Markdown one. A file with no such line
// is read as it always was: one block of prose.

// chapterLine is a heading by name: the word, then at most a short title.
var chapterLine = regexp.MustCompile(`(?i)^(?:chapter|prologue|epilogue|interlude|afterword|foreword|preface)\b[^.!?]{0,60}$`)

// partLine opens a division above the chapter.
var partLine = regexp.MustCompile(`(?i)^(?:part|book|act|volume)\b[^.!?]{0,60}$`)

// numberLine is a chapter by number alone: a numeral or a roman numeral.
var numberLine = regexp.MustCompile(`^(?:\d{1,3}|[IVXLC]{1,7})\.?$`)

// breakLine is a scene break: asterisks, hashes, or dashes, spaced or not.
var breakLine = regexp.MustCompile(`^(?:[*#~-]\s*){3,}$`)

// textToHTML renders plain text as a document when it has chapters, and
// reports whether it does. Paragraphs are runs of lines between blank
// lines. The title is the first paragraph when it is a single short line
// followed by a chapter, so the paragraph after it is the opening.
func textToHTML(content string) (string, bool) {
	paras := strings.Split(content, "\n\n")

	chapters := 0
	for _, p := range paras {
		if line, ok := oneLine(p); ok && (chapterLine.MatchString(line) || numberLine.MatchString(line)) {
			chapters++
		}
	}
	if chapters == 0 {
		return "", false
	}

	var b strings.Builder
	for i, p := range paras {
		text := strings.TrimSpace(p)
		if text == "" {
			continue
		}
		line, single := oneLine(p)
		switch {
		case single && partLine.MatchString(line):
			b.WriteString("<h1>" + html.EscapeString(line) + "</h1>\n")
		case single && (chapterLine.MatchString(line) || numberLine.MatchString(line)):
			b.WriteString("<h2>" + html.EscapeString(line) + "</h2>\n")
		case single && breakLine.MatchString(line):
			b.WriteString("<hr>\n")
		case single && i == 0 && len(strings.Fields(line)) <= 12 && isChapterNext(paras):
			b.WriteString("<h1>" + html.EscapeString(line) + "</h1>\n")
		default:
			b.WriteString("<p>" + html.EscapeString(text) + "</p>\n")
		}
	}
	return b.String(), true
}

// oneLine returns a paragraph's text when it is a single line.
func oneLine(p string) (string, bool) {
	text := strings.TrimSpace(p)
	if text == "" || strings.Contains(text, "\n") {
		return "", false
	}
	return text, true
}

// isChapterNext reports whether the paragraph after the first is a chapter
// heading, which is what makes the first a title.
func isChapterNext(paras []string) bool {
	for _, p := range paras[1:] {
		if strings.TrimSpace(p) == "" {
			continue
		}
		line, ok := oneLine(p)
		return ok && (chapterLine.MatchString(line) || numberLine.MatchString(line) || partLine.MatchString(line))
	}
	return false
}
