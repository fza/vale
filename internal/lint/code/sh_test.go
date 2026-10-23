package code_test

import (
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/lint/code"
)

// A shell script reaches a parser rather than the prose fallback, which would
// read every string literal as English.
func TestShellIsParsedAsCode(t *testing.T) {
	tests := []struct {
		name string
		ext  string
		want string
	}{
		{"a plain script", ".sh", ".sh"},
		{"a bash script", ".bash", ".sh"},
		{"a zsh script", ".zsh", ".sh"},
		{"a ksh script", ".ksh", ".sh"},
		{"a BeanShell source stays Java", ".bsh", ".java"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := core.GetNormedExt(tt.ext); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}

	lang, err := code.GetLanguageFromExt(".sh")
	if err != nil {
		t.Fatalf("a shell script should reach a parser: %v", err)
	}
	if lang.Parser == nil {
		t.Error("the shell language should carry a grammar")
	}
}

// Only a comment is prose; a string literal is a value the script passes on.
func TestShellReadsCommentsOnly(t *testing.T) {
	src := []byte("#!/bin/sh\necho \"a decision was made here\"\n# a decision was made here\n")

	lang, err := code.GetLanguageFromExt(".sh")
	if err != nil {
		t.Fatal(err)
	}

	comments, err := code.GetComments(src, lang)
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range comments {
		if c.Line == 2 {
			t.Errorf("a string literal should not be read as prose: %q", c.Text)
		}
	}
	if len(comments) == 0 {
		t.Error("the comment lines should be read")
	}
}
