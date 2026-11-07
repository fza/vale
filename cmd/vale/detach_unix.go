//go:build unix

package main

import "syscall"

// detachedProcess puts a child in a session of its own, so that the end of
// this run's session or a signal to its process group does not reach it.
func detachedProcess() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
