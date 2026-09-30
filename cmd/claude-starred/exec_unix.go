//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

func execClaude(args ...string) error {
	path, err := exec.LookPath("claude")

	if err != nil {
		return err
	}

	return syscall.Exec(path, append([]string{"claude"}, args...), os.Environ())
}
