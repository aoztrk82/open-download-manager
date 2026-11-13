// Package api exposes the queue over REST + WebSocket. The CLI, the Web UI and
// the browser extensions all speak this same protocol.
package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/aoztrk82/open-download-manager/internal/applog"
	"github.com/aoztrk82/open-download-manager/internal/ffmpeg"
	"github.com/aoztrk82/open-download-manager/internal/proto"
	"github.com/aoztrk82/open-download-manager/internal/queue"
	"github.com/aoztrk82/open-download-manager/internal/store"
	"github.com/aoztrk82/open-download-manager/internal/ytdlp"
)

const Version = "0.4.0"

type Server struct {
	Queue      *queue.Manager
	Token      string
	DefaultDir string
	UI         fs.FS // optional; served at "/" without auth when non-nil
	// Reveal shows a file in the OS file manager. Only set for the local daemon:
	// on a remote server it would open a window on the server, not on the client.
	Reveal func(path string) error
	// Integrations installs the bundled browser extensions. Local daemon only,
	// for the same reason as Reveal.
	Integrations Integrations
	// Extensions are the extension versions bundled in this build, by browser
	// family ("chrome", "firefox"); empty when not bundled.
	Extensions map[string]string
	// AnyDir lets clients pick any absolute download folder (local daemon).
	// Otherwise folders must stay inside DefaultDir.
	AnyDir bool
	// PickFolder shows a native folder chooser and returns the choice ("" if
	// cancelled). Local daemon only.
	PickFolder func(initial string) (string, error)
	// Lang is the shared UI language (local daemon only).
	Lang LangPref
	// Settings is the Settings dialog (local daemon only); with it the
	// download folder follows the settings instead of DefaultDir.
	Settings SettingsStore
	// Autostart is "start with Windows" (local daemon on Windows only).
	Autostart Autostart
	// Open opens a finished file with its default program (local daemon only).
	Open func(path string) error

	seenMu sync.Mutex
	seen   map[string]Seen
}

type Info struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	DefaultDir   string            `json:"defaultDir"`
	CanReveal    bool              `json:"canReveal"`
	CanIntegrate bool              `json:"canIntegrate"`
	CanPickDir   bool              `json:"canPickFolder"`
	CanSettings  bool              `json:"canSettings"`    // GET/PUT /v1/settings
	CanOpen      bool              `json:"canOpen"`        // POST /v1/downloads/{id}/open
	AnyDir       bool              `json:"anyFolder"`      // false: folders must be inside defaultDir
	Lang         string            `json:"lang,omitempty"` // local daemon: the program's UI language
	Extensions   map[string]string `json:"extensions"`
	// FFmpeg: this daemon can join media pieces (HLS/DASH, separate audio+video).
	FFmpeg bool `json:"ffmpeg"`
	// Ytdlp: yt-dlp is installed (YouTube and other sites; see /v1/media).
	Ytdlp bool `json:"ytdlp"`
}

// Browsers ODM ships an extension for. Edge uses the Chrome build.
var Browsers = []string{"firefox", "chrome", "edge"}

// Family maps a browser to the extension build it uses.
func Family(browser string) string {
	if browser == "edge" {
		return "chrome"
	}
	return browser
}

func knownBrowser(b string) bool {
	for _, x := range Browsers {
		if x == b {
			return true
		}
	}
	return false
}

// Seen is the last extension that talked to us from a browser.
type Seen struct {
	Version string    `json:"version"`
	At      time.Time `json:"at"`
}

// Integrations is implemented per OS by the local daemon.
type Integrations interface {
	// Browser reports whether the browser is installed and, for unpacked
	// installs, the folder the user loads the extension from.
	Browser(id string) (installed bool, folder string)
	// Install starts the browser-side install of the bundled extension.
	Install(id string) error
	// OpenFolder shows the unpacked extension folder (Chrome/Edge).
	OpenFolder(id string) error
}

// Integration is one row of GET /v1/integrations.
type Integration struct {
	ID        string `json:"id"`
	Installed bool   `json:"installed"` // the browser, not the extension
	Bundled   string `json:"bundled"`   // extension version in this build
	Seen      *Seen  `json:"seen,omitempty"`
	Folder    string `json:"folder,omitempty"`
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/info", s.info)
	mux.HandleFunc("GET /v1/downloads", s.list)
	mux.HandleFunc("POST /v1/downloads", s.add)
	mux.HandleFunc("POST /v1/probe", s.probe)
	mux.HandleFunc("POST /v1/pickfolder", s.pickFolder)
	mux.HandleFunc("PUT /v1/prefs", s.setPrefs)
	mux.HandleFunc("GET /v1/settings", s.getSettings)
	mux.HandleFunc("PUT /v1/settings", s.putSettings)
	mux.HandleFunc("GET /v1/log", s.getLog)
	mux.HandleFunc("POST /v1/log", s.postLog)
	mux.HandleFunc("POST /v1/log/reveal", s.revealLog)
	mux.HandleFunc("GET /v1/media", s.mediaInfo)
	mux.HandleFunc("POST /v1/media/probe", s.mediaProbe)
	mux.HandleFunc("POST /v1/media/update", s.mediaUpdate)
	mux.HandleFunc("GET /v1/downloads/{id}", s.get)
	mux.HandleFunc("GET /v1/downloads/{id}/connections", s.connections)
	mux.HandleFunc("POST /v1/downloads/{id}/pause", s.act(s.Queue.Pause))
	mux.HandleFunc("POST /v1/downloads/{id}/resume", s.act(s.Queue.Resume))
	mux.HandleFunc("POST /v1/downloads/{id}/reveal", s.reveal)
	mux.HandleFunc("POST /v1/downloads/{id}/open", s.openFile)
	mux.HandleFunc("POST /v1/downloads/{id}/queue", s.setQueue)
	mux.HandleFunc("POST /v1/downloads/{id}/move", s.moveInQueue)
	mux.HandleFunc("GET /v1/queues", s.queues)
	mux.HandleFunc("POST /v1/queues/later/start", s.laterStart)
	mux.HandleFunc("POST /v1/queues/later/stop", s.laterStop)
	mux.HandleFunc("DELETE /v1/downloads/{id}", s.remove)
	mux.HandleFunc("GET /v1/events", s.events)
	mux.HandleFunc("GET /v1/integrations", s.integrations)
	mux.HandleFunc("POST /v1/integrations/{id}/install", s.integrationAct(func(i Integrations, id string) error { return i.Install(id) }))
	mux.HandleFunc("POST /v1/integrations/{id}/folder", s.integrationAct(func(i Integrations, id string) error { return i.OpenFolder(id) }))
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
		if s.Token == "" {
			// Local mode: no secret, so the only protection against hostile web
			// pages is refusing anything that is not clearly us or the CLI.
			if !loopbackHost(r.Host) {
				writeErr(w, http.StatusForbidden, "host not allowed") // DNS rebinding
				return
			}
			if o := r.Header.Get("Origin"); o != "" && !originTrusted(o, r.Host) {
				writeErr(w, http.StatusForbidden, "origin not allowed")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if tok == "" {
			tok = r.URL.Query().Get("token")
		}
		if subtle.ConstantTimeCompare([]byte(tok), []byte(s.Token)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeErr(w, http.StatusUnauthorized, "invalid or missing token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// loopbackHost reports whether a Host header names this machine.
func loopbackHost(hostport string) bool {
	h := hostport
	if hh, _, err := net.SplitHostPort(hostport); err == nil {
		h = hh
	}
	h = strings.Trim(h, "[]")
	return h == "127.0.0.1" || h == "localhost" || h == "::1"
}

// originTrusted decides which browser origins may talk to a token-less local
// daemon: our own pages (same origin) and browser extensions. Extensions are
// allowed by scheme because Firefox's moz-extension host is a random per-install
// UUID. Everything else, including the literal "null", is rejected.
func originTrusted(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil || origin == "null" {
		return false
	}
	switch u.Scheme {
	case "chrome-extension", "moz-extension", "safari-web-extension":
		return u.Host != ""
	case "http":
		return u.Host == host && loopbackHost(u.Host)
	}
	return false
}

// cors lets extensions and the Web UI call the API cross-origin. With a token
// every /v1 call is authenticated; without one, headers are only emitted for
// trusted origins so hostile pages cannot even pass the preflight.
func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" && (s.Token != "" || originTrusted(o, r.Host)) {
			w.Header().Set("Access-Control-Allow-Origin", o)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	// Extensions report themselves as ?ext=<browser>/<version>. A query param
	// rather than a header: a new header would change the CORS preflight and an
	// older daemon would then reject every call from a newer extension.
	if b, v, ok := strings.Cut(r.URL.Query().Get("ext"), "/"); ok && knownBrowser(b) && v != "" && len(v) <= 32 {
		s.seenMu.Lock()
		if s.seen == nil {
			s.seen = map[string]Seen{}
		}
		s.seen[b] = Seen{Version: v, At: time.Now().UTC()}
		s.seenMu.Unlock()
	}
	ext := s.Extensions
	if ext == nil {
		ext = map[string]string{}
	}
	lang := ""
	if s.Lang != nil {
		lang = s.Lang.Lang()
	}
	_, ffErr := ffmpeg.Find() // a few stat calls; reflects an ffmpeg installed while running
	_, ytErr := ytdlp.Find()
	writeJSON(w, http.StatusOK, Info{Name: "odm", Version: Version, DefaultDir: s.defaultDir(), Lang: lang,
		CanReveal: s.Reveal != nil, CanIntegrate: s.Integrations != nil, CanPickDir: s.PickFolder != nil,
		CanSettings: s.Settings != nil, CanOpen: s.Open != nil,
		AnyDir: s.AnyDir, Extensions: ext, FFmpeg: ffErr == nil, Ytdlp: ytErr == nil})
}

func (s *Server) integrations(w http.ResponseWriter, r *http.Request) {
	if s.Integrations == nil {
		writeErr(w, http.StatusNotImplemented, "browser integration is only available on the local daemon")
		return
	}
	out := make([]Integration, 0, len(Browsers))
	s.seenMu.Lock()
	defer s.seenMu.Unlock()
	for _, b := range Browsers {
		it := Integration{ID: b, Bundled: s.Extensions[Family(b)]}
		it.Installed, it.Folder = s.Integrations.Browser(b)
		if v, ok := s.seen[b]; ok {
			it.Seen = &v
		}
		out = append(out, it)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) integrationAct(fn func(Integrations, string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Integrations == nil {
			writeErr(w, http.StatusNotImplemented, "browser integration is only available on the local daemon")
			return
		}
		// Same rule as add: these launch programs, so refuse bodies a hostile page
		// could send cross-origin without a preflight.
		if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
			writeErr(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
			return
		}
		id := r.PathValue("id")
		if !knownBrowser(id) {
			writeErr(w, 404, "unknown browser")
			return
		}
		if err := fn(s.Integrations, id); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
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
	// Refuse form-encoded/text/plain bodies: those are what a hostile page can
	// send cross-origin without a preflight.
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		writeErr(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}
	var req queue.AddRequest
	// 16 MiB: a stream download carries its playlist (a long film with short
	// segments is a few MB of text).
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20)).Decode(&req); err != nil {
		writeErr(w, 400, "bad json: "+err.Error())
		return
	}
	if req.Request.URL == "" {
		writeErr(w, 400, "request.url is required")
		return
	}
	if !queue.KnownQueue(req.Queue) {
		writeErr(w, 400, "unknown queue "+strconv.Quote(req.Queue))
		return
	}
	if m := req.Request.Media; m != nil {
		switch m.Kind {
		case proto.MediaHLS:
		case proto.MediaYtdlp:
			if err := checkPageURL(req.Request.URL); err != nil {
				writeErr(w, 400, err.Error())
				return
			}
			if m.Container != "" && m.Container != "mp4" && m.Container != "mkv" {
				writeErr(w, 400, "container must be mp4 or mkv")
				return
			}
		default:
			writeErr(w, 400, "unknown media kind "+strconv.Quote(m.Kind))
			return
		}
	}
	dir, err := s.checkDir(req.Dir)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	req.Dir = dir
	d, err := s.Queue.Add(req)
	if err != nil {
		applog.L().Warn("add refused", "from", clientOf(r), "url", req.Request.URL, "err", err)
		writeErr(w, 400, err.Error())
		return
	}
	logAdd(r, req, d.ID, d.Dir)
	writeJSON(w, 201, d)
}

func (s *Server) connections(w http.ResponseWriter, r *http.Request) {
	c, err := s.Queue.Connections(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, c)
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

func (s *Server) remove(w http.ResponseWriter, r *http.Request) {
	if err := s.Queue.Remove(r.PathValue("id"), r.URL.Query().Get("deleteFile") == "true"); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) reveal(w http.ResponseWriter, r *http.Request) {
	if s.Reveal == nil {
		writeErr(w, http.StatusNotImplemented, "show in folder is only available on the local daemon")
		return
	}
	d, err := s.Queue.Get(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	path := d.State.Path
	if path == "" {
		path = d.Dir
	}
	if err := s.Reveal(path); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	// charset: JSON is UTF-8 by spec, but some clients (Windows PowerShell 5.1)
	// assume Latin-1 without it and mangle non-ASCII file names.
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
