//go:build !windows

package main

import "github.com/spf13/cobra"

// The tray/window app is Windows-only; elsewhere a bare `odm` prints help.
func guiCommands() (func(*cobra.Command, []string) error, []*cobra.Command) {
	return func(cmd *cobra.Command, _ []string) error { return cmd.Help() }, nil
}
