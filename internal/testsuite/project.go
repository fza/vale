package testsuite

import "github.com/vale-cli/vale/v3/internal/core"

// NewRunnerFor prepares to run cases against a configuration that is already
// resolved, such as one built from text over an in-memory filesystem. Cases
// read rules and documents through cfg.FS. The project linter is built on
// first use, so a rule that does not compile fails the cases that depend on
// it, not the ones isolated to another rule.
func NewRunnerFor(cfg *core.Config) *Runner {
	return &Runner{
		flags:    cfg.Flags,
		cfg:      cfg,
		paths:    cfg.SearchPaths(),
		pathsSet: true,
		fs:       cfg.FS,
		deps:     map[string][]string{},
	}
}
