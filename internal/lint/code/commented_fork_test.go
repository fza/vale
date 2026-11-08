package code

import (
	"strings"
	"testing"
)

// TestShellCommentedCode tests that a shell script and a makefile, which
// Bash's grammar reads any sentence of as a command, take a line for code
// only with a code signal and no sentence in it.
func TestShellCommentedCode(t *testing.T) {
	for _, ext := range []string{".sh", ".mk"} {
		lang, err := GetLanguageFromExt(ext)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range []string{"export FOO=bar", `echo "$name"`, "CFLAGS = -O2"} {
			if !lang.isCode(line) {
				t.Errorf("%s: code read as prose: %q", ext, line)
			}
		}
		for _, line := range []string{"Builds the image", "Set $HOME first", "the build runs twice"} {
			if lang.isCode(line) {
				t.Errorf("%s: prose read as code: %q", ext, line)
			}
		}
	}
}

// TestSvelteCommentedCode tests that each part of a component judges its
// commented-out code with its own grammar: the script with TypeScript's, a
// markup comment by the shape of a Svelte tag, a style comment with CSS's.
func TestSvelteCommentedCode(t *testing.T) {
	source := []byte(`<script lang="ts">
	// const open = load(id);
	// Keep the panel closed.
</script>

<!-- <Button on:click={save} /> -->
<!-- {#if open} -->
<!-- <p>Some text here</p> -->
<!-- Keep the button hidden -->
<!-- Shows {count} items -->

<style>
	/* color: red; */
	/* The panel is quiet */
</style>
`)
	lang, err := GetLanguageFromExt(".svelte")
	if err != nil {
		t.Fatal(err)
	}
	comments, err := GetComments(source, lang)
	if err != nil {
		t.Fatal(err)
	}

	var code, prose []string
	for _, c := range comments {
		hidden, _ := lang.HideCode(c)
		for i, line := range strings.Split(c.Text, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if strings.TrimSpace(strings.Split(hidden.Text, "\n")[i]) == "" {
				code = append(code, line)
			} else {
				prose = append(prose, line)
			}
		}
	}

	wantCode := []string{"const open = load(id);", "<Button on:click={save} />", "{#if open}", "color: red;"}
	wantProse := []string{"Keep the panel closed.", "<p>Some text here</p>", "Keep the button hidden", "Shows {count} items", "The panel is quiet"}
	if strings.Join(code, "|") != strings.Join(wantCode, "|") {
		t.Errorf("code = %q; want %q", code, wantCode)
	}
	if strings.Join(prose, "|") != strings.Join(wantProse, "|") {
		t.Errorf("prose = %q; want %q", prose, wantProse)
	}
}
