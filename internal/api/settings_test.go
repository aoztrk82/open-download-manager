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

func TestLogEndpoints(t *testing.T) {
	h := dialogServer(t, &Server{AnyDir: true, Settings: &fakeSettings{s: config.DefaultSettings()}})
	for _, c := range []struct {
		name string
		hdr  map[string]string
		want int
	}{
		{"text/plain", map[string]string{"Content-Type": "text/plain"}, 415},
		{"hostile page", map[string]string{"Content-Type": "application/json", "Origin": "https://evil.example.com"}, 403},
		{"extension", map[string]string{"Content-Type": "application/json", "Origin": "chrome-extension://abc"}, 204},
	} {
		if got := do(h, "POST", "/v1/log", self, c.hdr, `{"msg":"capture","fields":{"url":"https://x/a.zip"}}`).Code; got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
	if got := do(h, "GET", "/v1/log", self, nil, "").Code; got != 200 {
		t.Fatalf("GET /v1/log: %d", got)
	}
	if got := do(dialogServer(t, &Server{}), "GET", "/v1/log", self, nil, "").Code; got != 501 {
		t.Fatalf("server GET /v1/log: %d", got)
	}
}

// A server (token mode) has Settings too, but folders stay inside its --dir.
func TestServerSettingsKeepFolders(t *testing.T) {
	base := t.TempDir()
	fs := &fakeSettings{s: config.DefaultSettings()}
	h := dialogServer(t, &Server{Token: "tok", DefaultDir: base, Settings: fs})
	auth := map[string]string{"Content-Type": "application/json", "Authorization": "Bearer tok"}
	inside, _ := json.Marshal(filepath.Join(base, "films"))
	outside, _ := json.Marshal(filepath.Join(t.TempDir(), "x"))
	cats := func(dir []byte) string {
		return `{"categories":[{"id":"video","exts":["mp4"],"dir":` + string(dir) + `}],"defaultDir":` + string(outside) + `}`
	}
	if got := do(h, "PUT", "/v1/settings", "files.example.com", auth, cats(outside)).Code; got != 400 {
		t.Fatalf("category outside the server folder: %d", got)
	}
	if got := do(h, "PUT", "/v1/settings", "files.example.com", auth, cats(inside)).Code; got != 200 {
		t.Fatalf("category inside: %d", got)
	}
	if fs.s.DefaultDir != "" {
		t.Fatalf("a server's download folder changed to %q", fs.s.DefaultDir)
	}
	if got := do(h, "GET", "/v1/settings", "files.example.com", map[string]string{}, "").Code; got != 401 {
		t.Fatalf("settings without the token: %d", got)
	}
}

func TestShowWindow(t *testing.T) {
	var shown string
	h := dialogServer(t, &Server{AnyDir: true, ShowWindow: func(id string) error { shown = id; return nil }})
	rec := do(h, "POST", "/v1/downloads", self, jsonHdr, addBody("", true))
	var d struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &d)
	if got := do(h, "POST", "/v1/downloads/"+d.ID+"/window", self, jsonHdr, "").Code; got != 204 || shown != d.ID {
		t.Fatalf("window: %d %q", got, shown)
	}
	if got := do(h, "POST", "/v1/downloads/nope/window", self, jsonHdr, "").Code; got != 404 {
		t.Fatalf("unknown id: %d", got)
	}
	if got := do(dialogServer(t, &Server{}), "POST", "/v1/downloads/x/window", self, jsonHdr, "").Code; got != 501 {
		t.Fatalf("no window support: %d", got)
	}
}
