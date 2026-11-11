//go:build windows

package main

import (
	"bytes"
	"testing"
)

func TestEmbeddedIcons(t *testing.T) {
	sig := []byte("\x89PNG\r\n\x1a\n")
	for _, c := range []struct {
		name string
		ico  []byte
		size int
	}{{"tray", trayICO, 16}, {"tray", trayICO, 32}, {"app", appICO, 16}, {"app", appICO, 32}, {"app", appICO, 256}} {
		if p := icoImage(c.ico, c.size); !bytes.HasPrefix(p, sig) {
			t.Errorf("%s icon has no %dpx PNG entry", c.name, c.size)
		}
	}
	if icoImage(appICO, 17) != nil {
		t.Error("17px must not match")
	}
}
