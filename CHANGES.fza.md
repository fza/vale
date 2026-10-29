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
| `scope: literal` | Rule scope | Match a string literal in Go, PHP, TypeScript, TSX, JavaScript or a Vue component's script. Also `literal.line` and `literal.block`. |
| `<vale off>` … `</vale off>` | Directive | The tag form of a `vale` directive, in markup and in a source comment alike. |
| `.sh`, `.bash`, `.zsh`, `.ksh` | Format | Read as code: only the comments are prose. |
| `.mk`, `.mak`, `.make`, `Makefile`, `makefile`, `GNUmakefile` | Format | Read as code: only the comments are prose. |
| `.vue` | Format | Read as code: the script blocks alone, with the TypeScript grammar. The template and the style reach no rule. |

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

### A fork build meets a package's `vale_version` by the release it carries

A fork build is tagged as a prerelease of its upstream base, `v3.25.1-fza.1`,
and a semver constraint naming no prerelease excludes every prerelease, so
`vale sync` refused a package asking for `>=3.0.0`. A version whose prerelease
opens with `fza.` is measured by the release it carries; any other prerelease
keeps upstream's reading.

- `cmd/vale/pkgver.go` — `supportsVale`, `forkPrerelease`.
- `cmd/vale/pkgver_test.go` — `TestSupportsValeOnAForkBuild`.

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

### Masked comment text is a run no rule crosses

A code comment carries no markup, so three things in it are masked out of its
text before a rule reads it: what it marks as code (a backticked span, a fenced
block), a URL, and the run of names a doc comment opens with. Each was blanked
to spaces, and a space in a rule's token matches any run of whitespace, so a
pattern joined the words on either side of the blank. `` It reads the `config`
the daemon writes. `` reported `the` as repeated, a phrase rule matched `see`
and `for` across a URL, and `Package probe does` matched `Package does`. In
Markdown the same lines report nothing, because Markdown masks inline code
with `*`.

Every masked character in a comment is now a `*` as well. It is no space, so no
pattern crosses it, and no word character, so no rule counts it as a word or
spells it. One `*` stands for one character rather than one byte, because an
alert counts its columns in characters: a span holding `é` pushed every alert
after it one column right per extra byte.

The URL mask is upstream's own code, so its fix is a branch of its own cut from
upstream, `fix/url-mask-placeholder`, and lifts out as an upstream pull request
alone. The span mask and the opener mask are fixed on the branches that carry
them.

Rejected:

- Spaces: every whitespace-widened pattern crosses them.
- A blank of the masked text's own byte width -- a no-break space or an
  ideographic space for a wider character, as upstream's URL mask used: a
  rule's `\s` does not match those, so a URL holding one stopped a phrase and an
  ASCII URL did not. One mask has to behave one way whatever it covers.
- Dropping the alerts inside a masked range, as upstream does for a
  documentation tag (`code.Mask`): the text stays readable, so a rule still
  spells an identifier and reads a URL's words.
- The code span alone, or the code span and the URL: a URL and the name after
  `Package` were crossed the same way, and each left out would need a release
  of its own.
- The URL fix on `feat/comment-code-mask`, beside the span mask it runs with:
  that branch's pull request would then change upstream's URL handling as well.

Accepted cost: a consumer's alerts move. One that matched across a mask
disappears, and one that a phrase exemption suppressed by matching across a
mask appears, as it already did for the same line in Markdown.

- `internal/lint/code.go` -- `maskKeepingLines` under `maskCode`, `mask` under `maskDocOpener`, `maskURLs`.
- `internal/nlp/provider.go` -- `BlankRunes` removed, its one caller gone.
- `internal/lint/code_test.go` -- `TestMaskCode`, `TestMaskURLs`; `internal/lint/docopener_test.go`.
- `testdata/e2e/lint.yaml` -- cases `code-span-placeholder` (Go, C, C++, Rust, Python, with Markdown as the control), `url-placeholder` (Go, C, Python, and Clojure through the regex fallback) and `doc-opener-placeholder`.

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
masked with one `*` per character, with a line break inside a fence surviving
as one, so every line and every column is preserved and an alert still maps back
onto the source; "Masked comment text is a run no rule crosses" says why `*`. A
fence is masked first, so the backticks opening and closing it cannot pair with
the ones a span inside it carries.

- `internal/lint/code.go` — `maskCode`, `fencedCode`, `inlineCode`, `maskKeepingLines`.
- `internal/lint/code_test.go`; `testdata/e2e/lint.yaml` case `code-span-placeholder`.

### A doc comment's opening run reads as code

Go, C, Swift and Rust all ask a doc comment to open with the name it documents,
and no markup is allowed in that position — the one place backticks cannot
reach. That run is masked, as inline code is, so a documented symbol reads as
code rather than as prose. One comment may document several symbols at once,
written as names separated by slashes, and every name in the run is masked.

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
- `internal/lint/docopener_test.go`; `testdata/e2e/lint.yaml` cases `doc-opener-by-language` and `doc-opener-placeholder`.

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

### A PHP, TypeScript and JavaScript string literal is read as Go's is

An exception's message, a log line and a command's output are written as
string literals, in PHP and TypeScript as much as in Go, and only Go's reached
a rule. PHP, TypeScript, TSX and JavaScript now report theirs under the same
`literal` family, extracted under the same condition: only where a rule names
it. PHP reads single- and double-quoted strings, heredocs and nowdocs; the
others read single- and double-quoted strings and template literals.

A heredoc and a nowdoc are captured by their body, so the opener and the
closing marker never reach a rule, and the line break after the opener moves
the literal's first line down, as the whitespace before a Lua comment does.
A body is content to its edges, so a quote opening or closing it stays; the
quote comes off only a node that opens on a quote token.

What a string interpolates is code: `$order`, `{$order->id}`, `${count}`.
Every child of a literal that is not its own text is masked with one `*` per
character, as a code span in a comment is ("Masked comment text is a run no
rule crosses" says why `*`), so the sentence around it still reads and every
column stays. A nowdoc interpolates nothing, so a `$` in one is its text. An
escape keeps its written width, as Go's always did.

A format placeholder is not masked. The consumer's `House.Literals` rules
report the same alerts on a Go string holding `%s`, `%d`, `%w`, `%1$s` and `%q`
as on the same string with each placeholder masked: a placeholder is no space,
so nothing crosses it, and no rule reads it as a word.

Each language drops the strings its syntax marks as structural, as Go drops an
import path and a struct tag, judged by position alone:

- PHP: an attribute's argument (`#[Flow\Scope('singleton')]`,
  `#[ORM\Column(type: 'string')]`), an array key, written in a literal or read
  through a subscript, and the path an `include` or `require` loads.
- TypeScript, TSX and JavaScript: the module an `import` or `export` names or a
  dynamic `import()` loads, an ambient `declare module '...'`, a literal type
  (`type Role = 'auditor' | 'admin'`), the key of an object, an interface, a
  class member or an enum member, a subscript's key, and a directive such as
  `'use strict'`.

Rejected:

- Masking format placeholders: no rule the consumer runs trips on one, so a
  mask would change no alert and leave a pattern to keep right for every
  `printf` dialect.
- Skipping a `require('...')` argument: only the callee's name tells it from any
  other call, which is content rather than position, so it stays in for the
  consumer to carve out by directive, as everything told apart by content does.
- Skipping an enum member's value or a JSX attribute's string: either may be
  copy a person reads, and only the content says which.
- Reading a heredoc's whole node: its opener and closing marker would be prose.

Accepted cost: a JSX attribute holding a class name or an identifier reaches a
`literal` rule, as a Go map value holding one does.

- `internal/lint/code/lang.go` -- `Language.LiteralText`.
- `internal/lint/code/query.go` -- `maskInterpolation`, `quoted`, `isQuote`; the masked text trimmed with its source.
- `internal/lint/code/php.go` -- the literal queries, `phpStructuralLiteral`, `isArrayKey`, `sameNode`.
- `internal/lint/code/js.go` -- `jsLiterals`, `jsLiteralText`, `jsStructuralLiteral`, shared by `ts.go` and `tsx.go`.
- `internal/lint/code/literal_lang_test.go`, with fixtures under `internal/lint/code/testdata/literal/`; `testdata/e2e/scopes.yaml` cases `literal-php`, `literal-typescript`, `literal-tsx`, `literal-javascript` and a `literal-unasked-*` case for each, with fixtures under `testdata/fixtures/scopes/`.

### A Vue single-file component is parsed as code

A `.vue` file matched no format, so Vale read it whole as plain text: template
copy, script comments and style alike. It is now code. Every `<script>` block,
`<script setup>` among them and both where one file holds the two, is read with
the TypeScript grammar, which reads plain JavaScript as well: comments, and
literals where a rule names the family. The template and the style reach no
rule.

The file is handed to the grammar with everything outside its script blocks'
contents blanked to spaces, line breaks kept, so every node the grammar reports
already stands at its line and column in the component, and no alert needs
mapping back. A block opens at the start of a line, as a component's top-level
blocks do, and ends at the first closing tag, where a browser ends one; a
script loaded through `src` closes its own tag and holds nothing.

Rejected:

- Reading `<template>` as markup: an interface's copy is written in the
  language the interface speaks, which need not be the language the rules are
  written for, and it stays unlinted.
- A Vue grammar: the tree-sitter module the fork builds against carries none,
  and a grammar would hand back each block as an embedded range to map anyway.
- Cutting each script block out and offsetting its alerts: a second coordinate
  system for every alert, where blanking keeps one.

- `internal/core/format.go` -- `.vue` mapped to code.
- `internal/lint/code/lang.go` -- `Language.Isolate`, and `.vue` in `languageFor`.
- `internal/lint/code/vue.go` -- `Vue`, `vueScripts`, `vueScript`, `vueScriptEnd`.
- `internal/lint/code/comments.go` -- `Isolate` applied before the parse in `GetComments`.
- `internal/core/util_test.go` -- `TestFormatFromExt`; `internal/lint/code/vue_test.go`, with fixtures under `internal/lint/code/testdata/vue/`; `testdata/e2e/scopes.yaml` cases `literal-vue` and `literal-unasked-vue`.

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

A result cache. Entries are addressed by a hash of everything that could change
the answer, and they live in a handful of files however many there are: an
append log, the tail, that each run adds to once, when its lint ends; sorted
read-only segments the tail is sealed into once it passes 1 MiB; and a manifest
listing the segments. Many Vale processes share one cache: a writer holds a
file lock for the milliseconds an append, a seal or a manifest swap takes, and
a reader takes none. Every record carries a CRC-32C, so a record cut short or
damaged is a miss, never a wrong answer, and an interrupted write costs a
re-lint.

Eviction is by last use, counted in three-day epochs. A record belongs to the
epoch it was written in; a hit on one from the previous epoch writes it again
in the current one, and segments older than the previous epoch are deleted
whole. An entry nobody reads is gone after three to six days.

Upkeep is the sweep, run after a run has reported: it deletes expired segments,
merges an epoch's smallest segments once it holds more than eight, and removes
files no manifest accounts for. One process sweeps at a time, under a
non-blocking lock the operating system releases when its holder dies, so no
claim goes stale. A sweep stops after 200 ms; a merge saves the last key it
wrote, and the next sweep resumes from there. Opening the cache never sweeps.

Each layout the cache directory has held has a clean lock of its own:
`clean-v0.lock` for one file per entry, `clean-v1.lock` for the packed store.
Whoever deletes a layout holds its lock exclusively and writes into it the
layout, its own pid and the time it started; whoever reads or writes a layout
holds its lock shared. A run therefore waits for a clean of the layout it uses,
never for a clean of another, and no layout is deleted by two processes at
once. `vale cache-clean` takes both locks, waiting for the runs under way, and
removes everything but the lock files, which other processes may be waiting
on.

The one-file-per-entry layout of earlier builds takes minutes to delete once it
has grown to millions of files: 360,000 took 35 s on the machine below. A sweep
that finds any of it, with its clean lock free, renames its 256 shard
directories aside, one rename each, and starts `vale cache-clean-layout v0`, a
hidden command, as a process of its own: in a session of its own on Unix,
detached from the console on Windows, inheriting no standard stream, so
nothing waiting on the run's output waits on it. That process takes the lock
itself and deletes what was moved aside. Nothing marks the layout as done: a
clean that is killed frees the lock and leaves files behind, and the next
sweep that finds any starts another.

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

Measured on an Apple-silicon Mac, each run linting 40 Markdown files, wall time
per run:

| Cache | 1 run | 2 at once | 8 at once |
|---|---|---|---|
| One file per entry, 384k entries, daily sweep due | 5.9 s | 6.7 s | 11.1 s |
| One file per entry, 384k entries, sweep not due | | 0.3-0.4 s | 0.8-1.3 s |
| Packed, 1.5M entries in 67 MB and 11 files | 0.8 s | 0.4-0.5 s | 0.6-1.6 s |
| `--no-cache` | | 0.3-0.4 s | 0.6-1.6 s |

The one-file-per-entry cache it replaces had grown to 1.48M entries and 5.7 GB
on the same machine, 97.6% of them a two-byte empty result, and its sweep took
161 s, started again by every process that opened the cache meanwhile.

Rejected:

- One file per entry, the Go build cache's layout: a file-system block and an
  inode for a two-byte result, and a sweep that has to stat every entry.
- A claimed sweep over that layout: one sweeper instead of many, but still
  millions of files and a walk over all of them.
- An embedded database. bbolt, Badger and Pebble hold their store for one
  process at a time. SQLite in pure Go (`modernc.org/sqlite`,
  `ncruces/go-sqlite3`) allows several, for a large dependency and a larger
  binary.
- One segment file per run: no writer ever waits, but every run leaves a file
  until the next merge.
- A merge in one go: a large one takes seconds, longer than a run may wait.
- Routine upkeep in a detached process: spawning one per sweep for work that
  fits in 200 ms buys nothing for its platform code. A process of its own is
  kept for the one deletion that does not fit.
- Deleting the earlier layout in the run that finds it: 1.48M files take about
  two and a half minutes, which that run would wait for, and a hook with a
  timeout may kill it.
- Deleting the earlier layout 200 ms a run: about 2,000 files a run, some 740
  runs for 1.48M files.
- One clean lock for every layout: every run would wait out the minutes the
  earlier layout takes to delete.
- `vale cache-clean` detaching too: it would return before the space is free,
  with nothing to report.
- A directory per configuration: only cheaper deletion, which epochs already
  give.

Accepted costs: an on-disk format of the fork's own, held by its tests; up to
200 ms of upkeep after a run has reported, before it exits; a process that
outlives the run which started it, once per machine, to delete the earlier
layout; and a hidden command for it. An earlier build sharing the directory
recreates its shard directories, which the next sweep moves aside and hands to
another clean.

- `internal/cache/cache.go`: the store (`Open`, `Key`, `Get`, `Put`, `Flush`, `Close`, `Clean`, `Size`, `Dir`).
- `internal/cache/tail.go`, `internal/cache/segment.go`, `internal/cache/manifest.go`: the on-disk format.
- `internal/cache/sweep.go`: `Sweep` (expiry, the resumable merge, strays, the old layout).
- `internal/cache/layout.go`: the clean locks (`holdClean`, `useLayout`), `Clean`, `CleanOldLayout`, `moveOldLayoutAside`.
- `internal/cache/lock_unix.go`, `internal/cache/lock_windows.go`, `internal/cache/lock_other.go`: the file lock, exclusive or shared.
- `internal/lint/cache.go`: the policy (`EnableCache`, `CloseCache`, `cacheSalt`, `hashTree`, `cacheable`, `cacheKey`, `cachedResult`, `storeResult`, `cachedExts`, `cachedAlert`).
- `internal/lint/lint.go`: `Linter.cache`, the lookup and store around `lintFile`, and the flush when `Lint` returns.
- `cmd/vale/main.go` (`CloseCache` once the alerts are printed), `cmd/vale/cache.go` (`cleanCacheLayout`, `startOldLayoutClean`), `cmd/vale/command.go` (the hidden `cache-clean-layout`), `cmd/vale/flag.go`, `internal/core/config.go`.
- `cmd/vale/detach_unix.go`, `cmd/vale/detach_windows.go`, `cmd/vale/detach_other.go`: how the clean is detached from the run.
- `internal/cache/cache_internal_test.go`, `internal/cache/sweep_internal_test.go`, `internal/cache/layout_internal_test.go`, `cmd/vale/cache_internal_test.go`, `internal/lint/cache_test.go` (`TestEveryFlagIsClassified` holds every CLI flag to a decision about the salt).
- `internal/e2e/e2e_test.go`: every scenario runs with `--no-cache`, so a case reports what it was given rather than what an earlier build reported for the same fixture.

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

Upstream expectations that meet a behaviour this fork deliberately adds or
replaces, and what the fork's test data says instead.

| Case | Upstream | This fork |
|---|---|---|
| `testdata/e2e/misc.yaml`, `vocab-multiple` | Two vocabularies spelling one token differently: the later one wins, and the earlier spelling is reported. | Both spellings are accepted, and neither is reported. |
| `testdata/e2e/config-flags.yaml`, `glob`, `glob-negated`, `glob-negated-multi` | `fixtures/formats` is linted whole, and the `.vale.ini` files in `adoc/`, `rst/` and `subdir3/` are fixtures of their own that nothing reads. | Those files are nested configurations, and `adoc/.vale.ini` holds a run-wide `[asciidoctor]` section, which a nested file is refused for. The cases run with `--no-nested`, which reads the tree the way upstream does. |

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
and Linux on amd64 and arm64 inside `ghcr.io/goreleaser/goreleaser-cross`,
pinned by tag (Vale's parsers are cgo, and the image carries a cross-compiler
per target, osxcross among them), publishes the GitHub release, and writes the
cask `vale` into `fza/homebrew-tap`:

```sh
brew install fza/tap/vale
```

The binary reports the tag as written, `vale version v3.25.1-fza.1`, which is
what a consumer pinning a tag compares against. The release owner and the cask
homepage come from `GITHUB_REPOSITORY_OWNER`.
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
- Upstream's `jdkato/choco-cross` image: built around upstream's Chocolatey
  step, and the fork needs a GoReleaser that writes casks, which a pinned
  goreleaser-cross tag states in its release notes.
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
