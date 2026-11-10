package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aoztrk82/open-download-manager/internal/engine"
	"github.com/aoztrk82/open-download-manager/internal/queue"
	"github.com/aoztrk82/open-download-manager/internal/store"
)

func newServer(t *testing.T, token string) http.Handler {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	q := queue.New(queue.Config{DefaultDir: t.TempDir()}, st, engine.New())
	return (&Server{Queue: q, Token: token}).Handler()
}

func do(h http.Handler, method, path, host string, hdr map[string]string, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = host
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// Token-less local mode must refuse everything a hostile web page could send.
func TestLocalModeGuards(t *testing.T) {
	h := newServer(t, "")
	const self = "127.0.0.1:6800"
	js := map[string]string{"Content-Type": "application/json"}
	with := func(origin string) map[string]string {
		m := map[string]string{"Content-Type": "application/json"}
		if origin != "" {
			m["Origin"] = origin
		}
		return m
	}
	add := `{"request":{"url":"http://example.com/a.bin"}}`

	tests := []struct {
		name         string
		method, path string
		host         string
		hdr          map[string]string
		body         string
		want         int
	}{
		{"cli, no origin", "GET", "/v1/info", self, nil, "", 200},
		{"localhost host", "GET", "/v1/info", "localhost:6800", nil, "", 200},
		{"dns rebinding host", "GET", "/v1/info", "evil.example.com:6800", nil, "", 403},
		{"web page origin", "POST", "/v1/downloads", self, with("https://evil.example.com"), add, 403},
		{"null origin", "POST", "/v1/downloads", self, with("null"), add, 403},
		{"other localhost port", "POST", "/v1/downloads", self, with("http://127.0.0.1:9999"), add, 403},
		{"own origin", "GET", "/v1/info", self, with("http://127.0.0.1:6800"), "", 200},
		{"firefox extension", "GET", "/v1/info", self, with("moz-extension://1b2c-3d4e"), "", 200},
		{"chrome extension", "GET", "/v1/info", self, with("chrome-extension://abcdefgh"), "", 200},
		{"hostile delete", "DELETE", "/v1/downloads/x?deleteFile=true", self, with("https://evil.example.com"), "", 403},
		{"form post, no preflight", "POST", "/v1/downloads", self,
			map[string]string{"Content-Type": "text/plain", "Origin": "http://127.0.0.1:6800"}, add, 415},
		{"json post from cli", "POST", "/v1/downloads", self, js, add, 201},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := do(h, tc.method, tc.path, tc.host, tc.hdr, tc.body).Code; got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestLocalPreflightOnlyForTrustedOrigins(t *testing.T) {
	h := newServer(t, "")
	pre := func(origin string) *httptest.ResponseRecorder {
		return do(h, "OPTIONS", "/v1/downloads", "127.0.0.1:6800", map[string]string{
			"Origin": origin, "Access-Control-Request-Method": "POST"}, "")
	}
	if got := pre("moz-extension://x").Header().Get("Access-Control-Allow-Origin"); got != "moz-extension://x" {
		t.Fatalf("extension preflight not allowed: %q", got)
	}
	if got := pre("https://evil.example.com").Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("hostile preflight must not get CORS headers, got %q", got)
	}
}

func TestTokenModeStillRequiresToken(t *testing.T) {
	h := newServer(t, "secret")
	if got := do(h, "GET", "/v1/info", "files.example.com", nil, "").Code; got != 401 {
		t.Fatalf("missing token: got %d", got)
	}
	if got := do(h, "GET", "/v1/info", "files.example.com", map[string]string{"Authorization": "Bearer nope"}, "").Code; got != 401 {
		t.Fatalf("wrong token: got %d", got)
	}
	if got := do(h, "GET", "/v1/info", "files.example.com", map[string]string{"Authorization": "Bearer secret"}, "").Code; got != 200 {
		t.Fatalf("right token: got %d", got)
	}
}
