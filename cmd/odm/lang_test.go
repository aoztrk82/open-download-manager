package main

import (
	"testing"

	"github.com/aoztrk82/open-download-manager/internal/config"
)

func TestProgLang(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir()) // config.Dir on Windows
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	var told string
	p := progLang{onChange: func(l string) { told = l }}
	if got := p.Lang(); got != systemLang() {
		t.Fatalf("no choice yet: %q, want system %q", got, systemLang())
	}
	if err := p.SetLang("xx"); err == nil {
		t.Fatal("unknown language accepted")
	}
	if err := p.SetLang("tr"); err != nil {
		t.Fatal(err)
	}
	if p.Lang() != "tr" || told != "tr" || config.LoadPrefs().Lang != "tr" {
		t.Fatalf("after SetLang: lang=%q told=%q saved=%q", p.Lang(), told, config.LoadPrefs().Lang)
	}
}
