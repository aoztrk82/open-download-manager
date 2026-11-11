package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCliDir(t *testing.T) {
	wd, _ := os.Getwd()
	if got, _ := cliDir("isos", false); got != filepath.Join(wd, "isos") {
		t.Fatalf("local relative: %q", got)
	}
	if got, _ := cliDir("isos", true); got != "isos" {
		t.Fatalf("remote must be sent as typed: %q", got)
	}
	if got, _ := cliDir("", false); got != "" {
		t.Fatalf("empty: %q", got)
	}
}
