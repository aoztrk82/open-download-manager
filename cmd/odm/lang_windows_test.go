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
	if l := systemLang(); trayStrings[l] == nil {
		t.Fatalf("systemLang() = %q has no strings", l)
	}
	trayLang.Store("tr")
	if trayText("quit") != "ODM'den Çık" {
		t.Fatal("tray does not follow trayLang")
	}
	trayLang.Store("xx")
	if trayText("quit") != "Quit ODM" {
		t.Fatal("unknown language must fall back to English")
	}
}
