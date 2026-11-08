# CLAUDE.md

Claude-Code-specific notes for the `fza/vale` fork. Its decisions live in the sdd graph
(`github.com/fza/vale-sdd`, private, checked out at `../vale-sdd`).

## Pushing (fza fork)

Pushing is allowed to `origin` (`github.com/fza/vale`) only. Never push to `upstream`
(`errata-ai/vale`), and file no pull request there.

- `main` is the fork's one long-lived branch and its GitHub default branch. Work lands on it.
- A fork tag `v<upstream base>-fza.<N>` is pushed to `origin`, and so is the upstream tag it builds
  on, which the release changelog counts from.
- The cask the release job writes into `fza/homebrew-tap` is the job's own commit. The tap's README
  row for this cask is committed and pushed in that repository, under its own rules.
