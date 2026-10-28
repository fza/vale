package code_test

import (
	_ "embed"
	"strings"
	"testing"

	"github.com/vale-cli/vale/v3/internal/lint/code"
)

//go:embed testdata/vue/component.vue
var vueComponent string

//go:embed testdata/vue/external.vue
var vueExternalScript string

func TestVueReadsEveryScriptBlock(t *testing.T) {
	comments := literalComments(t, ".vue", vueComponent)

	cases := []struct {
		name, text, scope string
		line, column      int
	}{
		{"script comment on the tag line", "Opens on the tag line.", "text.comment.line", 9, 21},
		{"script literal", "OrderView", "literal.line", 10, 24},
		{"script setup comment", "Holds the current order.", "text.comment.line", 16, 3},
		{"script setup literal", "Open the order", "literal.line", 17, 19},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			found := mustFind(t, comments, tc.text)

			if found.Scope != tc.scope {
				t.Errorf("the block's own scope should be kept, got %q", found.Scope)
			}
			if found.Line != tc.line {
				t.Errorf("the line should be the component's, got %d", found.Line)
			}
			if column := sourceColumn(found); column != tc.column {
				t.Errorf("the column should be the component's, got %d", column)
			}
		})
	}
}

func TestVueSkipsTemplateAndStyle(t *testing.T) {
	cases := []struct{ name, source string }{
		{"component", vueComponent},
		{"external script", vueExternalScript},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			comments := literalComments(t, ".vue", tc.source)
			if len(comments) == 0 {
				t.Fatal("the script blocks should still be read")
			}

			for _, comment := range comments {
				for _, outside := range []string{"Kommentar", "Bitte", "Kein", "Nicht", "Hides", "footer"} {
					if strings.Contains(comment.Text, outside) {
						t.Errorf("only a script block should be read, got %q", comment.Text)
					}
				}
			}
		})
	}
}

func TestVueLiteralsAreNotExtractedUnasked(t *testing.T) {
	lang, err := code.GetLanguageFromExt(".vue")
	if err != nil {
		t.Fatal(err)
	}

	comments, err := code.GetComments([]byte(vueComponent), lang)
	if err != nil {
		t.Fatal(err)
	}

	if texts := literalTexts(comments); len(texts) > 0 {
		t.Errorf("no literal should be extracted unasked, got %q", texts)
	}
	mustFind(t, comments, "Holds the current order.")
}
