package lint

import (
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
