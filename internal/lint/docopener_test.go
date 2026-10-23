package lint

import (
	"strings"
	"testing"

	"github.com/vale-cli/vale/v3/internal/lint/code"
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
			name:    "a run of names the block below declares",
			content: "// Stdin / Stdout / Stderr may each be nil.\nStdin  io.Reader\nStdout io.Writer\nStderr io.Writer\n",
			comment: code.Comment{Text: "Stdin / Stdout / Stderr may each be nil.", Source: "// Stdin / Stdout / Stderr may each be nil.", Line: 1},
			want:    "      /        /        may each be nil.",
		},
		{
			name:    "a run whose later name nothing declares keeps that name",
			content: "// Stdin / Stdround may each be nil.\nStdin io.Reader\n",
			comment: code.Comment{Text: "Stdin / Stdround may each be nil.", Source: "// Stdin / Stdround may each be nil.", Line: 1},
			want:    "      / Stdround may each be nil.",
		},
		{
			name:    "a run whose first name nothing declares masks nothing",
			content: "// Stdon / Stdout may each be nil.\nStdout io.Writer\n",
			comment: code.Comment{Text: "Stdon / Stdout may each be nil.", Source: "// Stdon / Stdout may each be nil.", Line: 1},
			want:    "Stdon / Stdout may each be nil.",
		},
		{
			name:    "a run of interface methods on their own lines",
			content: "// DBDump / DBPull streams it down.\nDBDump(w io.Writer) error\nDBPull(w io.Writer) error\n",
			comment: code.Comment{Text: "DBDump / DBPull streams it down.", Source: "// DBDump / DBPull streams it down.", Line: 1},
			want:    "       /        streams it down.",
		},
		{
			name:    "a slashed run after the opening stays prose",
			content: "// Stdin carries and/or holds it.\nStdin io.Reader\n",
			comment: code.Comment{Text: "Stdin carries and/or holds it.", Source: "// Stdin carries and/or holds it.", Line: 1},
			want:    "      carries and/or holds it.",
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

// A package comment opens with the word `Package` and then the name, and Go
// asks for it bare there too.
func TestMaskPackageOpener(t *testing.T) {
	tests := []struct {
		name    string
		content string
		comment code.Comment
		want    string
	}{
		{
			name:    "the package clause below names it",
			content: "// Package engine implements the backend.\npackage engine\n",
			comment: code.Comment{Text: "Package engine implements the backend.", Source: "// Package engine implements the backend.", Line: 1},
			want:    "Package        implements the backend.",
		},
		{
			name:    "a different name still reports",
			content: "// Package motor implements the backend.\npackage engine\n",
			comment: code.Comment{Text: "Package motor implements the backend.", Source: "// Package motor implements the backend.", Line: 1},
			want:    "Package motor implements the backend.",
		},
		{
			name:    "prose opening with the word is left alone",
			content: "// Package layout follows one rule.\ntype Registry struct{}\n",
			comment: code.Comment{Text: "Package layout follows one rule.", Source: "// Package layout follows one rule.", Line: 1},
			want:    "Package layout follows one rule.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maskDocOpener(strings.Split(tt.content, "\n"), tt.comment)
			if got != tt.want {
				t.Errorf("the package name should be blanked only when the clause names it: got %q, want %q", got, tt.want)
			}
			if len(got) != len(tt.comment.Text) {
				t.Error("byte length has to survive, or an alert maps onto the wrong column")
			}
		})
	}
}
