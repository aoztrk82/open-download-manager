package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNormalize(t *testing.T) {
	abs := filepath.Join(os.TempDir(), "odm-dl")
	s := DefaultSettings()
	s.BrowserTypes = []string{".PDF, *.epub", "pdf", ""}
	s.AutoSites = []string{"https://Files.Example.com/x?y", "*.cdn.net", "cdn.net"}
	s.HostConnections = []HostLimit{{"Example.com", 4}, {"example.com", 8}}
	s.DefaultDir = abs + string(filepath.Separator)
	s.Capture = map[string]bool{"firefox": false, "opera": true}
	n, err := s.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(n.BrowserTypes, []string{"pdf", "epub"}) {
		t.Errorf("types %q", n.BrowserTypes)
	}
	if !reflect.DeepEqual(n.AutoSites, []string{"files.example.com", "cdn.net"}) {
		t.Errorf("sites %q", n.AutoSites)
	}
	if len(n.HostConnections) != 1 || n.HostConnections[0] != (HostLimit{"example.com", 4}) {
		t.Errorf("host limits %+v", n.HostConnections)
	}
	if n.DefaultDir != abs {
		t.Errorf("dir %q", n.DefaultDir)
	}
	if !reflect.DeepEqual(n.Capture, map[string]bool{"firefox": false}) {
		t.Errorf("capture %v", n.Capture)
	}

	bad := []func(*Settings){
		func(s *Settings) { s.MaxActive = 0 },
		func(s *Settings) { s.Connections = 33 },
		func(s *Settings) { s.SpeedLimit = -1 },
		func(s *Settings) { s.DefaultDir = "relative" },
		func(s *Settings) { s.HostConnections = []HostLimit{{"a.com", 0}} },
		func(s *Settings) { s.SkipSites = []string{"a.com/path"} },
		func(s *Settings) { s.Categories[0].Dir = "rel" },
	}
	for i, f := range bad {
		s := DefaultSettings()
		f(&s)
		if _, err := s.Normalize(); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
}

func TestDirForAndConnections(t *testing.T) {
	s := DefaultSettings()
	s.DefaultDir = filepath.Join(os.TempDir(), "dl")
	if got := s.DirFor("movie.MKV", false); got != s.DefaultDir {
		t.Errorf("categories off: %q", got)
	}
	s.UseCategories = true
	video := filepath.Join(s.DefaultDir, "Video")
	if got := s.DirFor("movie.MKV", false); got != video {
		t.Errorf("mkv -> %q", got)
	}
	if got := s.DirFor("", true); got != video {
		t.Errorf("stream -> %q", got)
	}
	if got := s.DirFor("notes.unknownext", false); got != s.DefaultDir {
		t.Errorf("unknown -> %q", got)
	}
	// A folder of its own wins over the one inside the download folder.
	own := filepath.Join(os.TempDir(), "films")
	s.Categories[0].Dir = own
	if got := s.DirFor("a.mp4", false); got != own {
		t.Errorf("own folder -> %q", got)
	}

	s.Connections = 6
	s.HostConnections = []HostLimit{{"example.com", 2}}
	for host, want := range map[string]int{"example.com": 2, "dl.example.com": 2, "badexample.com": 6, "other.org": 6} {
		if got := s.ConnectionsFor(host); got != want {
			t.Errorf("%s: %d, want %d", host, got, want)
		}
	}
}

func TestPrefsKeepDefaults(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", os.Getenv("APPDATA"))
	if p := LoadPrefs(); !p.ShowProgress || p.MaxActive != 3 || len(p.Categories) != len(CategoryIDs) {
		t.Fatalf("fresh prefs: %+v", p)
	}
	dir, _ := Dir()
	// An older prefs.json only had the language.
	if err := os.WriteFile(filepath.Join(dir, "prefs.json"), []byte(`{"lang":"tr","showComplete":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	p := LoadPrefs()
	if p.Lang != "tr" || p.ShowComplete || !p.ShowProgress || p.MaxActive != 3 {
		t.Fatalf("merged prefs: %+v", p)
	}
}
