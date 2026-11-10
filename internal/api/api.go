// Package api exposes the queue over REST + WebSocket. The CLI, the Web UI and
// the browser extensions all speak this same protocol.
package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"

	"github.com/aoztrk82/open-download-manager/internal/queue"
	"github.com/aoztrk82/open-download-manager/internal/store"
)

const Version = "0.1.0"

type Server struct {
	Queue      *queue.Manager
	Token      string
	DefaultDir string
	UI         fs.FS // optional; served at "/" without auth when non-nil
}

type Info struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	DefaultDir string `json:"defaultDir"`
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/info", s.info)
	mux.HandleFunc("GET /v1/downloads", s.list)
	mux.HandleFunc("POST /v1/downloads", s.add)
	mux.HandleFunc("GET /v1/downloads/{id}", s.get)
	mux.HandleFunc("POST /v1/downloads/{id}/pause", s.act(s.Queue.Pause))
	mux.HandleFunc("POST /v1/downloads/{id}/resume", s.act(s.Queue.Resume))
	mux.HandleFunc("DELETE /v1/downloads/{id}", s.act(s.Queue.Remove))
	mux.HandleFunc("GET /v1/events", s.events)
	if s.UI != nil {
		mux.Handle("/", http.FileServerFS(s.UI))
	}
	return s.cors(s.auth(mux))
}

// auth protects /v1/*. Browsers cannot set headers on WebSocket handshakes, so
// ?token= is accepted too.
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/") || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if tok == "" {
			tok = r.URL.Query().Get("token")
		}
		if s.Token == "" || subtle.ConstantTimeCompare([]byte(tok), []byte(s.Token)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeErr(w, http.StatusUnauthorized, "invalid or missing token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// cors lets extensions and the Web UI call the API cross-origin. This is safe
// because every /v1 call still requires the secret bearer token.
func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" {
			w.Header().Set("Access-Control-Allow-Origin", o)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, Info{Name: "odm", Version: Version, DefaultDir: s.DefaultDir})
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	all, err := s.Queue.List()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if all == nil {
		all = []*store.Download{}
	}
	writeJSON(w, 200, all)
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	d, err := s.Queue.Get(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, d)
}

func (s *Server) add(w http.ResponseWriter, r *http.Request) {
	var req queue.AddRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, 400, "bad json: "+err.Error())
		return
	}
	if req.Request.URL == "" {
		writeErr(w, 400, "request.url is required")
		return
	}
	d, err := s.Queue.Add(req)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, d)
}

func (s *Server) act(fn func(string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(r.PathValue("id")); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

var upgrader = websocket.Upgrader{
	// Auth is the bearer token, not the origin, so any origin may connect.
	CheckOrigin: func(*http.Request) bool { return true },
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	ch, cancel := s.Queue.Subscribe()
	defer cancel()

	// Detect client disconnects.
	gone := make(chan struct{})
	go func() {
		defer close(gone)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	for {
		select {
		case <-gone:
			return
		case ev := <-ch:
			if err := conn.WriteJSON(ev); err != nil {
				return
			}
		}
	}
}

func fail(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, err.Error())
		return
	}
	writeErr(w, 500, err.Error())
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
