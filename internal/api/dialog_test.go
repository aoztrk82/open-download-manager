package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoztrk82/open-download-manager/internal/engine"
	"github.com/aoztrk82/open-download-manager/internal/queue"
	"github.com/aoztrk82/open-download-manager/internal/store"
)

func dialogServer(t *testing.T, s *Server) http.Handler {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if s.DefaultDir == "" {
		s.DefaultDir = t.TempDir()
	}
	// The queue is not started: these tests only look at what gets accepted.
	s.Queue = queue.New(queue.Config{DefaultDir: s.DefaultDir}, st, engine.New())
	return s.Handler()
}

var jsonHdr = map[string]string{"Content-Type": "application/json"}

func addBody(dir string, paused bool) string {
	b, _ := json.Marshal(map[string]any{"request": map[string]string{"url": "http://example.com/a.bin"}, "dir": dir, "paused": paused})
	return string(b)
}

func TestAddFolderRules(t *testing.T) {
	base := t.TempDir()
	inside := filepath.Join(base, "isos")
	outside := filepath.Join(t.TempDir(), "elsewhere")

	server := dialogServer(t, &Server{Token: "tok", DefaultDir: base})
	auth := map[string]string{"Content-Type": "application/json", "Authorization": "Bearer tok"}
	for _, c := range []struct {
		name, dir string
		want      int
	}{
		{"default", "", 201},
		{"inside", inside, 201},
		{"outside", outside, 400},
		{"dot-dot escape", filepath.Join(base, "..", "x"), 400},
		{"relative", "isos", 400},
	} {
		if got := do(server, "POST", "/v1/downloads", "files.example.com", auth, addBody(c.dir, false)).Code; got != c.want {
			t.Errorf("server %s: got %d, want %d", c.name, got, c.want)
		}
	}

	local := dialogServer(t, &Server{AnyDir: true})
	if got := do(local, "POST", "/v1/downloads", self, jsonHdr, addBody(outside, false)).Code; got != 201 {
		t.Errorf("local daemon, any folder: got %d", got)
	}
	if got := do(local, "POST", "/v1/downloads", self, jsonHdr, addBody("relative/dir", false)).Code; got != 400 {
		t.Errorf("local daemon, relative folder: got %d", got)
	}
}

func TestAddPaused(t *testing.T) {
	h := dialogServer(t, &Server{AnyDir: true})
	w := do(h, "POST", "/v1/downloads", self, jsonHdr, addBody("", true))
	var d store.Download
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil || w.Code != 201 {
		t.Fatalf("add: %d %s", w.Code, w.Body)
	}
	if d.Status != store.Paused {
		t.Fatalf("status %q, want paused", d.Status)
	}
}

func TestProbe(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 5000)
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="report.pdf"`)
		http.ServeContent(w, r, "x", time.Time{}, bytes.NewReader(data))
	}))
	defer src.Close()
	h := dialogServer(t, &Server{AnyDir: true})
	body := `{"request":{"url":"` + src.URL + `/dl?id=1"}}`

	if got := do(h, "POST", "/v1/probe", self, map[string]string{"Content-Type": "text/plain"}, body).Code; got != 415 {
		t.Fatalf("text/plain probe: %d", got)
	}
	if got := do(h, "POST", "/v1/probe", self, map[string]string{"Content-Type": "application/json", "Origin": "https://evil.example.com"}, body).Code; got != 403 {
		t.Fatalf("hostile probe: %d", got)
	}
	w := do(h, "POST", "/v1/probe", self, jsonHdr, body)
	var p ProbeResult
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil || w.Code != 200 {
		t.Fatalf("probe: %d %s", w.Code, w.Body)
	}
	if p.Size != int64(len(data)) || !p.AcceptRanges || p.Filename != "report.pdf" {
		t.Fatalf("probe result %+v", p)
	}
	if got := do(h, "POST", "/v1/probe", self, jsonHdr, `{"request":{"url":"gopher://x"}}`).Code; got == 200 {
		t.Fatal("unsupported scheme must fail")
	}
}

func TestPickFolder(t *testing.T) {
	if got := do(dialogServer(t, &Server{}), "POST", "/v1/pickfolder", self, jsonHdr, `{}`).Code; got != 501 {
		t.Fatalf("no picker: %d", got)
	}
	var asked string
	h := dialogServer(t, &Server{AnyDir: true, DefaultDir: "C:/dl", PickFolder: func(initial string) (string, error) {
		asked = initial
		return "D:/isos", nil
	}})
	if got := do(h, "POST", "/v1/pickfolder", self, map[string]string{"Content-Type": "text/plain"}, `{}`).Code; got != 415 {
		t.Fatalf("text/plain: %d", got)
	}
	if got := do(h, "POST", "/v1/pickfolder", self, map[string]string{"Content-Type": "application/json", "Origin": "https://evil.example.com"}, `{}`).Code; got != 403 {
		t.Fatalf("hostile: %d", got)
	}
	w := do(h, "POST", "/v1/pickfolder", self, jsonHdr, `{}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"D:/isos"`) || asked != "C:/dl" {
		t.Fatalf("pick: %d %s (initial %q)", w.Code, w.Body, asked)
	}
}
