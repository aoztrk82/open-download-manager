package api

import (
	"encoding/json"
	"testing"

	"github.com/aoztrk82/open-download-manager/internal/engine"
	"github.com/aoztrk82/open-download-manager/internal/queue"
	"github.com/aoztrk82/open-download-manager/internal/store"
)

// fakeIntegrations records calls instead of launching browsers.
type fakeIntegrations struct{ installed []string }

func (f *fakeIntegrations) Browser(id string) (bool, string) { return id != "edge", "C:/odm/" + id }
func (f *fakeIntegrations) Install(id string) error {
	f.installed = append(f.installed, id)
	return nil
}
func (f *fakeIntegrations) OpenFolder(id string) error { return nil }

func newIntegrationServer(t *testing.T, in Integrations) *Server {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	q := queue.New(queue.Config{DefaultDir: t.TempDir()}, st, engine.New())
	return &Server{Queue: q, Integrations: in, Extensions: map[string]string{"chrome": "0.2.0", "firefox": "0.2.0"}}
}

const self = "127.0.0.1:6800"

func TestIntegrationsDisabledWithoutImplementation(t *testing.T) {
	h := newIntegrationServer(t, nil).Handler() // e.g. a remote server
	if got := do(h, "GET", "/v1/integrations", self, nil, "").Code; got != 501 {
		t.Fatalf("list: got %d, want 501", got)
	}
	if got := do(h, "POST", "/v1/integrations/firefox/install", self, nil, "").Code; got != 501 {
		t.Fatalf("install: got %d, want 501", got)
	}
}

func TestIntegrationsInstallAndSeenVersion(t *testing.T) {
	f := &fakeIntegrations{}
	h := newIntegrationServer(t, f).Handler()

	js := map[string]string{"Content-Type": "application/json"}
	if got := do(h, "POST", "/v1/integrations/notabrowser/install", self, js, "").Code; got != 404 {
		t.Fatalf("unknown id: got %d, want 404", got)
	}
	// Bodies a page can send without a preflight must not launch a browser.
	if got := do(h, "POST", "/v1/integrations/firefox/install", self,
		map[string]string{"Content-Type": "text/plain"}, "").Code; got != 415 {
		t.Fatalf("text/plain install: got %d, want 415", got)
	}
	if len(f.installed) != 0 {
		t.Fatalf("install ran without JSON: %v", f.installed)
	}
	if got := do(h, "POST", "/v1/integrations/firefox/install", self, js, "").Code; got != 204 {
		t.Fatalf("install: got %d, want 204", got)
	}
	if len(f.installed) != 1 || f.installed[0] != "firefox" {
		t.Fatalf("install calls: %v", f.installed)
	}
	// A hostile page must not be able to trigger an install.
	if got := do(h, "POST", "/v1/integrations/chrome/install", self,
		map[string]string{"Origin": "https://evil.example.com"}, "").Code; got != 403 {
		t.Fatalf("hostile install: got %d, want 403", got)
	}

	// The extension reports its version on /v1/info; junk is ignored.
	do(h, "GET", "/v1/info?ext=firefox/0.1.0", self, map[string]string{"Origin": "moz-extension://x"}, "")
	do(h, "GET", "/v1/info?ext=evil/9.9", self, nil, "")
	w := do(h, "GET", "/v1/integrations", self, nil, "")
	var list []Integration
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	by := map[string]Integration{}
	for _, it := range list {
		by[it.ID] = it
	}
	if len(list) != 3 {
		t.Fatalf("want 3 browsers, got %+v", list)
	}
	if ff := by["firefox"]; ff.Seen == nil || ff.Seen.Version != "0.1.0" || ff.Bundled != "0.2.0" {
		t.Fatalf("firefox row: %+v", ff)
	}
	if e := by["edge"]; e.Bundled != "0.2.0" || e.Installed || e.Seen != nil {
		t.Fatalf("edge row (uses the chrome build): %+v", e)
	}

	var info Info
	if err := json.Unmarshal(do(h, "GET", "/v1/info", self, nil, "").Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if !info.CanIntegrate || info.Extensions["firefox"] != "0.2.0" {
		t.Fatalf("info: %+v", info)
	}
}
