//go:build windows

package main

import "testing"

func TestTrayStringsParity(t *testing.T) {
	for lang, m := range trayStrings {
		for k := range trayStrings["en"] {
			if m[k] == "" {
				t.Errorf("%s: missing %q", lang, k)
			}
		}
	}
	if l := uiLang(); trayStrings[l] == nil {
		t.Fatalf("uiLang() = %q has no strings", l)
	}
}
