package code

import (
	"bytes"
	"regexp"
)

// Vue reads a single-file component's script blocks with the TypeScript
// grammar -- `<script>` and `<script setup>` alike, both when one file holds
// the two -- and nothing else. Its template and style are blanked before the
// parse, so neither reaches a rule: a template's text is the interface copy,
// written in whatever language the interface speaks, and a style is CSS.
//
// The TypeScript grammar reads plain JavaScript as well, so a block without
// `lang="ts"` needs no grammar of its own.
func Vue() *Language {
	lang := TypeScript()
	lang.Isolate = vueScripts
	return lang
}

// vueScript matches the opening tag of a script block. A component's blocks
// open at the start of a line; a tag indented inside the template is not one.
var vueScript = regexp.MustCompile(`(?m)^<script\b[^>]*>`)

// vueScriptEnd is where a script block ends: the first closing tag, which is
// where a browser ends one too.
var vueScriptEnd = regexp.MustCompile(`</script\s*>`)

// vueScripts returns the component with everything outside its script blocks'
// contents blanked to spaces, line breaks kept, so the grammar reads the
// scripts at the very lines and columns they hold in the file.
func vueScripts(source []byte) []byte {
	blanked := make([]byte, len(source))
	for i, b := range source {
		if b == '\n' {
			blanked[i] = '\n'
		} else {
			blanked[i] = ' '
		}
	}

	at := 0
	for {
		opener := vueScript.FindIndex(source[at:])
		if opener == nil {
			return blanked
		}

		// A script loaded from `src` closes its own tag and holds nothing.
		content := at + opener[1]
		if bytes.HasSuffix(source[at+opener[0]:content], []byte("/>")) {
			at = content
			continue
		}

		end, next := len(source), len(source)
		if closing := vueScriptEnd.FindIndex(source[content:]); closing != nil {
			end, next = content+closing[0], content+closing[1]
		}

		copy(blanked[content:end], source[content:end])
		at = next
	}
}
