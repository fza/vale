package testsuite

import (
	"path/filepath"
	"slices"
	"testing"
)

// Rules names what the style loader would load: YAML with an `extends`,
// outside dot and underscore directories, and not a test file.
func TestRules(t *testing.T) {
	root := t.TempDir()
	styles := filepath.Join(root, "styles")

	writeFile(t, filepath.Join(styles, "S", "One.yml"), "extends: existence\ntokens:\n  - a\n")
	writeFile(t, filepath.Join(styles, "S", "sub", "Two.yml"), "extends: existence\ntokens:\n  - b\n")
	writeFile(t, filepath.Join(styles, "S", "_shared", "Frag.yml"), "extends: existence\ntokens:\n  - c\n")
	writeFile(t, filepath.Join(styles, "S", ".drafts", "Old.yml"), "extends: existence\ntokens:\n  - d\n")
	writeFile(t, filepath.Join(styles, "S", "One.test.yml"), "- name: x\n  input: a\n  want: ''\n")
	writeFile(t, filepath.Join(styles, "config", "scopes", "Names.yml"), "lead: text & doc(h1 + p)\n")
	writeFile(t, filepath.Join(styles, "config", "views", "V.yml"), "- name: x\n")

	runner := NewRunner(nil)
	runner.paths, runner.pathsSet = []string{styles}, true

	got, err := runner.Rules([]string{styles})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"S.One", "S.sub.Two"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// A file is taken as given.
	got, err = runner.Rules([]string{filepath.Join(styles, "S", "One.yml")})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"S.One"}) {
		t.Errorf("got %v, want [S.One]", got)
	}
}

func TestUncovered(t *testing.T) {
	results := []Result{
		{Got: "1:4:T.Weasel:'really' is a weasel word!\n"},
		{Got: ""},
		{Got: "2:1:T.Long:runs long: a message with: colons\n3:1:T.Weasel:again\n"},
	}

	got := Uncovered([]string{"T.Long", "T.Silent", "T.Weasel"}, results)
	if !slices.Equal(got, []string{"T.Silent"}) {
		t.Errorf("got %v, want [T.Silent]", got)
	}
	if got = Uncovered(nil, results); got != nil {
		t.Errorf("got %v from no rules", got)
	}
}
