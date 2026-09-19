package check

import (
	"strings"
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

func makeExistence(tokens []string) (*Existence, error) {
	def := baseCheck{"tokens": tokens}

	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		return nil, err
	}

	rule, err := NewExistence(cfg, def, "")
	if err != nil {
		return nil, err
	}

	return &rule, nil
}

func TestExistence(t *testing.T) {
	rule, err := makeExistence([]string{"test"})
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	file, err := core.NewFile("", cfg)
	if err != nil {
		t.Fatal(err)
	}

	alerts, _ := rule.Run(nlp.NewBlock("", "This is a test.", ""), file, cfg)
	if len(alerts) != 1 {
		t.Errorf("expected one alert, not %v", alerts)
	}
}

func FuzzExistenceInit(f *testing.F) {
	f.Add("hello")
	f.Fuzz(func(_ *testing.T, s string) {
		_, _ = makeExistence([]string{s})
	})
}

func FuzzExistence(f *testing.F) {
	rule, err := makeExistence([]string{"test"})
	if err != nil {
		f.Fatal(err)
	}

	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		f.Fatal(err)
	}

	file, err := core.NewFile("", cfg)
	if err != nil {
		f.Fatal(err)
	}

	f.Add("hello")
	f.Fuzz(func(_ *testing.T, s string) {
		_, _ = rule.Run(nlp.NewBlock("", s, ""), file, cfg)
	})
}

func makeExistenceWithExceptions(tokens, exceptions []string) (*Existence, error) {
	def := baseCheck{"tokens": tokens, "exceptions": exceptions, "ignorecase": true}

	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		return nil, err
	}

	rule, err := NewExistence(cfg, def, "")
	if err != nil {
		return nil, err
	}

	return &rule, nil
}

// TestExceptionPhraseSpansAnyWhitespace covers a phrase whose words a writer
// separated by something other than one space. Wrapping a line between the two
// words of an accepted phrase does not change what the phrase says, so the
// component word is not a finding there either.
func TestExceptionPhraseSpansAnyWhitespace(t *testing.T) {
	rule, err := makeExistenceWithExceptions([]string{"environments?"}, []string{"process environment"})
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	file, err := core.NewFile("", cfg)
	if err != nil {
		t.Fatal(err)
	}

	accepted := []struct {
		name string
		text string
	}{
		{name: "one space", text: "It reads its process environment first."},
		{name: "wrapped line", text: "It reads its process\nenvironment first."},
		{name: "two spaces", text: "It reads its process  environment first."},
		{name: "tab", text: "It reads its process\tenvironment first."},
	}

	for _, tt := range accepted {
		t.Run(tt.name, func(t *testing.T) {
			alerts, _ := rule.Run(nlp.NewBlock("", tt.text, ""), file, cfg)
			if len(alerts) != 0 {
				t.Errorf("an accepted phrase should raise nothing whatever separates its words, got %v", alerts)
			}
		})
	}

	// The exception must not swallow the word it qualifies: an unqualified
	// occurrence is the finding the rule exists for.
	alerts, _ := rule.Run(nlp.NewBlock("", "It reads its environment first.", ""), file, cfg)
	if len(alerts) != 1 {
		t.Errorf("an unqualified occurrence should still raise one alert, got %v", alerts)
	}
}

// A raw pattern is joined into a printf template; a `%` in it must survive.
func TestExistenceRawPercent(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	file, err := core.NewFile("", cfg)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		raw  string
		want int
	}{
		{"%", 2},
		{"50%", 1},
		{"%s", 1},
		{"%[sd]", 1},
	}

	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			def := baseCheck{"raw": []string{c.raw}, "nonword": true}
			rule, rerr := NewExistence(cfg, def, "")
			if rerr != nil {
				t.Fatal(rerr)
			}

			blk := nlp.NewBlock("", "Save %s files. A 50% discount.", "")
			alerts, aerr := rule.Run(blk, file, cfg)
			if aerr != nil {
				t.Fatal(aerr)
			}
			if len(alerts) != c.want {
				t.Errorf("%q: got %d alerts, want %d", c.raw, len(alerts), c.want)
			}
		})
	}
}

// Every match in a block is anchored at its own byte offset, however many
// there are and whatever precedes it.
func TestExistenceManyMatches(t *testing.T) {
	rule, err := makeExistence([]string{"simply"})
	if err != nil {
		t.Fatal(err)
	}

	for _, prefix := range []string{"", "Ünd so: "} {
		unit := "This is simply a test. "
		blk := nlp.NewBlock("", prefix+strings.Repeat(unit, 3000), "text")

		alerts, rerr := rule.Run(blk, &core.File{}, &core.Config{})
		if rerr != nil {
			t.Fatal(rerr)
		}
		if len(alerts) != 3000 {
			t.Fatalf("%q: got %d alerts, want 3000", prefix, len(alerts))
		}
		for i, a := range alerts {
			lo := len(prefix) + i*len(unit) + len("This is ")
			if !a.HasByteOffsets || a.Span[0] != lo || a.Span[1] != lo+len("simply") {
				t.Fatalf("%q: alert %d at %v (anchored: %v), want [%d %d]",
					prefix, i, a.Span, a.HasByteOffsets, lo, lo+len("simply"))
			}
			if blk.Text[a.Span[0]:a.Span[1]] != "simply" {
				t.Fatalf("%q: alert %d points at %q", prefix, i, blk.Text[a.Span[0]:a.Span[1]])
			}
		}
	}
}
