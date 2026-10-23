# CLAUDE.md

Claude-Code-specific notes for the `fza/vale` fork. What the fork carries, how it branches and how it
releases is catalogued in [CHANGES.fza.md](CHANGES.fza.md).

## Pushing (fza fork)

Pushing is allowed to `origin` (`github.com/fza/vale`) only. Never push to `upstream`
(`errata-ai/vale`); a change for upstream goes there as a pull request the owner files.

- `fza` and `v3` are force-pushed after `fza` is rebased onto a new upstream release. Both point at
  the same commit.
- A fork tag `v<upstream base>-fza.<N>` is pushed to `origin`, and so is the upstream tag it builds
  on, which the release changelog counts from.
- The cask the release job writes into `fza/homebrew-tap` is the job's own commit. The tap's README
  row for this cask is committed and pushed in that repository, under its own rules.
