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
