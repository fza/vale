# Fork additions

`fza/vale`, branches `fza` and `v3`, carries work `errata-ai/vale` does not have. This is the
catalogue of it: what each addition does, why it exists, the surface it exposes,
and the tests that hold it. Nothing here is upstream's, so a merge that drops any
of it is a silent regression rather than a conflict.

The fork sits on upstream `v3.25.1`. Each change is a branch of its own merged
into `fza` (see "Branches"), so the changes are listed one merge per line with:

```bash
git log --first-parent --oneline upstream/v3..fza
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

### A `vale` directive is honoured inside a source comment

A directive suppressed rules in markup and did nothing in source, so a comment
could not turn a rule off over the lines it applies to. A comment is now split
at each control line and each run is linted under the toggle state its own lines
sit beneath. Every run keeps the comment's full height — a line outside it, and
a line the directive suppressed, is blank rather than absent — so an alert still
maps back onto the source through the comment's strip table. The toggle state
reaches the file, so a directive carries into later comments.

- `internal/lint/code.go` — `eachDirectiveRun`, wrapping the call to `lintComment` in `lintCode`, which takes each run's text rather than the comment's own. A comment converted as part of a reStructuredText batch is linted from the batch only while the run is the whole comment.
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

The rewritten comment is then located in the source by the tag it came from.
`</vale off>` reaches the walker as `vale on`, a text the source does not hold,
so a plain search found no position and the region `<vale off>` opened was never
closed, suppressing every alert to the end of the file. The located tag is
masked out of the source whole: masking the comment's words one by one instead
consumed matching words of the prose, such as the `on` in `environment`, and
the alert on them could no longer be placed.

- `internal/core/file.go` — `NormalizeDirective`, `valeTagRE`.
- `internal/lint/md.go` — `markTagDirectives`, applied to the source before `md.Convert`.
- `internal/lint/ast.go` — the tag consumed in `lintHTMLTokens` before element bookkeeping.
- `internal/lint/walk.go` — `walker.rawTok` and `walker.raw()`, the source text captured before `Token` reuses the buffer `Raw` points into; `directiveIndex`, used by `walker.commentAt`.
- `internal/core/file_test.go` — `TestNormalizeDirective`; `internal/lint/md_directive_test.go` — `TestMarkTagDirectives`, `TestDirectiveIndex`.
- `testdata/e2e/comments.yaml` — the `tag-*` cases: Markdown and MDX with and without blank lines, and a code-comment control.

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

The mask applies only in a language whose convention it is: Go and the C family
(`Language.OpensWithName` on Go, C, C++ and Rust). Elsewhere a comment's first
word is prose, because a JavaScript comment above a line that happens to use
the same word documents nothing by name. Upstream masks a Go doc comment's
declared name itself (`DocName`, `leadingMask`); the fork's mask goes further,
to every name in a slash-separated run and to the name after `Package`, and the
two agree wherever both apply.

- `internal/lint/code.go` — `maskDocOpener`, `docOpener`, `packageOpener`, `runNames`, `declares`, `declarationBlock`, `namedInAny`, `namedIn`, `commentEnd`, `mask`, `blockSlack`; the call gated on `lang.OpensWithName`.
- `internal/lint/code/lang.go` — `Language.OpensWithName`, set in `go.go`, `c.go`, `cpp.go` and `rs.go`.
- `internal/lint/docopener_test.go`; `testdata/e2e/lint.yaml` case `doc-opener-by-language`.

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
no way to tell them from the help text beside them. The opening quote is
recorded as the first line's strip, the way a comment's opening delimiter is,
so a raw string's later lines map back through the dedent alone.

- `internal/lint/code/comments.go` — `LiteralScope`, `IsLiteral`, `WithLiterals`, `splitLiterals`.
- `internal/lint/code/lang.go` — `Language.Literals`, `Language.SkipLiteral`.
- `internal/lint/code/go.go` — the literal queries and `goStructuralLiteral`.
- `internal/lint/code/query.go` — the `literal` capture, `trimQuotes`, the family in the scope name, the quote as the first line's `opener`.
- `internal/lint/code/comments.go` — literals kept apart from `coalesce`, `dropDirectives` and the documentation masks, and put back in source order.
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

A nested section may carry a condition, `[*.md if .Meta.draft]`, exactly as a
root one does: the anchored label keeps the condition, and it applies to the
files beneath the nested file's directory alone.

`--config`, `VALE_CONFIG_PATH` and `--sources` read no nested file: naming a
configuration means that file rather than a search, so a run pinned to one
reports the same whatever a checkout holds.

- `internal/core/nested.go` -- `NestedConfigName`, `discoverNested`, `walkDirs`, `anchorLabel`, `loadNested`, `IsDirPath`.
- `internal/core/ini.go` -- `processSection`, splitting a section's condition off its glob; `reservedSections`.
- `internal/core/source.go` -- `ReadPipeline` taking the run's inputs, `wantsNested`.
- `internal/core/config.go` -- `Config.NestedFiles`, `CLIFlags.NoNested`.
- `internal/lint/cache.go` -- `--no-nested` in the salt; a nested file is a configuration file, so the file list already covers it.
- `internal/core/nested_test.go`; `testdata/e2e/config.yaml` cases `nested-carves-out-a-subtree`, `nested-deeper-wins`, `nested-disabled`, `nested-rejects-a-run-wide-key`; `testdata/e2e/conditional-sections.yaml` case `in a nested configuration`.

### A shell script is parsed as code

`.sh`, `.bash`, `.zsh` and `.ksh` are read with the shell grammar, so only the
comments are prose.

- `internal/core/format.go`, `internal/lint/code/lang.go`, `internal/lint/code/sh.go`.
- `internal/lint/code/sh_test.go`; `testdata/e2e/lint.yaml` case `shell-script-comments`.

### A build file is parsed as code, matched by name where it has no extension

A build file is named rather than suffixed, so matching on the extension alone
never reached one. `Makefile`, `makefile` and `GNUmakefile` are matched by name;
`.mk`, `.mak` and `.make` by extension. Both reach the shell grammar, which the
two share a comment syntax with and which recovers across a rule header it
cannot read, so a comment below one is captured exactly as a comment above it is.

A file matched by name has no extension of its own. Upstream's `languageFor`
falls back from the file's own extension to the normed one `FormatFromExt`
resolved, which is how such a file reaches its grammar.

- `internal/core/format.go` — `FormatByFilename`, consulted from `FormatFromExt` only where the configured `[formats]` mapping has nothing for the path, so a project naming such a file itself still decides what it is read as.
- `internal/lint/code/mk.go`, `internal/lint/code/lang.go`.
- `internal/core/format_test.go` — `TestFormatFromExtNamesABuildFile`; `testdata/e2e/lint.yaml` case `build-file-comments`.

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
alerts for as long as the entry lives. A file the cache cannot read is linted
the way an uncached run lints it and stores nothing. A cache that cannot be
opened is reported and then done without.

- `internal/cache/cache.go` — the store: `Open`, `Key`, `Get`, `Put`, `Clean`, `Size`, `Dir`, `trim`, `markUsed`.
- `internal/lint/cache.go` — the policy: `EnableCache`, `cacheSalt`, `hashTree`, `cacheable`, `cacheKey`, `cachedResult`, `storeResult`, `cachedExts`, `cachedAlert`.
- `internal/lint/lint.go` — `Linter.cache`, and the lookup and store around `lintFile`.
- `cmd/vale/cache.go`, `cmd/vale/command.go`, `cmd/vale/flag.go`, `cmd/vale/main.go`, `internal/core/config.go`.
- `internal/cache/cache_test.go`, `internal/lint/cache_test.go` (`TestEveryFlagIsClassified` holds every CLI flag to a decision about the salt).
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

## Absorbed by upstream

Two fork changes are upstream's own now, and the fork carries neither.

- An accepted phrase matching across any whitespace: upstream's `termPattern`
  turns the spacing of every vocabulary term into `\s+`, for the exception
  list and the phrase list alike, and since v3.25.1 a rule's own tokens match
  across a line wrap too.
- The spelling exception pattern compiled once per vocabulary rather than once
  per term: upstream since v3.25.1.

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

Upstream v3.23.0 to v3.25.1 move alerts a consumer compares line by line. None
is a fork feature:

- A space in an `existence` token or a `substitution` key matches a line break,
  so a wrapped phrase is reported where it was missed before.
- An alert in a comment is placed by what extraction took off its line, so its
  column can move on prose that did not change.
- A comment addressed to a tool -- `//go:`, `//nolint`, `# noqa` -- is dropped
  before it is read, and a documentation convention's tags and links (Javadoc,
  JSDoc, rustdoc, a Go doc comment's own name) are masked.
- A spelling alert says when a word is a casing mistake or looks like code, so
  its message text changes, and the default dictionary accepts common technical
  vocabulary it reported before.
- A document that cannot be read or parsed is reported after the rest, and the
  run goes on.

## Releases

A fork release is tagged `v<upstream base>-fza.<N>`: `v3.25.1-fza.1`,
`v3.25.1-fza.2`, then `v3.26.0-fza.1` once the fork sits on the next upstream
release. `N` restarts at 1 on every new base, so the base a release carries is
read off its tag alone. Every fork version contains `fza`, which is how a
consumer tells this build from upstream's.

GoReleaser refuses a tag that is not a semantic version, which rules out a
literal `.fza.N` appended to a stable base (`v3.25.1.fza.1`). The `-fza.N` form
is a prerelease in semver terms, so it sorts below the plain upstream version;
the release configuration marks it a full release regardless.

Rejected:

- `v3.25.1-0.fza.N`: keeps the literal `.fza.N` text at the cost of a
  meaningless `-0.` component, and gains nothing over `-fza.N`.
- `v3.25.1+fza.N`: semver ignores build metadata in precedence, so every fork
  release on one base compares equal to upstream's and to each other, and `+`
  needs escaping in every download URL.

### Where a release is built

A fork tag is released by `.github/workflows/release-fza.yml`, which runs only
for a tag matching `v*-fza.*`, against `.goreleaser.fza.yml`. It builds macOS
and Linux on amd64 and arm64 inside the cross-compiling image upstream's release
already uses (cgo parsers need its osxcross toolchain), publishes the GitHub
release, and writes the cask `vale` into `fza/homebrew-tap`:

```sh
brew install fza/tap/vale
```

The release owner and the cask homepage come from `GITHUB_REPOSITORY_OWNER`.
`skip_upload` comes from the Actions variable `HOMEBREW_CASK_SKIP_UPLOAD`, set
to `false` on the fork, since every fork tag is a semver prerelease and `auto`
would skip it. `HOMEBREW_TAP_TOKEN` is a fine-grained token with contents write
on the tap alone. A quarantine `install_steps` hook makes the unsigned binary
runnable on macOS.

Upstream's `main.yml` skips its GoReleaser and docs steps for a tag containing
`-fza.`, so a fork release starts no Chocolatey build and no vale.sh dispatch.

Rejected:

- Switching upstream's release workflow off in the fork's Actions settings:
  touches no upstream file, but the switch lives outside the repository, a fresh
  fork forgets it, and it also stops upstream's README lint.
- A cask entry inside upstream's `.goreleaser.yml`: one configuration, but the
  fork then edits upstream's most-changed release files and must skip
  Chocolatey and Windows builds it has no use for.

Accepted cost: the upstream base tag pushed to the fork for the changelog still
runs upstream's release job there, which fails at Chocolatey and publishes
nothing.

## Branches

Every fork change lives on its own branch: `feat/<name>` for a feature or a
performance change, `fix/<name>` for a correction. A branch starts at
`upstream/v3`, or at the one branch it builds on, and holds that change alone.
`fza` is `upstream/v3` with every change branch merged into it, one merge
commit per branch. The release branch is the branch upstream develops on,
`v3`: it is reset to `upstream/v3` and `fza` is merged into it, so it points
where `fza` does, stays the fork's default branch, and carries every release
tag. The release branch follows upstream's name, as the `fza/sdd` fork's
`main` does.

A new upstream release is taken by rebasing each change branch onto it, dropping
a commit upstream has since made redundant, and rebuilding `fza` from upstream
by merging the branches again. A conflict between two fork changes is resolved
in the merge that brings the second one in.

A branch cut from upstream lifts out as an upstream pull request as it stands,
and `git log --first-parent upstream/v3..fza` lists the fork's changes one merge
per line.

Rejected:

- Branches stacked on the `fza` tip and fast-forwarded into it: a linear `fza`,
  but each branch then carries every change before it, so none lifts out without
  a rebase, and rebasing `fza` leaves every branch pointing at commits it no
  longer contains.
- Branches stacked on `fza` and merged: shows each change's boundary, with the
  same stacking cost and merge commits that resolve nothing.
- A single linear `fza` with no change branches: separable commit by commit, but
  a change spanning several commits has no name, and nothing marks where it
  begins and ends.
- A separate `main` release branch with `v3` mirroring upstream: shows plain
  upstream on the fork, but adds a branch to keep in step and departs from
  naming the release branch after upstream's.
- Merging each upstream release into `fza`: never rewrites published history,
  but interleaves upstream merges with fork work and ties every change to the
  resolutions around it.

Accepted cost: every upstream update rewrites the change branches, `fza` and the
release branch, so each is force-pushed to the fork remote.
