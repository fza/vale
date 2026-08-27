package lint

import (
	"strings"
	"testing"

	"github.com/errata-ai/vale/v3/internal/core"
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
