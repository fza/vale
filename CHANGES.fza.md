# Fork additions

`fza/vale`, branch `v3`, carries work `errata-ai/vale` does not have. This is the
catalogue of it: what each addition does, why it exists, the surface it exposes,
and the tests that hold it. Nothing here is upstream's, so a merge that drops any
of it is a silent regression rather than a conflict.

The fork branched from upstream at `5d338235` (`v3.19.0-6`). List the commits
that carry this work with:

```bash
git log --oneline v3.22.0..HEAD
```

## User-facing surface

A merge that keeps the code but loses one of these names has still broken a
consumer.

| Surface | Kind | Meaning |
|---|---|---|
| `--no-cache` | CLI flag | Lint every file, ignoring any cached result. |
| `--no-nested` | CLI flag | Read no `.vale.ini` from the directories being linted. |
| `.vale.ini` in any directory | Configuration | Glob sections covering that directory and the tree beneath it. |
| `cache-clean` | CLI command | Remove every cached result, reporting how much was reclaimed. |
| `VALE_CACHE` | Environment variable | Where to store the alerts of unchanged files. |
| `scope: literal` | Rule scope | Match a string literal. Also `literal.line` and `literal.block`. |
| `<vale off>` … `</vale off>` | Directive | The tag form of a `vale` directive, in markup and in a source comment alike. |
| `.sh`, `.bash`, `.zsh`, `.ksh` | Format | Read as code: only the comments are prose. |
| `.mk`, `.mak`, `.make`, `Makefile`, `makefile`, `GNUmakefile` | Format | Read as code: only the comments are prose. |

## Correctness

### A vocabulary keeps every spelling it accepts

A vocabulary lists the spellings a project accepts. Keying the swap map by the
lowercase form alone kept whichever line came last, so a token written both
`mariadb` and `MariaDB` had one of its own accepted spellings reported as a
defect. Each key now holds every spelling, joined as an alternation, which the
rule matches as a pattern and therefore reports none of them.

- `internal/check/manager.go` — `vocabSpellings`, called from `loadVocabRules`.
- `internal/check/manager_test.go` — `TestVocabSpellings`.

### An accepted phrase matches across any whitespace

An accepted phrase is compiled into a pattern, so the literal spacing between
its words decided what it matched. A writer who wrapped a line between the two
words of an accepted phrase got the component word reported, which reads as a
rule firing on correct text. The whitespace inside a phrase matches any run of
it, so the phrase means the same thing however it is spaced.

Upstream carries this as `termPattern`, which also escapes the periods of a
literal term and is applied to the exception list as well as to the phrase
list. That is the wider fix, so it is the one in place; the fork keeps only its
test.

- `internal/check/definition.go` — `termPattern` (upstream).
- `internal/check/existence_test.go` — `TestExceptionPhraseSpansAnyWhitespace`.

### A `vale` directive is honoured inside a source comment

A directive suppressed rules in markup and did nothing in source, so a comment
could not turn a rule off over the lines it applies to. A comment is now split
at each control line and each run is linted under the toggle state its own lines
sit beneath. Every run keeps the comment's full height — a line outside it, and
a line the directive suppressed, is blank rather than absent — so an alert still
maps back onto the source through the comment's strip table. The toggle state
reaches the file, so a directive carries into later comments.

- `internal/lint/code.go` — `eachDirectiveRun`, wrapping the lint call in `lintCode`.
- `internal/core/file.go` — `IsCommentControl`.
- `internal/lint/code_test.go`.

### A setting reaches a rule group between a rule and its style

A rule's name spans the subdirectories it sits under, and a style's own name may
hold a dot, so `House.Literals.TwoWordVerb` belongs to the group
`House.Literals` inside the style `House`. A setting was looked for under the
rule's full name and then under the first segment alone, so a group key --
`House.Literals = NO` for one section -- sat at neither end and was never found,
and the group ran everywhere. Every ancestor prefix is now consulted, most
specific first, wherever a rule's setting, level, base style or in-text region
is resolved.

- `internal/core/util.go` -- `SettingKeys`.
- `internal/lint/lint.go` -- `lookup`, `lookupUnless`, `basedOn`.
- `internal/check/manager.go` -- `checkSetting`.
- `internal/core/file.go` -- `File.Level`, `File.RegionDisabled`.
- `internal/core/util_test.go` -- `TestSettingKeys`; `testdata/e2e/config.yaml` case `style-group-disabled`.

## Features

### A directive written as a tag

`<vale off>` opens a region and `</vale off>` closes it, reading the same in
markup and in a source comment. A closing tag carries the opposite sense of the
one it closes, so `</vale House.Passive>` restores that rule. The tag is read
from its own source text rather than from a parsed token, because an HTML
tokenizer lowercases an attribute name and drops the ones on a closing tag,
while a rule name is case-sensitive.

Markdown needs the rewrite before conversion: a closing tag holds no attribute,
so `</vale off>` is not valid HTML and a Markdown parser escapes it into
paragraph text. Only a line holding nothing else is rewritten, and never one
inside a fence, so a document showing a directive keeps showing it.

- `internal/core/file.go` — `NormalizeDirective`, `valeTagRE`.
- `internal/lint/md.go` — `markTagDirectives`, applied to the source before `md.Convert`.
- `internal/lint/ast.go` — the tag consumed in `lintHTMLTokens` before element bookkeeping.
- `internal/lint/walk.go` — `walker.rawTok` and `walker.raw()`, the source text captured before `Token` reuses the buffer `Raw` points into.
- `internal/core/file_test.go` — `TestNormalizeDirective`; `internal/lint/md_directive_test.go`.

### A backticked span and a fenced block are masked in a code comment

A code comment carries no markup for a parser to skip, so a prose rule read an
identifier, a flag, a path or a whole sample of output as prose. Each is now
blanked to spaces of its own byte length, with a line break inside a fence
surviving as one, so every line and every column is preserved and an alert still
maps back onto the source. A fence is masked first, so the backticks opening and
closing it cannot pair with the ones a span inside it carries.

- `internal/lint/code.go` — `maskCode`, `fencedCode`, `inlineCode`, `blankKeepingLines`.
- `internal/lint/code_test.go`.

### A doc comment's opening run reads as code

Go, C, Swift and Rust all ask a doc comment to open with the name it documents,
and no markup is allowed in that position — the one place backticks cannot
reach. That run is blanked so a documented symbol reads as code rather than as
prose. One comment may document several symbols at once, written as names
separated by slashes, and every name in the run is masked.

The mask applies only where the source below names the symbol, so a comment
opening with a misspelled name still reports. The first name is checked against
the declaration directly beneath; every later name is a sibling, so it is looked
for across the declaration block rather than on one line. A colon after the run
makes the comment an annotation rather than documentation, so those words stay
prose. A package comment is covered in the same way, with the name in its second
position after the word `Package`.

- `internal/lint/code.go` — `maskDocOpener`, `docOpener`, `packageOpener`, `runNames`, `declares`, `declarationBlock`, `namedInAny`, `namedIn`, `commentEnd`, `mask`, `blockSlack`.
- `internal/lint/docopener_test.go`.

### A Go string literal is its own scope family

A program prints strings to a person: a flag's help text, an error's message, a
prompt. They were unreachable. They are now extracted under `literal` —
`literal.line` and `literal.block` — which sits outside `text` deliberately: a
source file holds far more strings that are not prose than strings that are, so
under `text` every rule already written would match every path, wire value and
map key in the file.

Extraction is additive and conditional. The literal queries run only when the
configuration holds a rule naming the family, so a style that never mentions it
costs exactly what it did before. Go's two structural strings — an import path
and a struct tag — are dropped at capture, because a rule asking for prose has
no way to tell them from the help text beside them. The opening quote's column
is recorded as a strip rather than as an offset, because only the first line
pays it: a raw string's later lines begin at the margin.

- `internal/lint/code/comments.go` — `LiteralScope`, `IsLiteral`, `WithLiterals`, `splitLiterals`.
- `internal/lint/code/lang.go` — `Language.Literals`, `Language.SkipLiteral`.
- `internal/lint/code/go.go` — the literal queries and `goStructuralLiteral`.
- `internal/lint/code/query.go` — the `literal` capture, `trimQuotes`, the family in the scope name, the quote recorded as a strip.
- `internal/lint/code.go` — `blockScope`, the `literal` exemption in `skipsComment`, `code.WithLiterals` gated on `Manager.HasScope`.
- `internal/lint/fragment.go` — a literal skipped on the markup path, which would rebuild it under `text`.
- `internal/lint/code/literal_test.go`; `testdata/e2e/scopes.yaml` cases `literal` and `literal-unasked`, with fixtures under `testdata/fixtures/scopes/`.

### A directory carries its own configuration

A `.vale.ini` in any directory holds glob sections covering that directory and
the tree beneath it, so a subtree states its own rules where it lives rather
than through a path buried in the root file. Each nested file's sections are
anchored to its own directory and appended in depth order, which makes them
ordinary sections and leaves everything that reads a section untouched: one
configuration, one `check.Manager`, one compile.

A nested section replaces the style list exactly as a flat section does, while
rule settings, levels and vocabularies accumulate -- the split Vale already
draws within one file. `BasedOnStyles =` remains the hard reset it is.

Discovery follows the paths a run is given and finishes before the rules
compile, because a nested section's `BasedOnStyles` adds to the styles
compilation loads. A file argument contributes every configuration between the
root and itself; a directory argument is walked for its directories alone,
skipping what a lint walk skips. Directory lookups are memoised, so a run given
several thousand paths under one tree reads each directory once.

A nested file carries glob sections and nothing else. Every key Vale reads
outside a section decides a whole run, so a nested file holding one is refused
by name rather than reaching past the directory it describes.

`--config`, `VALE_CONFIG_PATH` and `--sources` read no nested file: naming a
configuration means that file rather than a search, so a run pinned to one
reports the same whatever a checkout holds.

- `internal/core/nested.go` -- `NestedConfigName`, `discoverNested`, `walkDirs`, `anchorLabel`, `loadNested`, `IsDirPath`.
- `internal/core/ini.go` -- `processSection`, `reservedSections`.
- `internal/core/source.go` -- `ReadPipeline` taking the run's inputs, `wantsNested`.
- `internal/core/config.go` -- `Config.NestedFiles`, `CLIFlags.NoNested`.
- `internal/lint/cache.go` -- `--no-nested` in the salt; a nested file is a configuration file, so the file list already covers it.
- `internal/core/nested_test.go`; `testdata/e2e/config.yaml` cases `nested-carves-out-a-subtree`, `nested-deeper-wins`, `nested-disabled`, `nested-rejects-a-run-wide-key`.

### A shell script is parsed as code

`.sh`, `.bash`, `.zsh` and `.ksh` are read with the shell grammar, so only the
comments are prose.

- `internal/core/format.go`, `internal/lint/code/lang.go`, `internal/lint/code/sh.go`.
- `internal/lint/code/sh_test.go`.

### A build file is parsed as code, matched by name where it has no extension

A build file is named rather than suffixed, so matching on the extension alone
never reached one. `Makefile`, `makefile` and `GNUmakefile` are matched by name;
`.mk`, `.mak` and `.make` by extension. Both reach the shell grammar, which the
two share a comment syntax with and which recovers across a rule header it
cannot read, so a comment below one is captured exactly as a comment above it is.

A file matched by name has no extension of its own, so `CodeExt` supplies the
normed one to whatever needs to name a language.

- `internal/core/format.go` — `FormatByFilename`, consulted from `FormatFromExt` only where the configured `[formats]` mapping has nothing for the path, so a project naming such a file itself still decides what it is read as.
- `internal/core/file.go` — `File.CodeExt`.
- `internal/lint/code/mk.go`, `internal/lint/code/lang.go`.
- `internal/lint/code.go` and `internal/lint/fragment.go` — `GetLanguageFromExt(f.CodeExt())` rather than `f.RealExt`.
- `internal/core/format_test.go` — `TestFormatFromExtNamesABuildFile`.

## Performance

These are the reason the fork exists for its consumer. A merge that keeps the
features and loses these has lost the point.

### The alerts of an unchanged file are reused

A result cache modelled on the Go build cache: entries are addressed by a hash
of everything that could change the answer, each lives in its own file, and
nothing indexes them. A missing, truncated or unreadable entry is a miss, so an
interrupted write costs a re-lint and never a wrong result. Eviction is by last
use — a read touches an entry's modification time at most hourly, and a sweep
that runs at most daily deletes what has gone unused for five days.

The salt covers the running binary's own size and modification time — a
development build reports no version, so the version alone cannot tell two of
them apart and one would read what the other wrote — the binary's version, the
resolved configuration, the ini files
it was read from, the `--sources` files, `--ignore-syntax`, `--filter` and its
body, and the styles path hashed as a tree. A change to any of it leaves the old
entries unreachable rather than wrong. The key adds the file's path — the
configuration assigns rules by path, so the same bytes under two names are two
different questions — and its content.

Only formats whose alerts follow from the file's own bytes are cached: all code,
and Markdown and HTML. The rest are excluded because their converter reads the
filesystem itself — DITA resolves maps and content references, XML applies an
external stylesheet, and AsciiDoc, reStructuredText and Typst are converted by a
separate program whose own include handling decides what it reads — so a sound
key would have to name files Vale never sees. Only a complete answer is stored:
a run that failed part way through would otherwise hide the rest of a file's
alerts for as long as the entry lives. A cache that cannot be opened is reported
and then done without.

- `internal/cache/cache.go` — the store: `Open`, `Key`, `Get`, `Put`, `Clean`, `Size`, `Dir`, `trim`, `markUsed`.
- `internal/lint/cache.go` — the policy: `EnableCache`, `cacheSalt`, `hashTree`, `cacheable`, `cacheKey`, `cachedResult`, `storeResult`, `cachedExts`, `cachedAlert`.
- `internal/lint/lint.go` — `Linter.cache`, and the lookup and store around `lintFile`.
- `cmd/vale/cache.go`, `cmd/vale/command.go`, `cmd/vale/flag.go`, `cmd/vale/main.go`, `internal/core/config.go`.
- `internal/cache/cache_test.go`, `internal/lint/cache_test.go`.
- `internal/e2e/e2e_test.go` — every scenario runs with `--no-cache`, so a case reports what it was given rather than what an earlier build reported for the same fixture.

### Every input path goes through one worker pool

Walking the inputs one at a time drained the pool for each before starting the
next, so a run given a list of files — what `git ls-files | xargs vale`
produces — linted them one after another and left the pool idle. One pool now
spans every root. Results are collected per input and flattened afterwards, so
the order files are reported in still follows the order they were asked for;
concurrency decides which file within a directory finishes first, as it always
has.

- `internal/lint/lint.go` — `lintFiles` taking every root, `lintResult.root`, `inOrder`, `fileWorkers`.
- `internal/lint/order_test.go`.

### The vocabulary is matched in Vale, prefiltered

`strcase` recompiles each vocabulary term for every word it examines, so a
converter holding a real project's thousand-term vocabulary cost a thousand
compilations per word. The vocabulary is compiled once when the rule loads, and
Vale's own prefilter (`regex.Required`) rules most terms out with a substring
test before the engine is asked anything.

`$title` builds its converter per string over only the terms the prefilter
cannot rule out, which leaves a handful. `$sentence` uses a converter written
here, because its tokenizer has to know the vocabulary — a term may span what
would otherwise be several tokens — so its pattern is built from the vocabulary
and compiled once when the rule loads rather than once per block. A term that
does not compile still answers for a word it equals. A match error ends a scan
and keeps what was found rather than panicking, because a vocabulary entry is
user-supplied.

The replacement is held to `strcase`'s own answers: `TestSentenceConverterMatchesStrcase`
runs both over a matrix of headings and vocabulary sets, and
`TestVocabMatcherPrefilterIsSound` holds the prefilter to skipping work only,
never to changing an answer.

- `internal/check/vocab.go` — `vocabMatcher`, `vocabEntry`, `sentenceConverter`, `indicatorFunc`, `tokenize`, `toTitle`.
- `internal/check/capitalization.go`, `internal/check/variables.go`.
- `internal/check/vocab_test.go`, `internal/check/variables_test.go`.

### The spelling exception pattern is compiled once

Compiling inside the loop that gathers exceptions rebuilt the alternation from
every term collected so far, making the cost quadratic in the size of the
vocabulary and throwing away all but the last result. A 1,000-term vocabulary
spent over three seconds there before a single file was read.

- `internal/check/spelling.go` — the compile moved after the loop in `addExceptions`.

### A built-in rule the configuration disables everywhere is not built

The built-in rules answer to `Vale.Rule = NO` the same way a style's do, and
building one is not free: `Vale.Spelling` reads a dictionary off disk, and
`Vale.Terms` compiles a pattern holding the whole vocabulary. Each is now gated
on `enabledSomewhere`, as the style rules already were.

A rule built from a vocabulary that a section names is exempt. Such a rule is
registered as globally off and switched on for that section's files alone, so
the global setting it carries is a default rather than a disable, and reading it
as one would skip a rule the run goes on to ask for.

- `internal/check/manager.go` — the gates in `loadDefaultRules`, `addTerms` and `addAvoid`, and `sectionVocabRule` inside `enabledSomewhere`.
- `internal/check/enabled_test.go` — `TestDisabledBuiltinRuleIsNotCompiled`, `TestEnabledBuiltinRulesAreCompiled`, `TestDisabledVocabRuleIsNotCompiled`, `TestSectionVocabRuleIsCompiled`.

## Divergences from upstream's own fixtures

One upstream expectation states a behaviour this fork deliberately replaces, so
its test data says what this fork does.

| Case | Upstream | This fork |
|---|---|---|
| `testdata/e2e/misc.yaml`, `vocab-multiple` | Two vocabularies spelling one token differently: the later one wins, and the earlier spelling is reported. | Both spellings are accepted, and neither is reported. |

## What proves it survives

The fork's own suite is necessary and not sufficient: it does not exercise the
rules the consuming repository depends on. `UPSTREAM-MERGE.md` holds the build
environment, the known conflicts, and the consumer check that does.

Two upstream behaviours differ at v3.22.0 from earlier releases, and a
repository moving to it meets both as findings against prose that was clean
before. Neither is a fork feature, and neither is a fault in the prose:

- A rejected term is compiled through `termPattern`, as an accepted one already
  was, so a rejected phrase matches across the line a writer wrapped it on.
- The rewritten spelling engine does not accept an acronym's plural -- `POSTs`
  -- on its own. A vocabulary that wants one lists it.
