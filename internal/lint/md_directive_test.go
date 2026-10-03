package lint

import "testing"

func TestMarkTagDirectives(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			"rewrites a region",
			"a\n\n<vale off>\n\nb\n\n</vale off>\n",
			"a\n\n<!-- vale off -->\n\nb\n\n<!-- vale on -->\n",
		},
		{
			"rewrites a rule",
			"<vale House.Passive = NO>\n</vale House.Passive>\n",
			"<!-- vale House.Passive = NO -->\n<!-- vale House.Passive = YES -->\n",
		},
		{
			"keeps the indent a list item needs",
			"- item\n\n  <vale off>\n",
			"- item\n\n  <!-- vale off -->\n",
		},
		{"leaves a fenced example alone", "```\n<vale off>\n```\n", "```\n<vale off>\n```\n"},
		{"leaves a tilde fence alone", "~~~\n<vale off>\n~~~\n", "~~~\n<vale off>\n~~~\n"},
		{"leaves an inline mention alone", "See `<vale off>` for that.\n", "See `<vale off>` for that.\n"},
		{"leaves a line carrying prose alone", "Write <vale off> here.\n", "Write <vale off> here.\n"},
		{"leaves the comment form alone", "<!-- vale off -->\n", "<!-- vale off -->\n"},
		{"leaves text with no directive alone", "a\n\nb\n", "a\n\nb\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := markTagDirectives(tt.in)
			if got != tt.want {
				t.Errorf("got %q; want %q", got, tt.want)
			}
		})
	}
}

func TestDirectiveIndex(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		txt     string
		wantAt  int
		wantLen int
	}{
		{"finds the comment form", "a\n<!-- vale on -->\n", "vale on", 7, 7},
		{"finds a closing tag by what it means", "a\n</vale off>\n", "vale on", 2, 11},
		{"finds a rule's closing tag", "</vale House.Passive>", "vale House.Passive = YES", 0, 21},
		{"takes an opening tag whole", "a <vale off>", "vale off", 2, 10},
		{"takes whichever comes first", "</vale off> vale on", "vale on", 0, 11},
		{"skips a tag meaning something else", "<vale off> vale on", "vale on", 11, 7},
		{"reports a directive not written", "a\nb\n", "vale on", -1, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			at, n := directiveIndex(tt.src, tt.txt)
			if at != tt.wantAt || n != tt.wantLen {
				t.Errorf("directive should be located at its own span in the source, got (%d, %d)", at, n)
			}
		})
	}
}
