//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
)

func execClaude(args ...string) error {
	signal.Ignore(os.Interrupt)
	command := exec.Command("claude", args...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := command.Run()

	var exitError *exec.ExitError

	if errors.As(err, &exitError) {
		os.Exit(exitError.ExitCode())
	}

	return err
}
