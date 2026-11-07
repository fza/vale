package code

import (
	"regexp"
	"strings"
)

// A PHPDoc block mixes two languages on one line: the tag syntax a tool reads
// -- `@param array<string, int> $map`, `@ORM\Column(type="string")` -- and
// the description a person reads after it. PHPStan, Psalm, IDEs and
// frameworks such as Doctrine and Flow all parse the syntax, so it cannot be
// rewritten to suit a prose rule; it is masked instead, and only the
// description stays prose.
//
// The syntax nests (generics, array shapes, callables, annotation arguments
// spanning lines), which no regular expression follows, so the block is
// scanned by hand.

// phpTagShape says how much syntax follows a block tag's name.
type phpTagShape int

const (
	phpTagName       phpTagShape = iota // the name alone: @api, @internal, @todo, @ORM\Id
	phpTagType                          // a type: @return, @throws, @extends
	phpTagTypeAndVar                    // a type, then a variable: @param, @var, @property
	phpTagReference                     // one token, an FQSEN or a URL: @see, @link, @covers
	phpTagVersion                       // a version, when one follows: @since, @deprecated
	phpTagTemplate                      // a name, then an optional bound: @template T of Foo
	phpTagMethod                        // a signature: @method static Foo bar(int $x)
	phpTagLine                          // the whole line, which holds no prose: @author, @license
)

var phpTagShapes = map[string]phpTagShape{
	"param":           phpTagTypeAndVar,
	"param-out":       phpTagTypeAndVar,
	"var":             phpTagTypeAndVar,
	"property":        phpTagTypeAndVar,
	"property-read":   phpTagTypeAndVar,
	"property-write":  phpTagTypeAndVar,
	"global":          phpTagTypeAndVar,
	"assert":          phpTagTypeAndVar,
	"assert-if-true":  phpTagTypeAndVar,
	"assert-if-false": phpTagTypeAndVar,

	"return":     phpTagType,
	"throws":     phpTagType,
	"yield":      phpTagType,
	"type":       phpTagType,
	"extends":    phpTagType,
	"implements": phpTagType,
	"use":        phpTagType,
	"mixin":      phpTagType,

	"see":                phpTagReference,
	"link":               phpTagReference,
	"uses":               phpTagReference,
	"used-by":            phpTagReference,
	"covers":             phpTagReference,
	"coversdefaultclass": phpTagReference,

	"since":      phpTagVersion,
	"version":    phpTagVersion,
	"deprecated": phpTagVersion,

	"template":               phpTagTemplate,
	"template-covariant":     phpTagTemplate,
	"template-contravariant": phpTagTemplate,

	"method": phpTagMethod,

	"author":     phpTagLine,
	"copyright":  phpTagLine,
	"license":    phpTagLine,
	"package":    phpTagLine,
	"subpackage": phpTagLine,
	"category":   phpTagLine,
	"example":    phpTagLine,
	"filesource": phpTagLine,
}

// The analysers prefix their own variants of the standard tags:
// `@phpstan-param`, `@psalm-return`, `@phan-var`.
var phpToolPrefixes = []string{"phpstan-", "psalm-", "phan-"}

var (
	phpInlineTag = regexp.MustCompile(`\{@[A-Za-z][\w\\-]*[^}\n]*\}`)
	phpVariable  = regexp.MustCompile(`\$[A-Za-z_\x80-\xff][\w\x80-\xff]*`)
	phpVersion   = regexp.MustCompile(`^v?\d[\w.+-]*`)
)

// PHPDoc masks a PHPDoc block's tag syntax: each block tag with the type,
// variable or reference it carries, a Doctrine or Flow annotation with its
// arguments, each inline tag, and a variable wherever it is named.
func PHPDoc(s string) []Mask {
	var out []Mask
	out = append(out, ranges(phpInlineTag, s)...)
	out = append(out, ranges(phpVariable, s)...)

	for at := 0; at < len(s); {
		lineEnd := strings.IndexByte(s[at:], '\n')
		if lineEnd < 0 {
			lineEnd = len(s)
		} else {
			lineEnd += at
		}

		start := skipBlanks(s, at)
		if start < lineEnd && s[start] == '@' {
			end := phpTagEnd(s, start, lineEnd)
			if end > start {
				out = append(out, Mask{start, end})
			}
		}
		at = lineEnd + 1
	}

	return out
}

// phpTagEnd returns where the syntax of the block tag at s[at] ends.
func phpTagEnd(s string, at, lineEnd int) int {
	nameEnd := at + 1
	for nameEnd < len(s) && isPHPTagNameByte(s[nameEnd]) {
		nameEnd++
	}
	if nameEnd == at+1 {
		return at
	}

	if nameEnd < len(s) && s[nameEnd] == '(' {
		// An annotation call: `@ORM\ManyToOne(fetch="EAGER")`.
		if end, ok := balancedEnd(s, nameEnd); ok {
			return end
		}
		return lineEnd
	}

	switch phpShapeOf(s[at+1 : nameEnd]) {
	case phpTagType:
		return scanPHPType(s, skipBlanks(s, nameEnd), nameEnd)
	case phpTagTypeAndVar:
		return scanPHPTypeAndVar(s, nameEnd)
	case phpTagReference:
		return scanToken(s, skipBlanks(s, nameEnd), nameEnd)
	case phpTagVersion:
		from := skipBlanks(s, nameEnd)
		if loc := phpVersion.FindStringIndex(s[from:lineEnd]); loc != nil {
			return from + loc[1]
		}
		return nameEnd
	case phpTagTemplate:
		return scanPHPTemplate(s, nameEnd)
	case phpTagMethod:
		return scanPHPMethod(s, nameEnd)
	case phpTagLine:
		return lineEnd
	default:
		return nameEnd
	}
}

func phpShapeOf(name string) phpTagShape {
	name = strings.ToLower(name)
	for _, prefix := range phpToolPrefixes {
		if strings.HasPrefix(name, prefix) {
			name = strings.TrimPrefix(name, prefix)
			break
		}
	}
	if shape, ok := phpTagShapes[name]; ok {
		return shape
	}
	return phpTagName
}

// scanPHPTypeAndVar reads `[Type] [$var]` after a tag; either may be absent,
// and a variable may be passed by reference or be variadic.
func scanPHPTypeAndVar(s string, at int) int {
	from := skipBlanks(s, at)
	if end, ok := phpVarEnd(s, from); ok {
		return end
	}

	end := scanPHPType(s, from, at)
	if end == at {
		return at
	}
	if varEnd, ok := phpVarEnd(s, skipBlanks(s, end)); ok {
		return varEnd
	}
	return end
}

// scanPHPTemplate reads `T [of|as|super Type]`.
func scanPHPTemplate(s string, at int) int {
	end := scanToken(s, skipBlanks(s, at), at)
	if end == at {
		return at
	}
	from := skipBlanks(s, end)
	for _, keyword := range []string{"of ", "as ", "super "} {
		if strings.HasPrefix(s[from:], keyword) {
			return scanPHPType(s, skipBlanks(s, from+len(keyword)), end)
		}
	}
	return end
}

// scanPHPMethod reads `[static] [ReturnType] name([params])`. A method with
// no return type reads as a callable type, which ends in the same place.
func scanPHPMethod(s string, at int) int {
	from := skipBlanks(s, at)
	if strings.HasPrefix(s[from:], "static ") {
		from = skipBlanks(s, from+len("static "))
	}

	end := scanPHPType(s, from, at)
	if end == at {
		return at
	}

	name := skipBlanks(s, end)
	nameEnd := name
	for nameEnd < len(s) && isPHPNameByte(s[nameEnd]) {
		nameEnd++
	}
	if nameEnd > name && nameEnd < len(s) && s[nameEnd] == '(' {
		if signatureEnd, ok := balancedEnd(s, nameEnd); ok {
			return signatureEnd
		}
	}
	return end
}

// scanPHPType reads a type expression starting at s[at] and returns where it
// ends, or fallback when there is none: a union or intersection of atoms,
// each a name, a quoted literal or a parenthesized type, followed by generic
// arguments, an array shape, a callable's parameters and return type, or `[]`.
func scanPHPType(s string, at, fallback int) int {
	end, ok := scanPHPTypeAtom(s, at)
	if !ok {
		return fallback
	}

	for {
		op := skipBlanks(s, end)
		if op >= len(s) || (s[op] != '|' && s[op] != '&') {
			return end
		}
		// `&$name` is a variable passed by reference: no type atom opens with
		// `$`, so the scan stops before it.
		next, ok := scanPHPTypeAtom(s, skipBlanks(s, op+1))
		if !ok {
			return end
		}
		end = next
	}
}

func scanPHPTypeAtom(s string, at int) (int, bool) {
	i := at
	if i < len(s) && s[i] == '?' {
		i++
	}
	if i >= len(s) {
		return at, false
	}

	switch c := s[i]; {
	case c == '(':
		end, ok := balancedEnd(s, i)
		if !ok {
			return at, false
		}
		i = end
	case c == '"' || c == '\'':
		end, ok := quotedEnd(s, i)
		if !ok {
			return at, false
		}
		i = end
	case isPHPNameByte(c) || c == '\\':
		for i < len(s) {
			if isPHPNameByte(s[i]) || s[i] == '\\' || s[i] == '-' {
				i++
			} else if strings.HasPrefix(s[i:], "::") {
				i += 2
			} else {
				break
			}
		}
	default:
		return at, false
	}

	for i < len(s) {
		switch s[i] {
		case '<', '{', '(':
			end, ok := balancedEnd(s, i)
			if !ok {
				return i, true
			}
			isCall := s[i] == '('
			i = end
			if colon := skipBlanks(s, i); isCall && colon < len(s) && s[colon] == ':' {
				i = scanPHPType(s, skipBlanks(s, colon+1), i)
			}
		case '[':
			if !strings.HasPrefix(s[i:], "[]") {
				return i, true
			}
			i += 2
		default:
			return i, true
		}
	}
	return i, true
}

// phpVarEnd reads `$name`, `&$name`, `...$name` or `&...$name` at s[at].
func phpVarEnd(s string, at int) (int, bool) {
	i := at
	if strings.HasPrefix(s[i:], "&") {
		i++
	}
	if strings.HasPrefix(s[i:], "...") {
		i += 3
	}
	loc := phpVariable.FindStringIndex(s[i:])
	if loc == nil || loc[0] != 0 {
		return at, false
	}
	return i + loc[1], true
}

// balancedEnd returns the index just past the bracket that closes the one at
// s[at], or false when the brackets do not balance. Quoted strings are
// skipped, so a bracket inside an annotation's string argument does not count.
func balancedEnd(s string, at int) (int, bool) {
	closers := map[byte]byte{'(': ')', '[': ']', '{': '}', '<': '>'}
	var stack []byte
	for i := at; i < len(s); i++ {
		switch c := s[i]; c {
		case '"', '\'':
			end, ok := quotedEnd(s, i)
			if !ok {
				return at, false
			}
			i = end - 1
		case '(', '[', '{', '<':
			stack = append(stack, closers[c])
		case ')', ']', '}', '>':
			if len(stack) == 0 || stack[len(stack)-1] != c {
				return at, false
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return i + 1, true
			}
		}
	}
	return at, false
}

// quotedEnd returns the index just past the quote closing the one at s[at],
// on the same line; a backslash escapes the character after it.
func quotedEnd(s string, at int) (int, bool) {
	quote := s[at]
	for i := at + 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '\n':
			return at, false
		case quote:
			return i + 1, true
		}
	}
	return at, false
}

// scanToken reads a run of non-blank characters, or returns fallback.
func scanToken(s string, at, fallback int) int {
	i := at
	for i < len(s) && s[i] != ' ' && s[i] != '\t' && s[i] != '\n' {
		i++
	}
	if i == at {
		return fallback
	}
	return i
}

func skipBlanks(s string, at int) int {
	for at < len(s) && (s[at] == ' ' || s[at] == '\t') {
		at++
	}
	return at
}

func isPHPNameByte(c byte) bool {
	return c == '_' || c >= 0x80 || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func isPHPTagNameByte(c byte) bool {
	return isPHPNameByte(c) || c == '\\' || c == '-'
}
