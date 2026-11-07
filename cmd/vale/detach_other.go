//go:build !unix && !windows

package main

import "syscall"

// detachedProcess has nothing to set where processes have no sessions.
func detachedProcess() *syscall.SysProcAttr {
	return nil
}
