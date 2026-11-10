// Package config resolves per-user paths shared by the server and the CLI.
package config

import (
	"os"
	"path/filepath"
)

const DefaultPort = 6800

// Dir returns (and creates) the per-user ODM config directory.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(base, "odm")
	return d, os.MkdirAll(d, 0o700)
}

func DefaultDownloadDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "downloads"
	}
	return filepath.Join(home, "Downloads")
}
