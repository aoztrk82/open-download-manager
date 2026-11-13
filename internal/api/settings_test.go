package api

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/aoztrk82/open-download-manager/internal/config"
)

type fakeSettings struct{ s config.Settings }

func (f *fakeSettings) Settings() config.Settings           { return f.s }
func (f *fakeSettings) SetSettings(s config.Settings) error { f.s = s; return nil }

type fakeAutostart struct{ on bool }

func (f *fakeAutostart) Enabled() bool     { return f.on }
func (f *fakeAutostart) Set(on bool) error { f.on = on; return nil }

func TestSettingsEndpoints(t *testing.T) {
	if got := do(dialogServer(t, &Server{}), "GET", "/v1/settings", self, nil, "").Code; got != 501 {
		t.Fatalf("server without settings: %d", got)
	}
	dir := filepath.Join(t.TempDir(), "dl")
	fs := &fakeSettings{s: config.DefaultSettings()}
	fs.s.DefaultDir = dir
	auto := &fakeAutostart{}
	h := dialogServer(t, &Server{AnyDir: true, Settings: fs, Autostart: auto})

	var v SettingsView
	if err := json.Unmarshal(do(h, "GET", "/v1/settings", self, nil, "").Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Autostart == nil || *v.Autostart || v.CPUConnections < 2 || v.MaxActive != 3 {
		t.Fatalf("GET: %+v", v)
	}

	for _, c := range []struct {
		name string
		hdr  map[string]string
		body string
		want int
	}{
		{"text/plain", map[string]string{"Content-Type": "text/plain"}, `{"maxActive":5}`, 415},
		{"hostile", map[string]string{"Content-Type": "application/json", "Origin": "https://evil.example.com"}, `{"maxActive":5}`, 403},
		{"bad number", jsonHdr, `{"maxActive":0}`, 400},
		{"relative dir", jsonHdr, `{"defaultDir":"x"}`, 400},
		{"ok", jsonHdr, `{"maxActive":5,"browserTypes":[".PDF"],"autostart":true,"capture":{"firefox":false}}`, 200},
	} {
		if got := do(h, "PUT", "/v1/settings", self, c.hdr, c.body).Code; got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
	if fs.s.MaxActive != 5 || len(fs.s.BrowserTypes) != 1 || fs.s.BrowserTypes[0] != "pdf" || !auto.on {
		t.Fatalf("after PUT: %+v autostart=%v", fs.s, auto.on)
	}
	// Fields the client did not send keep their values; maps merge per browser.
	if fs.s.DefaultDir != dir || !fs.s.ShowProgress || fs.s.Capture["firefox"] || !fs.s.Capture["chrome"] {
		t.Fatalf("partial PUT lost values: %+v", fs.s)
	}
	// The download folder follows the settings.
	var info Info
	_ = json.Unmarshal(do(h, "GET", "/v1/info", self, nil, "").Body.Bytes(), &info)
	if info.DefaultDir != dir || !info.CanSettings || info.CanOpen {
		t.Fatalf("info: %+v", info)
	}
}

func TestOpenFile(t *testing.T) {
	var opened string
	h := dialogServer(t, &Server{AnyDir: true, Open: func(p string) error { opened = p; return nil }})
	rec := do(h, "POST", "/v1/downloads", self, jsonHdr, addBody("", true))
	var d struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &d)
	if got := do(h, "POST", "/v1/downloads/"+d.ID+"/open", self, jsonHdr, "").Code; got != 409 {
		t.Fatalf("unfinished download: %d", got)
	}
	if got := do(h, "POST", "/v1/downloads/nope/open", self, jsonHdr, "").Code; got != 404 {
		t.Fatalf("unknown id: %d", got)
	}
	if opened != "" {
		t.Fatalf("opened %q", opened)
	}
	if got := do(dialogServer(t, &Server{}), "POST", "/v1/downloads/x/open", self, jsonHdr, "").Code; got != 501 {
		t.Fatalf("server without open: %d", got)
	}
}
