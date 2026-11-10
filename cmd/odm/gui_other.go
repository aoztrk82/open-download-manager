//go:build !windows

package main

import (
	"os"

	"github.com/spf13/cobra"
)

// The tray/window app is Windows-only; elsewhere a bare `odm` prints help.
func guiCommands() (func(*cobra.Command, []string) error, []*cobra.Command) {
	return func(cmd *cobra.Command, _ []string) error { return cmd.Help() }, nil
}

// startLocalDaemon starts a headless local daemon.
func startLocalDaemon() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return startDetached(exe, true, "serve", "--local")
}
