package code

import "testing"

func TestPHPDocMasks(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		masked []string
		prose  []string
	}{
		{
			name: "a block tag masks its type and variable, not the description",
			text: "Sends the mail.\n" +
				"@param string $methodName the method to call\n" +
				"@param ?Foo\\Bar|null &$target where the result goes\n" +
				"@param int ...$values the values to add\n" +
				"@throws \\RuntimeException when the mail cannot be sent\n" +
				"@var array<TokenInterface> $authenticatedTokens\n",
			masked: []string{
				"@param string $methodName", "@param ?Foo\\Bar|null &$target", "@param int ...$values",
				"@throws \\RuntimeException", "@var array<TokenInterface> $authenticatedTokens",
			},
			prose: []string{"Sends the mail", "the method to call", "where the result goes", "the values to add", "when the mail cannot be sent"},
		},
		{
			name: "a nested type is masked to its last bracket",
			text: "@param array<string, list<int>> $map the lookup table\n" +
				"@return array{name: string, tags?: list<string>} the parsed record\n" +
				"@phpstan-param callable(int, string): bool $check the predicate\n",
			masked: []string{
				"@param array<string, list<int>> $map",
				"@return array{name: string, tags?: list<string>}",
				"@phpstan-param callable(int, string): bool $check",
			},
			prose: []string{"the lookup table", "the parsed record", "the predicate"},
		},
		{
			name: "an annotation is masked with its arguments, across lines and nested",
			text: "The orders of a customer.\n" +
				"@ORM\\ManyToOne(fetch=\"EAGER\")\n" +
				"@ORM\\OrderBy({\"x\" = \"DESC\"})\n" +
				"@Flow\\Validate(argumentName=\"scheduleDate\",\n" +
				"    type=\"NotEmpty\")\n" +
				"@ORM\\JoinTable(joinColumns={@ORM\\JoinColumn(name=\"a_id\")})\n" +
				"@Flow\\Inject\n" +
				"Kept in sync by the importer.\n",
			masked: []string{
				"@ORM\\ManyToOne(fetch=\"EAGER\")",
				"@ORM\\OrderBy({\"x\" = \"DESC\"})",
				"@Flow\\Validate(argumentName=\"scheduleDate\",\n    type=\"NotEmpty\")",
				"@ORM\\JoinTable(joinColumns={@ORM\\JoinColumn(name=\"a_id\")})",
				"@Flow\\Inject",
			},
			prose: []string{"The orders of a customer", "Kept in sync by the importer"},
		},
		{
			name: "references, versions, templates, signatures and inline tags are masked",
			text: "Reads {@see Reader::read()} and {@link https://example.com docs}.\n" +
				"@see \\Foo\\Bar::baz() for the details\n" +
				"@since 2.1.0 the default\n" +
				"@deprecated use readAll instead\n" +
				"@template T of \\Countable the item type\n" +
				"@method static Collection<int, User> findBy(array $criteria) runs the query\n" +
				"@author Jane Doe <jane@example.com>\n" +
				"@internal only the importer calls this\n",
			masked: []string{
				"{@see Reader::read()}", "{@link https://example.com docs}",
				"@see \\Foo\\Bar::baz()", "@since 2.1.0", "@deprecated", "@template T of \\Countable",
				"@method static Collection<int, User> findBy(array $criteria)",
				"@author Jane Doe <jane@example.com>", "@internal",
			},
			prose: []string{
				"Reads", "for the details", "the default", "use readAll instead", "the item type",
				"runs the query", "only the importer calls this", "use",
			},
		},
		{
			name:   "a variable named in prose is masked, an amount is not",
			text:   "Call this before $controllerContext is set; costs $5 at most.\n",
			masked: []string{"$controllerContext"},
			prose:  []string{"Call this before", "$5"},
		},
		{
			name: "unclosed syntax is masked to the end of its line",
			text: "@ORM\\Column(type=\"string\"\nThe column holds the name.\n" +
				"@param array<string $x the broken type\n" +
				"@param array<int} $y the mismatched type\n",
			masked: []string{"@ORM\\Column(type=\"string\"", "@param array<string $x", "@param array<int} $y"},
			prose:  []string{"The column holds the name", "the broken type", "the mismatched type"},
		},
		{
			name:   "a broken type with no variable after it is masked to its last whole part",
			text:   "@return array<string the broken result\n",
			masked: []string{"@return array"},
			prose:  []string{"<string the broken result"},
		},
		{
			name: "an angle bracket inside an annotation's arguments is no bracket",
			text: "@Assert\\Expression(value > 3,\n" +
				"    message=\"too small\")\n" +
				"@Assert\\Choice(choices={1 => \"a\", 2 => \"b\"})\n" +
				"@Assert\\Expression(value < 3) The value stays small.\n",
			masked: []string{
				"@Assert\\Expression(value > 3,\n    message=\"too small\")",
				"@Assert\\Choice(choices={1 => \"a\", 2 => \"b\"})",
				"@Assert\\Expression(value < 3)",
			},
			prose: []string{"The value stays small"},
		},
		{
			name: "PHPUnit tags mask what they name",
			text: "@dataProvider provideInvalidEmails\n" +
				"@depends clone testCreatesTheUser\n" +
				"@group slow-integration\n" +
				"@ticket GVB-123\n" +
				"@requires PHP >= 8.1\n" +
				"@requires extension mysqli\n" +
				"@expectedException \\Foo\\BarException when the input is empty\n" +
				"@usesDefaultClass \\Foo\\Bar\n" +
				"@backupGlobals enabled\n" +
				"@testdox Rejects an empty address\n",
			masked: []string{
				"@dataProvider provideInvalidEmails", "@depends clone testCreatesTheUser",
				"@group slow-integration", "@ticket GVB-123", "@requires PHP >= 8.1",
				"@requires extension mysqli", "@expectedException \\Foo\\BarException",
				"@usesDefaultClass \\Foo\\Bar", "@backupGlobals enabled", "@testdox",
			},
			prose: []string{"when the input is empty", "Rejects an empty address"},
		},
		{
			name: "testWith masks its data rows, on its line and the lines after it",
			text: "@testWith [\"a\", 1]\n" +
				"          [\"bc\", 2]\n" +
				"Each row is one case.\n",
			masked: []string{"@testWith [\"a\", 1]\n          [\"bc\", 2]"},
			prose:  []string{"Each row is one case"},
		},
		{
			name: "the analysers' own tags mask their syntax",
			text: "@phpstan-type UserShape array{name: string, age: int}\n" +
				"@psalm-type Ids = list<positive-int>\n" +
				"@phpstan-import-type UserShape from \\App\\Repo\n" +
				"@psalm-suppress InvalidArgument, MixedAssignment\n" +
				"@phpstan-ignore argument.type (the legacy caller passes a string)\n" +
				"@param-closure-this \\App\\Builder $this\n" +
				"@template-extends \\App\\Base<int>\n" +
				"@phpstan-require-extends \\App\\Model\n" +
				"@phpstan-self-out self<string>\n" +
				"@psalm-if-this-is Foo<int>\n" +
				"@ignore the old entry point\n" +
				"@type int the legacy alias of var\n",
			masked: []string{
				"@phpstan-type UserShape array{name: string, age: int}",
				"@psalm-type Ids = list<positive-int>",
				"@phpstan-import-type UserShape from \\App\\Repo",
				"@psalm-suppress InvalidArgument, MixedAssignment",
				"@phpstan-ignore argument.type",
				"@param-closure-this \\App\\Builder $this",
				"@template-extends \\App\\Base<int>",
				"@phpstan-require-extends \\App\\Model",
				"@phpstan-self-out self<string>",
				"@psalm-if-this-is Foo<int>",
				"@ignore", "@type int",
			},
			prose: []string{
				"the legacy caller passes a string", "the old entry point",
				"the legacy alias of var",
			},
		},
		{
			name:   "an example names a file and the lines to show",
			text:   "@example demo/send.php 10 5 sends one mail\n",
			masked: []string{"@example demo/send.php 10 5"},
			prose:  []string{"sends one mail"},
		},
		{
			name:   "a tag named inside a sentence is prose, as PHPDoc reads it",
			text:   "See also @param and @return for the rest.\n",
			masked: nil,
			prose:  []string{"See also @param and @return for the rest"},
		},
		{
			name:   "a quoted argument may hold a closing bracket",
			text:   "@Assert\\Regex(pattern=\"/^[a-z)]+\\\"$/\") The name holds letters.\n",
			masked: []string{"@Assert\\Regex(pattern=\"/^[a-z)]+\\\"$/\")"},
			prose:  []string{"The name holds letters"},
		},
		{
			name:   "an intersection type is masked whole",
			text:   "@param Countable&Traversable $items the items to count\n",
			masked: []string{"@param Countable&Traversable $items"},
			prose:  []string{"the items to count"},
		},
		{
			name:   "a variable given without a type is masked with its tag",
			text:   "@param $legacy the old name\n",
			masked: []string{"@param $legacy"},
			prose:  []string{"the old name"},
		},
		{
			name: "array, grouped, literal and constant types are masked whole",
			text: "@return string[] the names\n" +
				"@return (int|string)[] the keys\n" +
				"@param 'asc'|'desc' $order the sort order\n" +
				"@param Status::ACTIVE $status the state\n",
			masked: []string{
				"@return string[]", "@return (int|string)[]",
				"@param 'asc'|'desc' $order", "@param Status::ACTIVE $status",
			},
			prose: []string{"the names", "the keys", "the sort order", "the state"},
		},
		{
			name:   "a tag name is matched whatever its case",
			text:   "@coversDefaultClass \\Foo\\Bar the subject under test\n",
			masked: []string{"@coversDefaultClass \\Foo\\Bar"},
			prose:  []string{"the subject under test"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkMasks(t, PHPDoc(tt.text), tt.text, tt.masked, tt.prose)
		})
	}
}

func TestPHPDocIgnoreMaskStopsBeforeTheBlankOpeningItsReason(t *testing.T) {
	text := "@phpstan-ignore argument.type   (the legacy caller passes a string)\n"
	want := Mask{0, len("@phpstan-ignore argument.type")}
	for _, m := range PHPDoc(text) {
		if m[0] == 0 && m != want {
			t.Errorf("the mask should end at the identifiers, leaving the blanks before the reason; masks %v", m)
		}
	}
}

func TestPHPDocIgnoresAnAtSignThatOpensNoTag(t *testing.T) {
	text := "Write to admin@example.com or @ the desk.\n@ alone\n"
	if masks := PHPDoc(text); len(masks) != 0 {
		t.Errorf("an @ that opens no tag should mask nothing; masks %v", masks)
	}
}

func TestPHPCommentsCarryDocMasks(t *testing.T) {
	source := "<?php\nclass A {\n\t/**\n\t * Sends the mail.\n\t * @param string $methodName the method to call\n\t */\n\tpublic function x($methodName) {}\n}\n"
	comments, err := GetComments([]byte(source), PHP())
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 {
		t.Fatalf("a doc block should be one comment, got %d", len(comments))
	}
	checkMasks(t, comments[0].Masks, comments[0].Text,
		[]string{"@param string $methodName"},
		[]string{"Sends the mail", "the method to call"})
}
