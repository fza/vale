package nlp

import "testing"

func TestQuotedWords(t *testing.T) {
	for _, tt := range []struct {
		text string
		want int
	}{
		{`"Get out," she said. He grumbled, "Fine."`, 3},
		{`No quotes here.`, 0},
		{"“Curly,” he said. ‘And single,’ she said.", 3},
		{`An open quote "runs to the paragraph's end`, 5},
	} {
		if got := QuotedWords(tt.text); got != tt.want {
			t.Errorf("QuotedWords(%q) = %d, want %d", tt.text, got, tt.want)
		}
	}
}
