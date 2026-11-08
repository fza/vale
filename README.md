# Vale – `fza` fork

A fork of [Vale](https://github.com/vale-cli/vale), the command-line linter for prose. For
everything Vale itself does, see the [upstream repository](https://github.com/vale-cli/vale) and
the [Vale docs](https://docs.vale.sh).

**Currently pinned on upstream version:** [`v3.26.0`](https://github.com/vale-cli/vale/releases/tag/v3.26.0)

Fork releases are tagged `v<upstream base>-fza.<N>`, for example `v3.26.0-fza.4`.

## Install

macOS and Linux, with Homebrew:

```bash
brew install fza/tap/vale
```

If upstream's `vale` formula is installed, remove it first with `brew uninstall vale`.

Prebuilt archives for macOS and Linux (amd64, arm64) are attached to each
[release](https://github.com/fza/vale/releases).

## What the fork adds

### Prose in source code

- A `vale` directive works inside a code comment, and also as a tag pair:
  `<vale off>` … `</vale off>`.
- Code spans, fenced blocks, URLs and the name that opens a doc comment are masked, so rules do
  not read them as prose.
- String literals in Go, PHP, TypeScript, JavaScript, Vue and Svelte are linted when a rule uses
  the `literal` scope (`literal.line`, `literal.block`).
- These files are read as code, so only their comments are linted:
  - shell scripts: `.sh`, `.bash`, `.zsh`, `.ksh`
  - build files: `Makefile`, `makefile`, `GNUmakefile`, `.mk`, `.mak`, `.make`
  - components: `.vue`, `.svelte`
- PHPDoc tags such as `@param string $name` are not linted as prose.
- `CommentedCode = lint|mask|flag` in a glob section sets what happens to comment lines that are
  code. `lint` is the default. `mask` hides them from every rule, and `flag` also reports them as
  `Vale.CommentedCode`.

### Configuration

- A `.vale.ini` in any directory being linted adds glob sections for that directory. Use
  `--no-nested` to ignore these files.
- A setting for a rule group (`House.Literals = NO`) applies to every rule in that group.

### Correctness

- A vocabulary accepts all the spellings it lists, for example both `mariadb` and `MariaDB`.
- `vale sync` accepts a fork build for a package's `vale_version`.

### Performance

- Results for unchanged files come from a cache. Use `--no-cache` to skip it, `cache-clean` to
  empty it, and `VALE_CACHE` to move it.
- All input paths are linted by one worker pool.
- Capitalization rules match the vocabulary faster.
- Built-in rules that the configuration turns off everywhere are not built.

## License

[MIT](LICENSE)
