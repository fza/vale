# Merging upstream into this fork

This fork is `fza/vale`, branch `v3`. `origin` is the fork and `upstream` is
`errata-ai/vale`. The branch carries fork-only work that upstream does not have,
so a merge is a real resolution rather than a fast-forward.

## Build in a clean environment, or nothing compiles

**A shell inside the Nix development environment cannot build this repository.**
It exports `SDKROOT`, `DEVELOPER_DIR`, `NIX_CFLAGS_COMPILE` and `NIX_LDFLAGS`
pointing into an `apple-sdk-14.4` store path that need not exist on the machine.
Vale uses cgo for its parsers, so the compile fails at the first system header:

```
# runtime/cgo
_cgo_export.c:3:10: fatal error: 'stdlib.h' file not found
```

That reads like a broken checkout and is not one. Clearing the variables one by
one is not enough, because several more name the same store path. Build with a
bare environment instead:

```bash
env -i HOME="$HOME" PATH="/usr/bin:/bin:/usr/sbin:/sbin:$(dirname $(which go))" \
    go build ./...
```

The same prefix runs the suite and installs the binary.

## What must survive

Every commit on `upstream/v3..HEAD` is fork-only work, and a merge that drops one
is a silent regression. List them before starting:

```bash
git log --oneline upstream/v3..HEAD
```

They cover: a Go string literal read as prose under a literal scope; a doc
comment's opening run and a package comment's own name read as code; a shell
script and a build file parsed as code, so only comments are prose; backticked
spans and fenced blocks masked inside a comment; a vale directive accepted as a
tag and honoured inside a source comment; vocabulary matching across whitespace
and across every accepted spelling; alert reuse for a file that has not changed;
and one worker pool serving every input path.

## The conflicts

See them without touching the working tree:

```bash
git merge-tree --write-tree HEAD upstream/v3
```

Most resolve mechanically. Four carry a real design difference, and each needs
both sides rather than a choice between them.

| File | This fork | Upstream |
|---|---|---|
| `internal/lint/lint.go` | one shared worker pool over every input | a per-source loop, now preparing DITA inside it |
| `internal/lint/walk.go` | `rawTok`, the source text captured before the buffer is reused | a suffix-array index with occurrence counts and line offsets |
| `internal/lint/md.go` | `markTagDirectives` before conversion | an explicit `Parser().Parse` then `Renderer().Render` |
| `internal/lint/code.go` | masking inside `eachDirectiveRun` | `f.SetText(maskURLs(comment.Text))` before the run |

**`code.go` is already solved.** Upstream masks URLs once, ahead of the loop this
fork runs per directive. Setting the text twice makes the second win, so fold the
call into the fork's own masking instead:

```go
f.SetText(maskCode(maskURLs(text)))
```

It conflicts on the adjacent line too, where this fork calls
`GetLanguageFromExt(f.CodeExt())` rather than reading the extension directly.
Keep both.

## Finishing

1. Run the suite in the bare environment above.
2. Tag the next fork version and push the branch and the tag to `origin`.
3. In `format-d-fdbox`, set `VALE_TAG` in `scripts/dev-setup.sh` to that tag and
   run the script. `prose:lint` there refuses a binary at any other tag, so a
   stale one reports rather than linting with rules this repository never asked
   for.

## What proves the merge

This fork's own suite passing is necessary and not sufficient: it does not
exercise the rules the consuming repository depends on. Run the consumer's prose
lint and compare the total against what it reported before the merge:

```bash
cd /Users/felix/Codepot/devops/format-d-fdbox && devbox run prose:lint
```

**Before this merge it reports no error and no warning, across every file it
enumerates.** A merge that loses a fork feature moves that count, because a
feature dropped means a rule reading source the way upstream does rather than the
way this fork does. Treat any movement as a lost resolution and find which one.
