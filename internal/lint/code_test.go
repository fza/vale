package lint

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vale-cli/vale/v3/internal/core"
)

// Both readers of a source file consult skipsComment, so a scope excluded from
// one is excluded from the other. Mapping a markup format onto a file switches
// which reader runs, and used to switch this off with it. See #858.
func TestSkipsComment(t *testing.T) {
	tests := []struct {
		name    string
		ignored []string
		scope   string
		want    bool
	}{
		{"nothing ignored", nil, "text.comment.block", false},
		{"the scope itself", []string{"text.comment.block"}, "text.comment.block", true},
		{"every comment", []string{"comment"}, "text.comment.block", true},
		{"a different scope", []string{"text.comment.line"}, "text.comment.block", false},
		{"alongside the defaults", []string{"code", "tt", "text.comment.block"},
			"text.comment.block", true},
	}

	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}
	linter, err := NewLinter(cfg)
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			linter.Manager.Config.IgnoredScopes = tt.ignored
			if got := linter.skipsComment(tt.scope); got != tt.want {
				t.Errorf("skipsComment(%q) = %v with %v, want %v",
					tt.scope, got, tt.ignored, tt.want)
			}
		})
	}
}

// A `vale` directive inside a source comment used to reach nothing: only the
// markup reader called UpdateComments, so a marker in C, Swift or any other
// commented language was linted as prose and suppressed nothing.
//
// A comment is handed to lint in runs, one per stretch sharing a toggle state,
// which is what lets a rule-specific directive suppress that rule alone.
func TestEachDirectiveRun(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "no directive passes through whole",
			text: "Intro line.\nSecond line.",
			want: []string{"Intro line.\nSecond line."},
		},
		{
			name: "off and on blank the region and both markers",
			text: "Intro.\nvale off\n    \"quoted\"\nvale on\nTail.",
			want: []string{"Intro.\n\n\n\n", "\n\n\n\nTail."},
		},
		{
			name: "a tag opens and closes the same region",
			text: "Intro.\n<vale off>\n    \"quoted\"\n</vale off>\nTail.",
			want: []string{"Intro.\n\n\n\n", "\n\n\n\nTail."},
		},
		{
			name: "a per-check directive splits the runs",
			text: "Intro.\nvale House.Passive = NO\nStill linted.",
			want: []string{"Intro.\n\n", "\n\nStill linted."},
		},
		{
			name: "a comment holding only a directive is not linted",
			text: "vale off",
			want: nil,
		},
		{
			name: "an off region with no prose left is not linted",
			text: "vale off\n    \"quoted\"\nvale on",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &core.File{Comments: make(map[string]bool)}

			var got []string
			err := eachDirectiveRun(f, tt.text, func(run string) error {
				got = append(got, run)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}

			if len(got) != len(tt.want) {
				t.Fatalf("runs should split at each directive: got %q, want %q", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("a run should blank what it excludes: got %q, want %q", got[i], tt.want[i])
				}
				if len(strings.Split(got[i], "\n")) != len(strings.Split(tt.text, "\n")) {
					t.Error("the line count has to survive, or an alert maps onto the wrong source line")
				}
			}
		})
	}
}

// A per-check directive suppresses that check alone, so the run beneath it is
// still handed to lint rather than blanked the way an `off` region is.
func TestEachDirectiveRunKeepsPerCheckProse(t *testing.T) {
	f := &core.File{Comments: make(map[string]bool)}

	var runs []string
	err := eachDirectiveRun(f, "vale House.Passive = NO\nStill linted.", func(run string) error {
		runs = append(runs, run)
		if !f.QueryComments("House.Passive") {
			t.Error("the run should be linted with that check suppressed")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(runs) != 1 {
		t.Fatalf("the prose under the directive should still be linted: got %q", runs)
	}
}

// `vale off` with no closing marker carries past the comment it sits in, the
// same way it carries past a paragraph in markup.
func TestEachDirectiveRunCarriesAcrossComments(t *testing.T) {
	f := &core.File{Comments: make(map[string]bool)}

	collect := func(text string) []string {
		var runs []string
		err := eachDirectiveRun(f, text, func(run string) error {
			runs = append(runs, run)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return runs
	}

	collect("vale off")

	if runs := collect("A later comment."); runs != nil {
		t.Errorf("an unclosed off should suppress the next comment too: got %q", runs)
	}

	collect("vale on")

	runs := collect("Linted again.")
	if len(runs) != 1 || runs[0] != "Linted again." {
		t.Errorf("on should resume linting: got %q", runs)
	}
}

// A code comment carries no markup, so a prose rule used to read the notation it
// names -- an identifier, a flag, a path -- as ordinary English. Backticks mark
// that notation, and masking the span makes a comment read the way Markdown does.
func TestMaskCode(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"no backticks passes through", "Plain prose here.", "Plain prose here."},
		{"a span becomes a mask of its own width", "See `worktreePath` first.", "See ************** first."},
		{"a span of several bytes per character keeps its character count", "Keep `naïve` here.", "Keep ******* here."},
		{"two spans on one line", "`a` and `bb`.", "*** and ****."},
		{"an unclosed backtick swallows nothing", "A ` lone tick.", "A ` lone tick."},
		{"a span never crosses a line", "open ` here\nand ` there", "open ` here\nand ` there"},
		{
			"a fence is masked and keeps its line breaks",
			"Shape:\n\n```\nfoo(bar)\n```\n\nDone.",
			"Shape:\n\n***\n********\n***\n\nDone.",
		},
		{
			"a span inside a fence is masked once",
			"```\nsee `x` here\n```",
			"***\n************\n***",
		},
		{
			"an unclosed fence swallows nothing",
			"```\nfoo(bar)\nstill prose.",
			"```\nfoo(bar)\nstill prose.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maskCode(tt.text)
			if got != tt.want {
				t.Errorf("notation should be masked in place: got %q, want %q", got, tt.want)
			}
			if utf8.RuneCountInString(got) != utf8.RuneCountInString(tt.text) {
				t.Errorf("character count has to survive, or an alert maps onto the wrong column")
			}
		})
	}
}

// The format a file is read as supplies the grammar for an extension that has
// none of its own; without one, such a file is linted by the regex fallback,
// which has no JavaScript entry and so reads no comments at all.
func TestLanguageForMappedExtension(t *testing.T) {
	tests := []struct {
		name    string
		formats map[string]string
		ext     string
		want    bool
	}{
		{"own grammar", nil, ".js", true},
		{"unmapped, no grammar", nil, ".es", false},
		{"mapped to a grammar", map[string]string{"es": "js"}, ".es", true},
		{"mapped to a format without one", map[string]string{"es": "md"}, ".es", false},
		{"mapped by a glob", map[string]string{"*.es": "js"}, ".es", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normed, _ := core.FormatFromExt("test"+tt.ext, tt.formats)
			lang, langErr := (&Linter{}).languageFor(&core.File{RealExt: tt.ext, NormedExt: normed})
			if got := langErr == nil && lang != nil; got != tt.want {
				t.Errorf("languageFor(%q) with %v found a grammar = %v, want %v",
					tt.ext, tt.formats, got, tt.want)
			}
		})
	}
}

// A URL in a code comment is never prose. Masking it the way Markdown masks
// inline code keeps a rule from reading it, and from joining the words on
// either side of it, while every column after it still lands.
func TestMaskURLs(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"no URL passes through", "Plain prose here.", "Plain prose here."},
		{"a URL becomes a mask of its own width", "See https://example.com/x for more.", "See ********************* for more."},
		{"the punctuation closing a sentence stays", "Read https://example.com/x.", "Read *********************."},
		{"a URL of several bytes per character keeps its character count", "See https://example.com/café here.", "See ************************ here."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maskURLs(tt.text)
			if got != tt.want {
				t.Errorf("a URL should be masked in place: got %q, want %q", got, tt.want)
			}
			if utf8.RuneCountInString(got) != utf8.RuneCountInString(tt.text) {
				t.Error("character count has to survive, or an alert maps onto the wrong column")
			}
		})
	}
}
