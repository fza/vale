package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// writeInputs creates n files and returns their paths in an order that is not
// the order the directory holds them in.
func writeInputs(t *testing.T, dir string, n int) []string {
	t.Helper()

	paths := make([]string, 0, n)
	for i := range n {
		path := filepath.Join(dir, fmt.Sprintf("file-%02d.txt", i))
		body := fmt.Sprintf("The abandonment of plan %d was intentional.\n", i)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}

	// Reverse, so a result that merely follows the walk cannot pass.
	for i, j := 0, len(paths)-1; i < j; i, j = i+1, j-1 {
		paths[i], paths[j] = paths[j], paths[i]
	}

	return paths
}

// Files are reported in the order they were asked for, which is what a caller
// piping a file list into Vale sees. Only the order within a directory is left
// to whichever worker finishes first.
func TestLintKeepsInputOrder(t *testing.T) {
	inputs := writeInputs(t, t.TempDir(), 24)

	linted, err := demoLinter(t).Lint(inputs, "*")
	if err != nil {
		t.Fatal(err)
	}

	if len(linted) != len(inputs) {
		t.Fatalf("linted %d files, gave %d", len(linted), len(inputs))
	}

	for i, file := range linted {
		if file.Path != inputs[i] {
			t.Fatalf("position %d: got %q, want %q", i, file.Path, inputs[i])
		}
	}
}

// A run repeated over the same inputs reports them the same way.
func TestLintOrderIsStable(t *testing.T) {
	inputs := writeInputs(t, t.TempDir(), 24)

	var first []string
	for run := range 5 {
		linted, err := demoLinter(t).Lint(inputs, "*")
		if err != nil {
			t.Fatal(err)
		}

		got := make([]string, 0, len(linted))
		for _, file := range linted {
			got = append(got, file.Path)
		}

		if run == 0 {
			first = got
			continue
		}
		for i := range got {
			if got[i] != first[i] {
				t.Fatalf("run %d, position %d: got %q, want %q",
					run, i, got[i], first[i])
			}
		}
	}
}

// Every file of every input is reported, whether the input names a file or a
// directory holding several.
func TestLintCoversEveryInput(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0o750); err != nil {
		t.Fatal(err)
	}

	loose := writeInputs(t, dir, 3)
	grouped := writeInputs(t, nested, 4)

	inputs := append([]string{nested}, loose...)

	linted, err := demoLinter(t).Lint(inputs, "*")
	if err != nil {
		t.Fatal(err)
	}

	if len(linted) != len(loose)+len(grouped) {
		t.Fatalf("linted %d files, want %d", len(linted), len(loose)+len(grouped))
	}

	// The directory came first, so its files fill the front of the result.
	for i, file := range linted[len(grouped):] {
		if file.Path != loose[i] {
			t.Fatalf("position %d: got %q, want %q", i, file.Path, loose[i])
		}
	}
}
