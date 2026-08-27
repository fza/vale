package lint

import (
	"strings"
	"testing"

	"github.com/errata-ai/vale/v3/internal/lint/code"
)

func TestMaskDocOpener(t *testing.T) {
	tests := []struct {
		name    string
		content string
		comment code.Comment
		want    string
	}{
		{
			name:    "the declaration below names it",
			content: "// stagingDir makes a directory.\nfunc stagingDir() {}\n",
			comment: code.Comment{Text: "stagingDir makes a directory.", Source: "// stagingDir makes a directory.", Line: 1},
			want:    "           makes a directory.",
		},
		{
			name:    "a misspelled opener still reports",
			content: "// stagingDur makes a directory.\nfunc stagingDir() {}\n",
			comment: code.Comment{Text: "stagingDur makes a directory.", Source: "// stagingDur makes a directory.", Line: 1},
			want:    "stagingDur makes a directory.",
		},
		{
			name:    "a type declaration counts",
			content: "// Registry holds every feat.\ntype Registry struct{}\n",
			comment: code.Comment{Text: "Registry holds every feat.", Source: "// Registry holds every feat.", Line: 1},
			want:    "         holds every feat.",
		},
		{
			name:    "a longer name is not a match",
			content: "// Reg holds every feat.\ntype Registry struct{}\n",
			comment: code.Comment{Text: "Reg holds every feat.", Source: "// Reg holds every feat.", Line: 1},
			want:    "Reg holds every feat.",
		},
		{
			name:    "a blank first line is skipped",
			content: "//\n// stagingDir makes a directory.\nfunc stagingDir() {}\n",
			comment: code.Comment{Text: "\nstagingDir makes a directory.", Source: "//\n// stagingDir makes a directory.", Line: 1},
			want:    "\n           makes a directory.",
		},
		{
			name:    "prose opening a comment is left alone",
			content: "// The registry holds every feat.\ntype Registry struct{}\n",
			comment: code.Comment{Text: "The registry holds every feat.", Source: "// The registry holds every feat.", Line: 1},
			want:    "The registry holds every feat.",
		},
		{
			name:    "an annotation label is prose",
			content: "# FIXME: fix this.\ndef FIXME():\n",
			comment: code.Comment{Text: "FIXME: fix this.", Source: "# FIXME: fix this.", Line: 1},
			want:    "FIXME: fix this.",
		},
		{
			name:    "nothing follows the comment",
			content: "// stagingDir makes a directory.\n",
			comment: code.Comment{Text: "stagingDir makes a directory.", Source: "// stagingDir makes a directory.", Line: 1},
			want:    "stagingDir makes a directory.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maskDocOpener(strings.Split(tt.content, "\n"), tt.comment)
			if got != tt.want {
				t.Errorf("the opener should be blanked only when the declaration names it: got %q, want %q", got, tt.want)
			}
			if len(got) != len(tt.comment.Text) {
				t.Error("byte length has to survive, or an alert maps onto the wrong column")
			}
		})
	}
}
