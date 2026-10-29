//go:build windows

package main

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// detachedProcess starts a child without a console and in a process group of
// its own, so that closing this run's console does not end it.
func detachedProcess() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
}
