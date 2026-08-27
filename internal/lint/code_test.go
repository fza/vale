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
func TestApplyCommentDirectives(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		want   string
		linted bool
	}{
		{
			name:   "no directive passes through",
			text:   "Intro line.\nSecond line.",
			want:   "Intro line.\nSecond line.",
			linted: true,
		},
		{
			name:   "off and on blank the region and both markers",
			text:   "Intro.\nvale off\n    \"quoted\"\nvale on\nTail.",
			want:   "Intro.\n\n\n\nTail.",
			linted: true,
		},
		{
			name:   "a per-check directive leaves the prose alone",
			text:   "Intro.\nvale House.Passive = NO\nStill linted.",
			want:   "Intro.\n\nStill linted.",
			linted: true,
		},
		{
			name:   "a comment holding only a directive is not linted",
			text:   "vale off",
			want:   "",
			linted: false,
		},
		{
			name:   "an off region with no prose left is not linted",
			text:   "vale off\n    \"quoted\"\nvale on",
			want:   "\n\n",
			linted: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &core.File{Comments: make(map[string]bool)}

			got, linted := applyCommentDirectives(f, tt.text)
			if got != tt.want {
				t.Errorf("text should have its suppressed lines blanked: got %q, want %q", got, tt.want)
			}
			if linted != tt.linted {
				t.Errorf("a comment with nothing left should not be linted: got %v", linted)
			}
			if len(strings.Split(got, "\n")) != len(strings.Split(tt.text, "\n")) {
				t.Error("the line count has to survive, or an alert maps onto the wrong source line")
			}
		})
	}
}

// `vale off` with no closing marker carries past the comment it sits in, the
// same way it carries past a paragraph in markup.
func TestApplyCommentDirectivesCarriesAcrossComments(t *testing.T) {
	f := &core.File{Comments: make(map[string]bool)}

	_, _ = applyCommentDirectives(f, "vale off")

	got, linted := applyCommentDirectives(f, "A later comment.")
	if got != "" || linted {
		t.Errorf("an unclosed off should suppress the next comment too: got %q, linted %v", got, linted)
	}

	_, _ = applyCommentDirectives(f, "vale on")

	got, linted = applyCommentDirectives(f, "Linted again.")
	if got != "Linted again." || !linted {
		t.Errorf("on should resume linting: got %q, linted %v", got, linted)
	}
}
