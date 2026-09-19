package lint

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	grh "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

// Markdown configuration.
var goldMd = goldmark.New(
	goldmark.WithExtensions(
		extension.GFM,
		extension.Footnote,
		// Treat `$$…$$` display math as a skipped block so it isn't
		// spell-checked as prose. See #878 and math.go.
		mathExtension{},
	),
	goldmark.WithRendererOptions(
		grh.WithUnsafe(),
	),
)

// Convert extended info strings -- e.g., ```callout{'title': 'NOTE'} -- that
// might confuse Blackfriday into normal "```".
var reExInfo = regexp.MustCompile("`{3,}" + `.+`)

var reLinkRef = regexp.MustCompile(`\]\[(?:[^]\n]+)\]`)
var reLinkDef = regexp.MustCompile(`\[(?:[^]\n]+)\]:`)

var reNumericList = regexp.MustCompile(`(?m)^\d+\.`)

func (l *Linter) lintMarkdown(f *core.File) error {
	return l.lintMarkdownWith(f, goldMd)
}

// lintMarkdownWith lints f as Markdown read by the given converter -- the
// plain configuration, or a dialect's such as MyST's.
func (l *Linter) lintMarkdownWith(f *core.File, md goldmark.Markdown) error {
	var buf bytes.Buffer

	err := l.lintMetadata(f)
	if err != nil {
		return err
	}

	s, err := l.Transform(f)
	if err != nil {
		return err
	}

	// A directive written as a tag is rewritten into the comment form before
	// parsing, because a closing tag holds no attribute and a Markdown parser
	// escapes it into paragraph text rather than passing it through as markup.
	src := []byte(markTagDirectives(s))
	doc := md.Parser().Parse(text.NewReader(src))
	if err = md.Renderer().Render(&buf, src, doc); err != nil {
		return core.NewE100(f.Path, err)
	}

	if md == goldMdx {
		// The transform rewrites the front matter, so the spans of the
		// parsed text are not the file's; the file is parsed again for them.
		if s != f.Content {
			doc = md.Parser().Parse(text.NewReader([]byte(f.Content)))
		}
		f.Content = maskSpans(f.Content, mdxTagMasks(doc))
	}
	f.Content = prepMarkdown(f.Content)
	return l.lintHTMLTokens(f, buf.Bytes(), 0)
}

// markTagDirectives rewrites a directive written as a tag into the comment form
// Markdown carries through to HTML. A closing tag holds no attribute, so
// `</vale off>` is not valid HTML and a Markdown parser escapes it into
// paragraph text rather than passing it through as markup.
//
// Only a line holding nothing else is rewritten, and never one inside a fence,
// so a document showing a directive keeps showing it. The line survives as a
// line, which is what keeps every later alert on the row it belongs to.
func markTagDirectives(text string) string {
	if !strings.Contains(text, "vale") {
		return text
	}

	lines := strings.Split(text, "\n")
	fence := ""

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if fence != "" {
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fence = trimmed[:3]
			continue
		}

		directive, ok := core.NormalizeDirective(trimmed)
		if !ok {
			continue
		}

		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		lines[i] = indent + "<!-- " + directive + " -->"
	}

	return strings.Join(lines, "\n")
}

func prepMarkdown(content string) string {
	// NOTE: This is required to avoid finding matches inside info strings. For
	// example, if we're looking for 'json' we many incorrectly report the
	// location as being in an infostring like '```json'.
	//
	// See https://github.com/errata-ai/vale/v2/issues/248.
	body := reExInfo.ReplaceAllStringFunc(content, func(m string) string {
		parts := strings.Split(m, "`")

		// This ensures that we respect the number of opening backticks, which
		// could be more than 3.
		//
		// See https://github.com/errata-ai/vale/v2/issues/271.
		tags := strings.Repeat("`", len(parts)-1)
		span := strings.Repeat("*", nlp.StrLen(parts[len(parts)-1]))

		return tags + span
	})

	// NOTE: This is required to avoid finding matches inside link references.
	body = reLinkRef.ReplaceAllStringFunc(body, func(m string) string {
		return "][" + strings.Repeat("*", nlp.StrLen(m)-3) + "]"
	})
	body = reLinkDef.ReplaceAllStringFunc(body, func(m string) string {
		return "[" + strings.Repeat("*", nlp.StrLen(m)-3) + "]:"
	})

	// NOTE: This is required to avoid finding matches inside ordered lists.
	body = reNumericList.ReplaceAllStringFunc(body, func(m string) string {
		return strings.Repeat("*", nlp.StrLen(m))
	})

	return body
}
