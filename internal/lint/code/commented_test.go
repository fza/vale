package code

import (
	"reflect"
	"strings"
	"testing"
)

// proseLines are comment lines every language has to read as prose: plain
// sentences, sentences that mention code, and markup.
var proseLines = []string{
	"This function returns the user name.",
	"Keep the cache warm for later",
	"see the docs for details",
	"the value is read twice",
	"Returns nil when nothing matches",
	"run it again",
	"Section statutesChanged is gone",
	"a, b and c",
	"Use foo instead of bar.",
	"increments counter",
	"why not?",
	"Deprecated: use Run",
	"first pass",
	"e.g. the same",
	"step 1: build it",
	"x is the width",
	"hello world",
	"not needed",
	"(see below)",
	"Calls foo() first.",
	"Example: 3 items",
	"20% faster",
	"a / b ratio",
	"use --force here",
	"Calls foo(bar) twice",
	"returns x->y",
	"the $var holds it",
	"if x == 0 we stop",
	"see foo::bar for more",
	"Prefer a->b over c",
	"https://example.com/a/b",
	"Copyright (c) 2024 Foo",
	"(optional)",
	"`foo`",
	`"quoted"`,
	"[link]",
	"@deprecated",
	"{@link Foo}",
	"---",
	"<br>",
	"<strong>Note</strong>",
	"<p>Some text here</p>",
	"**bold**",
	"e.g. `foo()`",
	"1. first",
	"* item one",
	"> quoted line",
	"| a | b |",
	"[1]: https://x.y",
	"`.vale.ini`.",
	`[]string{"a", "b", "c"}.`,
	"active | disabled | override",
	"Default: 'bottom' | 'top'",
	`Header: "(n) NodeLabel"`,
	"text <target>",
	"request   <byteLength> LF <bytes>",
	"`natural_width  ≈ N × AVG_STEP_WIDTH`",
	`"<project> v<release> documentation".`,
	"TODO: x = compute(y)",
	"FIXME foo($bar);",
	"}",
	"",
}

// codeLines are, per extension, lines its grammar reads as code.
var codeLines = map[string][]string{
	".php":   {"$this->assertTrue($result, 'Section changed');", "return $value;", "if ($ready) {", "foo($bar);", "$x = 1;", "} else {"},
	".go":    {"x := foo(1)", "if err != nil {", "fmt.Println(x)", "defer f.Close()", "func run() error {", "x++"},
	".js":    {"const x = foo(1);", "return value;", "if (ready) {", "console.log(x)", "x = 1", "<Button onClick={run} />"},
	".jsx":   {"<Button onClick={run} />"},
	".ts":    {"const x: number = foo(1);", "return value;", "console.log(x)"},
	".tsx":   {"<Button onClick={run} />"},
	".py":    {"x = foo(1)", "if ready:", "print(x)", "def run(self):"},
	".rb":    {"x = foo(1)", "puts(x)"},
	".java":  {"int x = foo(1);", "return value;", "System.out.println(x);"},
	".c":     {"int x = foo(1);", "return value;", `printf("%d", x);`},
	".cpp":   {"int x = foo(1);", "std::cout << x;"},
	".cs":    {"var x = Foo(1);", "return value;"},
	".rs":    {"let x = foo(1);", "return value;", `println!("{}", x);`},
	".lua":   {"local x = foo(1)", "print(x)"},
	".hs":    {"x = foo 1", "main = print x"},
	".ex":    {"x = foo(1)", "IO.puts(x)"},
	".jl":    {"x = foo(1)", "println(x)"},
	".kt":    {"val x = foo(1)", "println(x)"},
	".r":     {"x <- foo(1)", "print(x)", "library(dplyr)", "df$col <- 2", "foo(1) -> y", "x <- c(1, 2, 3)", "f <- function(a) {"},
	".yml":   {"key: value", "- name: build"},
	".toml":  {`key = "value"`, "[tool.poetry]"},
	".css":   {"color: red;", "a { color: red; }"},
	".proto": {"string name = 1;"},
	".qml":   {"width = 100"},
}

// TestCommentedCode tests that each grammar reads the code lines as code and
// none of the prose lines as code.
func TestCommentedCode(t *testing.T) {
	for ext, code := range codeLines {
		t.Run(ext, func(t *testing.T) {
			lang, err := GetLanguageFromExt(ext)
			if err != nil {
				t.Fatal(err)
			}
			for _, line := range proseLines {
				if lang.isCode(line) {
					t.Errorf("prose read as code: %q", line)
				}
			}
			for _, line := range code {
				if !lang.isCode(line) {
					t.Errorf("code read as prose: %q", line)
				}
			}
		})
	}
}

// TestCommentedCodeMisses pins lines that are code but read as prose, the
// cost of the gates that keep sentences from reading as code.
// TestCommentedCodeShapes tests the prose a language's own shape or signal
// keeps from reading as code, where the grammar would read it so.
func TestCommentedCodeShapes(t *testing.T) {
	prose := map[string][]string{
		".r": {"x = the width of it"},
	}
	for ext, lines := range prose {
		lang, err := GetLanguageFromExt(ext)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range lines {
			if lang.isCode(line) {
				t.Errorf("%s: prose read as code: %q", ext, line)
			}
		}
	}
}

func TestCommentedCodeMisses(t *testing.T) {
	misses := map[string][]string{
		".go":  {"return nil"},
		".py":  {"return value", "import os"},
		".rb":  {"puts x", "if ready", "def run"},
		".yml": {"- item", "description: a longer value"},
	}
	for ext, lines := range misses {
		lang, err := GetLanguageFromExt(ext)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range lines {
			if lang.isCode(line) {
				t.Errorf("%s: %q read as code", ext, line)
			}
		}
	}
}

// TestCgoPreamble tests that the C code above `import "C"` is hidden whole and
// reported as no run, while a comment above any other import, or one a line
// away from it, is read line by line.
func TestCgoPreamble(t *testing.T) {
	source := []byte(`package p

// #include "parser.h"
// TSLanguage *tree_sitter_x();
import "C"

// x := run(1)
import "fmt"

// y := run(2)

import "C"
`)
	lang, err := GetLanguageFromExt(".go")
	if err != nil {
		t.Fatal(err)
	}
	comments, err := GetComments(source, lang)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 3 {
		t.Fatalf("got %d comments, want 3", len(comments))
	}

	live := []bool{true, false, false}
	for i, c := range comments {
		if c.Live != live[i] {
			t.Errorf("comment %d: Live = %v", i, c.Live)
		}
		hidden, runs := lang.HideCode(c)
		if strings.TrimSpace(hidden.Text) != "" {
			t.Errorf("comment %d: text left: %q", i, hidden.Text)
		}
		if got := len(runs) > 0; got == c.Live {
			t.Errorf("comment %d: runs = %+v", i, runs)
		}
	}
}

func TestHideCode(t *testing.T) {
	lang, err := GetLanguageFromExt(".php")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		text   string
		source string
		doc    string
		want   string
		runs   []CommentedRun
	}{
		{
			name: "example in a comment documenting a declaration",
			text: "Run starts it.\n\n\t$x = run($y);",
			doc:  "Run",
			want: "Run starts it.\n\n" + strings.Repeat(" ", len("\t$x = run($y);")),
		},
		{
			name:   "example in a doc block",
			text:   "Runs it:\n$x = run($y);",
			source: "/**\n * Runs it:\n * $x = run($y);\n */",
			want:   "Runs it:\n" + strings.Repeat(" ", len("$x = run($y);")),
		},
		{
			name:   "plain block",
			text:   "$x = run($y);",
			source: "/*\n $x = run($y);\n*/",
			want:   strings.Repeat(" ", len("$x = run($y);")),
			runs:   []CommentedRun{{First: 1, Last: 1, Column: 1, Text: "$x = run($y);"}},
		},
		{
			name: "prose only",
			text: "Keep the cache warm\nfor later",
			want: "Keep the cache warm\nfor later",
		},
		{
			name: "one line among prose",
			text: "Was:\n  $x = run($y);\nNow gone.",
			want: "Was:\n" + strings.Repeat(" ", len("  $x = run($y);")) + "\nNow gone.",
			runs: []CommentedRun{{First: 2, Last: 2, Column: 3, Text: "$x = run($y);"}},
		},
		{
			name: "empty line bridges, closing brace joins",
			text: "if ($ready) {\n\n    run($x);\n}\nThen prose.",
			want: "             \n\n            \n \nThen prose.",
			runs: []CommentedRun{{First: 1, Last: 4, Column: 1, Text: "if ($ready) {"}},
		},
		{
			name: "line without a word bridges",
			text: "run($x);\n  );\nrun($y);",
			want: "        \n    \n        ",
			runs: []CommentedRun{{First: 1, Last: 3, Column: 1, Text: "run($x);"}},
		},
		{
			name: "trailing empty line stays",
			text: "run($x);\n\nprose here",
			want: "        \n\nprose here",
			runs: []CommentedRun{{First: 1, Last: 1, Column: 1, Text: "run($x);"}},
		},
		{
			name: "prose splits runs",
			text: "run($x);\nthen prose\nrun($y);",
			want: "        \nthen prose\n        ",
			runs: []CommentedRun{
				{First: 1, Last: 1, Column: 1, Text: "run($x);"},
				{First: 3, Last: 3, Column: 1, Text: "run($y);"},
			},
		},
		{
			name: "column counts runes",
			text: "  $x = 1;",
			want: strings.Repeat(" ", len("  $x = 1;")),
			runs: []CommentedRun{{First: 1, Last: 1, Column: 3, Text: "$x = 1;"}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, runs := lang.HideCode(Comment{Text: c.text, Source: c.source, Doc: c.doc})
			if got.Text != c.want {
				t.Errorf("text = %q; want %q", got.Text, c.want)
			}
			if !reflect.DeepEqual(runs, c.runs) {
				t.Errorf("runs = %+v; want %+v", runs, c.runs)
			}
		})
	}
}
