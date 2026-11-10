// Package config resolves per-user paths shared by the server and the CLI.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
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

// LoadOrCreateToken reads the token file, generating a random one if missing.
func LoadOrCreateToken() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, "token")
	if b, err := os.ReadFile(p); err == nil && strings.TrimSpace(string(b)) != "" {
		return strings.TrimSpace(string(b)), nil
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(raw)
	return tok, os.WriteFile(p, []byte(tok), 0o600)
}

// ReadToken reads the token without creating it.
func ReadToken() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(dir, "token"))
	return strings.TrimSpace(string(b)), err
}
